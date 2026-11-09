package client

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iflyelf/iflygo/internal/configgen"
	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/overlay"
	"gopkg.in/yaml.v3"
)

// Prober 网关探测与配置重载接口（*overlay.Overlay 满足）。
type Prober interface {
	Reachable(vpnIP string) bool
	EnsureTunnel(vpnIP string)
	Reload(yamlBytes []byte) error
}

// RouteManager 不安全路由健康管理：探测网关 → 动态重算 unsafe_routes → 热加载。
type RouteManager struct {
	dir       string
	ov        Prober
	mu        sync.Mutex
	routes    []model.RouteSpec
	effective []configgen.Route
	hash      string
}

// NewRouteManager 创建路由健康管理器。
func NewRouteManager(dir string, ov Prober) *RouteManager {
	return &RouteManager{dir: dir, ov: ov}
}

// Load 从状态目录读取路由描述。
func (m *RouteManager) Load() error {
	b, err := os.ReadFile(filepath.Join(m.dir, "routes.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var routes []model.RouteSpec
	if err := json.Unmarshal(b, &routes); err != nil {
		return err
	}
	m.mu.Lock()
	m.routes = routes
	m.mu.Unlock()
	return nil
}

// Run 周期探测并在变化时热加载。
func (m *RouteManager) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.ProbeOnce()
		}
	}
}

// ProbeOnce 探测所有网关并应用变化。
func (m *RouteManager) ProbeOnce() {
	m.mu.Lock()
	routes := append([]model.RouteSpec(nil), m.routes...)
	m.mu.Unlock()
	if len(routes) == 0 {
		return
	}

	// 并发探测（上限 8），避免网关较多时串行 2s 超时拖长健康检查窗口。
	health := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	seen := map[string]bool{}
	for _, rt := range routes {
		for _, gw := range rt.Gateways {
			if seen[gw.IP] {
				continue
			}
			seen[gw.IP] = true
			wg.Add(1)
			go func(gw model.GatewaySpec) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				ok := m.probe(gw)
				mu.Lock()
				health[gw.IP] = ok
				mu.Unlock()
			}(gw)
		}
	}
	wg.Wait()

	eff := EffectiveUnsafeRoutes(routes, health)
	hash := routeHash(eff)
	m.mu.Lock()
	changed := hash != m.hash
	if changed {
		m.hash = hash
		m.effective = eff
	}
	m.mu.Unlock()
	if !changed {
		return
	}
	log.Printf("🔁 网关健康变化，更新不安全路由: %s", summarizeRoutes(eff))
	if err := m.apply(eff); err != nil {
		log.Printf("⚠️ 热加载不安全路由失败: %v", err)
	}
}

func (m *RouteManager) probe(gw model.GatewaySpec) bool {
	if gw.HealthPort > 0 {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(gw.IP, strconv.Itoa(gw.HealthPort)), 2*time.Second)
		if err == nil {
			conn.Close()
			return true
		}
		return false
	}
	m.ov.EnsureTunnel(gw.IP)
	return m.ov.Reachable(gw.IP)
}

// Reapply 在 overlay 重启/热加载后重新应用当前有效的不安全路由。
//
// overlay 重启会从磁盘 config.yml 重新载入，动态健康筛选结果可能丢失；
// 这里用上一次的健康结果重新写回，避免等待下一次探测。
func (m *RouteManager) Reapply() error {
	m.mu.Lock()
	eff := append([]configgen.Route(nil), m.effective...)
	m.mu.Unlock()
	if len(eff) == 0 {
		return nil
	}
	return m.apply(eff)
}

// apply 将有效 unsafe_routes 写入配置并热加载。
func (m *RouteManager) apply(routes []configgen.Route) error {
	base, err := os.ReadFile(filepath.Join(m.dir, "config.yml"))
	if err != nil {
		return err
	}
	var root map[string]any
	if err := yaml.Unmarshal(base, &root); err != nil {
		return err
	}
	tun, _ := root["tun"].(map[string]any)
	if tun == nil {
		tun = map[string]any{}
		root["tun"] = tun
	}
	tun["unsafe_routes"] = routesToYAML(routes)
	out, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	caP, certP, keyP := overlay.PKIPaths(m.dir)
	out, err = overlay.RewritePKIPaths(out, caP, certP, keyP)
	if err != nil {
		return err
	}
	return m.ov.Reload(out)
}

// EffectiveUnsafeRoutes 根据健康状态计算有效网关（去掉 down，保留相对权重）。
func EffectiveUnsafeRoutes(specs []model.RouteSpec, health map[string]bool) []configgen.Route {
	var out []configgen.Route
	for _, rt := range specs {
		healthy := healthyGateways(rt, health)
		if len(healthy) == 0 {
			switch rt.AllDownPolicy {
			case "fail_open", "blackhole":
				continue // 移除路由
			default: // fail_closed：保留最后已知（原网关）
				healthy = append([]model.GatewaySpec(nil), rt.Gateways...)
			}
		}
		out = append(out, routeWithGateways(rt, healthy))
	}
	return out
}

// healthyGateways 选出健康网关（active_passive 按优先级分层）。
func healthyGateways(rt model.RouteSpec, health map[string]bool) []model.GatewaySpec {
	up := func(gw model.GatewaySpec) bool {
		v, ok := health[gw.IP]
		return !ok || v
	}

	if rt.Mode == "active_passive" {
		// 找到最小优先级且存在健康网关的层
		prio := map[int]bool{}
		for _, gw := range rt.Gateways {
			if up(gw) {
				prio[gw.Priority] = true
			}
		}
		best := -1
		for p := range prio {
			if best == -1 || p < best {
				best = p
			}
		}
		if best < 0 {
			return nil
		}
		var out []model.GatewaySpec
		for _, gw := range rt.Gateways {
			if gw.Priority == best && up(gw) {
				out = append(out, gw)
			}
		}
		return out
	}

	var out []model.GatewaySpec
	for _, gw := range rt.Gateways {
		if up(gw) {
			out = append(out, gw)
		}
	}
	return out
}

func routeWithGateways(rt model.RouteSpec, gws []model.GatewaySpec) configgen.Route {
	var via any
	if len(gws) == 1 {
		via = gws[0].IP
	} else {
		list := make([]configgen.Gateway, 0, len(gws))
		for _, g := range gws {
			list = append(list, configgen.Gateway{Gateway: g.IP, Weight: g.Weight})
		}
		via = list
	}
	return configgen.Route{Route: rt.CIDR, Via: via, MTU: rt.MTU, Metric: rt.Metric}
}

func routesToYAML(routes []configgen.Route) []any {
	out := make([]any, 0, len(routes))
	for _, r := range routes {
		m := map[string]any{"route": r.Route}
		if r.MTU > 0 {
			m["mtu"] = r.MTU
		}
		if r.Metric > 0 {
			m["metric"] = r.Metric
		}
		switch v := r.Via.(type) {
		case string:
			m["via"] = v
		case []configgen.Gateway:
			var list []any
			for _, g := range v {
				list = append(list, map[string]any{"gateway": g.Gateway, "weight": g.Weight})
			}
			m["via"] = list
		}
		out = append(out, m)
	}
	return out
}

func routeHash(routes []configgen.Route) string {
	b, _ := json.Marshal(routes)
	return string(b)
}

func summarizeRoutes(routes []configgen.Route) string {
	parts := make([]string, 0, len(routes))
	for _, r := range routes {
		parts = append(parts, r.Route)
	}
	if len(parts) == 0 {
		return "(空)"
	}
	return strings.Join(parts, ",")
}
