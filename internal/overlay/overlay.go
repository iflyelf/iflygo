// Package overlay 封装 Nebula，提供启停、热加载与隧道状态查询。
package overlay

import (
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sync"

	"github.com/slackhq/nebula"
	"github.com/slackhq/nebula/config"
	"gopkg.in/yaml.v3"
)

// Peer 一条已建立隧道的信息。
type Peer struct {
	VPNIP    string   `json:"vpn_ip"`
	Remote   string   `json:"remote"`
	Relays   []string `json:"relays,omitempty"`
	CertName string   `json:"cert_name,omitempty"`
}

// Overlay 封装 Nebula 控制句柄。
type Overlay struct {
	mu     sync.Mutex
	ctl    *nebula.Control
	cfg    *config.C
	logger *slog.Logger
}

// New 创建 Overlay。
func New(logger *slog.Logger) *Overlay {
	if logger == nil {
		logger = slog.Default()
	}
	return &Overlay{logger: logger}
}

// Start 用 YAML 配置启动 overlay（非阻塞）。
func (o *Overlay) Start(yamlBytes []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ctl != nil {
		return fmt.Errorf("overlay 已启动")
	}
	cfg := config.NewC(o.logger)
	if err := cfg.LoadString(string(yamlBytes)); err != nil {
		return fmt.Errorf("解析配置: %w", err)
	}
	ctl, err := nebula.Main(cfg, false, "iflygo", o.logger, nil)
	if err != nil {
		return fmt.Errorf("初始化 Nebula: %w", err)
	}
	if err := ctl.Start(); err != nil {
		return fmt.Errorf("启动 overlay: %w", err)
	}
	o.ctl = ctl
	o.cfg = cfg
	return nil
}

// Stop 停止 overlay 并等待退出。
func (o *Overlay) Stop() error {
	o.mu.Lock()
	ctl := o.ctl
	o.ctl = nil
	o.cfg = nil
	o.mu.Unlock()
	if ctl == nil {
		return nil
	}
	ctl.Stop()
	_ = ctl.Wait()
	return nil
}

// Reload 热加载配置（防火墙/路由/relay 等）。
func (o *Overlay) Reload(yamlBytes []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cfg == nil {
		return fmt.Errorf("overlay 未启动")
	}
	// Nebula v1.12.0 起 ReloadConfigString 返回 ReloadResult（含 Err/RestartRequired 等）
	res := o.cfg.ReloadConfigString(string(yamlBytes))
	if res.Err != nil {
		return res.Err
	}
	return nil
}

// Peers 返回已建立隧道列表。
func (o *Overlay) Peers() []Peer {
	o.mu.Lock()
	ctl := o.ctl
	o.mu.Unlock()
	if ctl == nil {
		return nil
	}
	hosts := ctl.ListHostmapHosts(false)
	out := make([]Peer, 0, len(hosts))
	for _, h := range hosts {
		p := Peer{Remote: h.CurrentRemote.String()}
		if len(h.VpnAddrs) > 0 {
			p.VPNIP = h.VpnAddrs[0].String()
		}
		if h.Cert != nil {
			p.CertName = h.Cert.Name()
		}
		for _, r := range h.CurrentRelaysToMe {
			p.Relays = append(p.Relays, r.String())
		}
		out = append(out, p)
	}
	return out
}

// Reachable 判断与某个 overlay 节点是否已建立隧道。
func (o *Overlay) Reachable(vpnIP string) bool {
	o.mu.Lock()
	ctl := o.ctl
	o.mu.Unlock()
	if ctl == nil {
		return false
	}
	addr, err := netip.ParseAddr(vpnIP)
	if err != nil {
		return false
	}
	hi := ctl.GetHostInfoByVpnAddr(addr, false)
	return hi != nil && hi.RemoteIndex != 0
}

// EnsureTunnel 主动触发与某 overlay 节点握手（用于健康探测）。
func (o *Overlay) EnsureTunnel(vpnIP string) {
	o.mu.Lock()
	ctl := o.ctl
	o.mu.Unlock()
	if ctl == nil {
		return
	}
	if addr, err := netip.ParseAddr(vpnIP); err == nil {
		ctl.CreateTunnel(addr)
	}
}

// WritePKI 将 CA/证书/私钥写入目录。
func WritePKI(dir, ca, cert, key string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	files := map[string]struct {
		data string
		mode os.FileMode
	}{
		"ca.crt":   {ca, 0o644},
		"node.crt": {cert, 0o644},
		"node.key": {key, 0o600},
	}
	for name, f := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(f.data), f.mode); err != nil {
			return err
		}
	}
	return nil
}

// PKIPaths 返回状态目录下的证书路径。
func PKIPaths(dir string) (caPath, certPath, keyPath string) {
	return filepath.Join(dir, "ca.crt"), filepath.Join(dir, "node.crt"), filepath.Join(dir, "node.key")
}

// RewritePKIPaths 将配置里的 pki 路径改写为绝对路径。
func RewritePKIPaths(yamlBytes []byte, caPath, certPath, keyPath string) ([]byte, error) {
	var m map[string]any
	if err := yaml.Unmarshal(yamlBytes, &m); err != nil {
		return nil, err
	}
	pki, ok := m["pki"].(map[string]any)
	if !ok {
		pki = map[string]any{}
		m["pki"] = pki
	}
	pki["ca"] = caPath
	pki["cert"] = certPath
	pki["key"] = keyPath
	return yaml.Marshal(m)
}
