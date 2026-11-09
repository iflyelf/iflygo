package svc

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iflyelf/iflygo/internal/tcprelay"
)

// RelayRuntime TCP 中继运行时：监听器可随页面/配置文件热重建。
//
// 页面保存 tcp_relay.* 后调用 ReloadTCPRelay 即可即时生效；多副本通过
// StartRelaySettingsWatcher 周期性同步。模式（auto/tcp/udp）会随心跳下发给客户端，
// 客户端无需手工指定协议。
type RelayRuntime struct {
	mu        sync.RWMutex
	listen    string
	advertise string
	mode      string
	token     string
	port      int
	ln        net.Listener
	cancel    context.CancelFunc
}

// RelayInfo 中继运行时快照。
type RelayInfo struct {
	Mode      string
	Port      int
	Token     string
	Advertise string
}

// NewRelayRuntime 创建中继运行时。
func NewRelayRuntime() *RelayRuntime {
	return &RelayRuntime{mode: "auto"}
}

// Apply 应用新的中继配置；仅当监听地址或令牌变化时重建监听器。
//
//   - mode: auto / tcp / udp（udp 表示不启动中继，客户端只走原生 UDP）
//   - listen: 显式 host:port；"off" 关闭；空值由调用方换算默认端口后传入
func (r *RelayRuntime) Apply(ctx context.Context, mode, listen, advertise, token string) error {
	mode = normalizeRelayMode(mode)
	listen = strings.TrimSpace(listen)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.mode = mode
	r.advertise = strings.TrimSpace(advertise)

	// udp 模式或显式关闭：回收监听器。
	if mode == "udp" || strings.EqualFold(listen, "off") {
		r.stopLocked()
		r.listen = listen
		r.token = token
		return nil
	}
	if listen == "" {
		return fmt.Errorf("TCP 中继监听地址为空（应由调用方换算默认端口）")
	}

	// 当前监听器仍可用且地址/令牌均未变化：直接复用。
	if r.ln != nil && r.listen == listen && r.token == token {
		return nil
	}
	// 同一地址但令牌变化：必须先释放旧监听，否则无法重新 bind。
	sameAddr := r.ln != nil && r.listen == listen
	if sameAddr {
		r.stopLocked()
	}

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", listen, err)
	}
	addr, _ := ln.Addr().(*net.TCPAddr)
	port := 0
	if addr != nil {
		port = addr.Port
	}

	// 先启动新监听，再关闭旧监听，避免配置保存期间出现不可用窗口。
	oldLn, oldCancel := r.ln, r.cancel
	relayCtx, cancel := context.WithCancel(ctx)
	srv := tcprelay.NewServer("")
	srv.Token = token
	go func() {
		if err := srv.Serve(relayCtx, ln); err != nil {
			log.Printf("⚠️ TCP 中继服务退出（%s）: %v", ln.Addr(), err)
		}
	}()

	r.ln = ln
	r.cancel = cancel
	r.listen = listen
	r.token = token
	r.port = port

	if oldLn != nil {
		if oldCancel != nil {
			oldCancel()
		}
		_ = oldLn.Close()
	}
	return nil
}

// Stop 关闭中继监听。
func (r *RelayRuntime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
}

func (r *RelayRuntime) stopLocked() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.ln != nil {
		_ = r.ln.Close()
		r.ln = nil
	}
	r.port = 0
}

// Info 一次性返回运行时快照，避免热路径多次加锁。
func (r *RelayRuntime) Info() RelayInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	mode := r.mode
	if mode == "" {
		mode = "auto"
	}
	return RelayInfo{Mode: mode, Port: r.port, Token: r.token, Advertise: r.advertise}
}

// Port 返回实际监听端口（0 表示未启用）。
func (r *RelayRuntime) Port() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.port
}

// Mode 返回数据面模式 auto/tcp/udp。
func (r *RelayRuntime) Mode() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.mode == "" {
		return "auto"
	}
	return r.mode
}

// Token 返回中继共享令牌。
func (r *RelayRuntime) Token() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.token
}

// Advertise 返回显式配置的对外地址。
func (r *RelayRuntime) Advertise() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.advertise
}

func normalizeRelayMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "tcp":
		return "tcp"
	case "udp":
		return "udp"
	default:
		return "auto"
	}
}

// defaultRelayListen 未显式配置时监听「控制面端口 +1」。
func defaultRelayListen(host string, port int) string {
	if strings.TrimSpace(host) == "" {
		host = "0.0.0.0"
	}
	if port <= 0 {
		port = 8090
	}
	return net.JoinHostPort(host, strconv.Itoa(port+1))
}

// relayRuntimeSettings 是可在运行期热更新、且需要跨副本同步的中继设置键。
var relayRuntimeSettings = []string{
	"tcp_relay.mode",
	"tcp_relay.listen",
	"tcp_relay.advertise",
	"tcp_relay.token",
}

// StartRelaySettingsWatcher 周期性同步其他副本保存的 TCP 中继设置。
func (s *ServiceContext) StartRelaySettingsWatcher(ctx context.Context, interval time.Duration) {
	if s == nil || s.Settings == nil || s.Relay == nil || s.DB == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		syncOnce := func() {
			// SyncFromDB 同时处理「其他副本修改」与「恢复为配置文件值」，
			// 避免只应用增量导致删除不生效。
			changed, err := s.Settings.SyncFromDB(ctx, relayRuntimeSettings)
			if err != nil || len(changed) == 0 {
				return
			}
			s.ReloadTCPRelay(ctx)
			log.Printf("🔄 已同步其他副本的 TCP 中继配置: %s", strings.Join(changed, ", "))
		}
		syncOnce()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				syncOnce()
			}
		}
	}()
}

// ReloadTCPRelay 把当前（页面/文件）配置应用到中继运行时。
func (s *ServiceContext) ReloadTCPRelay(ctx context.Context) {
	if s == nil || s.Relay == nil || s.Settings == nil {
		return
	}
	c := s.Settings.Snapshot()
	listen := strings.TrimSpace(c.TCPRelay.Listen)
	if listen == "" {
		listen = defaultRelayListen(c.Server.Host, c.Server.Port)
	}
	if err := s.Relay.Apply(ctx, c.TCPRelay.Mode, listen, c.TCPRelay.Advertise, c.TCPRelay.Token); err != nil {
		log.Printf("⚠️ TCP 中继配置应用失败: %v", err)
		return
	}
	log.Printf("✅ TCP 中继已应用: mode=%s port=%d", s.Relay.Mode(), s.Relay.Port())
}
