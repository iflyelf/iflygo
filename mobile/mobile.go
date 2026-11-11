// Package mobile 是 iflygo 移动端（Android / HarmonyOS 4）的 Go 核心。
//
// 通过 gomobile bind 生成 AAR，供 Android 原生界面调用：
//   - Enroll：用一次性 token 注册，保存证书/Nebula 配置/状态；
//   - Start(fd)：用 Android VpnService 提供的 TUN fd 启动 Nebula overlay；
//   - Stop / Status / VPNParams / Logs。
//
// 该包不依赖 CLI/信号处理，可在 Android 应用进程中直接运行。
package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/iflyelf/iflygo/internal/client"
	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/overlay"
	"github.com/iflyelf/iflygo/internal/tcprelay"
	"github.com/iflyelf/iflygo/internal/version"
	"gopkg.in/yaml.v3"
)

// Client 移动端客户端核心。
type Client struct {
	mu           sync.Mutex
	server       string
	stateDir     string
	ov           *overlay.Overlay
	hbCancel     context.CancelFunc
	bridgeCancel context.CancelFunc
	lastErr      string
}

// NewClient 创建移动客户端（gomobile 绑定入口）。
//
// gomobile 会把包级函数映射为静态方法，Android 侧调用 Mobile.newClient(...)。
func NewClient(server, stateDir string) *Client { return New(server, stateDir) }

// New 创建移动客户端。stateDir 应为 Android 的 filesDir 子目录。
func New(server, stateDir string) *Client {
	c := &Client{server: normalizeServer(server), stateDir: stateDir}
	initLogging()
	return c
}

// SetServer 更新控制面地址（用户在界面修改后调用）。
func (c *Client) SetServer(server string) {
	c.mu.Lock()
	c.server = normalizeServer(server)
	c.mu.Unlock()
}

// Enroll 用一次性 token 注册并保存配置；返回状态 JSON。
func (c *Client) Enroll(token string) (string, error) {
	c.mu.Lock()
	if c.ov != nil {
		c.mu.Unlock()
		return "", fmt.Errorf("隧道运行中，请先停止再注册")
	}
	server := c.server
	stateDir := c.stateDir
	c.mu.Unlock()
	if server == "" {
		err := fmt.Errorf("请先填写控制面地址")
		c.setErr(err)
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		err := fmt.Errorf("请先填写 enroll token")
		c.setErr(err)
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	id, secret, bundle, err := client.Register(ctx, server, strings.TrimSpace(token), client.RegInfo{
		OS:      "android",
		Arch:    runtime.GOARCH,
		Kernel:  "android",
		Version: version.Version,
	})
	if err != nil {
		c.setErr(err)
		return "", err
	}
	if err := client.ApplyBundle(stateDir, bundle); err != nil {
		c.setErr(err)
		return "", err
	}
	st := &client.State{
		Server: server, ClientID: id, Secret: secret, Name: bundle.Name,
		Network: bundle.Network, Cidr4: bundle.Cidr4, Cidr6: bundle.Cidr6,
		IPv4: bundle.IPv4, IPv6: bundle.IPv6, MTU: bundle.MTU, DNS: bundle.DNS,
		ConfigRev: bundle.ConfigRev,
	}
	if err := st.Save(stateDir); err != nil {
		c.setErr(err)
		return "", err
	}
	c.setErr(nil)
	return c.Status(), nil
}

// Refresh 重新拉取服务端配置并更新本地状态（不重启 overlay）。
func (c *Client) Refresh() error {
	c.mu.Lock()
	stateDir := c.stateDir
	c.mu.Unlock()
	st, err := client.LoadState(stateDir)
	if err != nil {
		c.setErr(err)
		return err
	}
	api := client.New(st.Server, st.ClientID, st.Secret)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bundle, err := api.Config(ctx)
	if err != nil {
		c.setErr(err)
		return err
	}
	if err := client.ApplyBundle(stateDir, bundle); err != nil {
		c.setErr(err)
		return err
	}
	st.Name = bundle.Name
	st.Network = bundle.Network
	st.Cidr4 = bundle.Cidr4
	st.Cidr6 = bundle.Cidr6
	st.IPv4 = bundle.IPv4
	st.IPv6 = bundle.IPv6
	st.MTU = bundle.MTU
	st.DNS = bundle.DNS
	st.ConfigRev = bundle.ConfigRev
	if err := st.Save(stateDir); err != nil {
		c.setErr(err)
		return err
	}
	c.setErr(nil)
	return nil
}

// Start 用 Android VpnService 传入的 TUN fd 启动 overlay。
func (c *Client) Start(fd int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ov != nil {
		return fmt.Errorf("隧道已在运行")
	}
	st, err := client.LoadState(c.stateDir)
	if err != nil {
		c.lastErr = "未注册，请先 enroll"
		return fmt.Errorf("%s", c.lastErr)
	}
	cfg, err := os.ReadFile(filepath.Join(c.stateDir, "config.yml"))
	if err != nil {
		c.lastErr = "读取 Nebula 配置失败: " + err.Error()
		return fmt.Errorf("%s", c.lastErr)
	}
	caP, certP, keyP := overlay.PKIPaths(c.stateDir)
	cfg, err = overlay.RewritePKIPaths(cfg, caP, certP, keyP)
	if err != nil {
		c.lastErr = "改写 PKI 路径失败: " + err.Error()
		return fmt.Errorf("%s", c.lastErr)
	}

	// 服务端下发的数据面策略：优先 client.yaml，其次初次心跳。
	var hb *client.HeartbeatResult
	hbCtx, hbCancel := context.WithTimeout(context.Background(), 8*time.Second)
	if r, err := client.New(st.Server, st.ClientID, st.Secret).Heartbeat(hbCtx); err == nil {
		hb = r
	}
	hbCancel()
	fileCfg, _ := client.LoadFileConfig(client.DefaultFileConfigPath(c.stateDir))
	relay := client.ResolveRelay("", "", "", fileCfg, hb)
	if relay.Mode == "tcp" && relay.Addr == "" {
		c.lastErr = "服务端未下发 TCP 中继地址，无法使用 tcp 模式"
		return fmt.Errorf("%s", c.lastErr)
	}
	if relay.Mode != "udp" && relay.Addr != "" {
		bridgeCtx, bridgeCancel := context.WithCancel(context.Background())
		rewritten, rerr := tcprelay.RewriteConfigWithOptions(bridgeCtx, relay.Addr, relay.Token,
			tcprelay.RewriteOptions{ForceRelay: relay.Mode == "tcp"}, cfg)
		if rerr != nil {
			bridgeCancel()
			c.lastErr = "配置 TCP 中继失败: " + rerr.Error()
			return fmt.Errorf("%s", c.lastErr)
		}
		cfg = rewritten
		c.bridgeCancel = bridgeCancel
	}

	ov := overlay.New(mobileLogger())
	if err := ov.StartWithFD(cfg, fd); err != nil {
		if c.bridgeCancel != nil {
			c.bridgeCancel()
			c.bridgeCancel = nil
		}
		c.lastErr = "启动隧道失败: " + err.Error()
		return fmt.Errorf("%s", c.lastErr)
	}
	c.ov = ov
	c.hbCancel = c.startHeartbeat(st)
	c.lastErr = ""
	return nil
}

// Stop 停止 overlay 与心跳。
func (c *Client) Stop() error {
	c.mu.Lock()
	ov := c.ov
	c.ov = nil
	cancel := c.hbCancel
	c.hbCancel = nil
	bridgeCancel := c.bridgeCancel
	c.bridgeCancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if ov != nil {
		if err := ov.Stop(); err != nil {
			c.setErr(err)
			return err
		}
	}
	if bridgeCancel != nil {
		bridgeCancel()
	}
	c.setErr(nil)
	return nil
}

// Status 返回状态 JSON：enrolled/running/server/name/network/ipv4/ipv6/error。
func (c *Client) Status() string {
	c.mu.Lock()
	running := c.ov != nil
	server := c.server
	lastErr := c.lastErr
	stateDir := c.stateDir
	c.mu.Unlock()

	out := map[string]any{
		"enrolled": false, "running": running, "server": server,
		"name": "", "network": "", "ipv4": "", "ipv6": "", "dns": "", "mtu": 0, "error": lastErr,
	}
	if st, err := client.LoadState(stateDir); err == nil {
		out["enrolled"] = true
		out["server"] = st.Server
		out["name"] = st.Name
		out["network"] = st.Network
		out["ipv4"] = st.IPv4
		out["ipv6"] = st.IPv6
		out["dns"] = st.DNS
		out["mtu"] = st.MTU
	}
	if fc, err := client.LoadFileConfig(client.DefaultFileConfigPath(stateDir)); err == nil {
		out["data_plane_mode"] = fc.DataPlane.Mode
		out["tcp_relay"] = fc.DataPlane.TCPRelay
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// VPNParams 返回 Android VpnService 配置 JSON：
// ipv4/ipv4_prefix/ipv6/ipv6_prefix/mtu/routes/dns。
func (c *Client) VPNParams() (string, error) {
	c.mu.Lock()
	stateDir := c.stateDir
	c.mu.Unlock()
	st, err := client.LoadState(stateDir)
	if err != nil {
		return "", fmt.Errorf("未注册，请先 enroll")
	}
	if st.IPv4 == "" && st.IPv6 == "" {
		return "", fmt.Errorf("配置缺少客户端地址，请重新 enroll")
	}
	params := vpnParams{MTU: st.MTU}
	if params.MTU <= 0 {
		params.MTU = 1280
	}
	if st.IPv4 != "" {
		params.IPv4 = addrOnly(st.IPv4)
		if p, err := netip.ParsePrefix(st.Cidr4); err == nil {
			params.IPv4Prefix = p.Bits()
		}
		if params.IPv4Prefix == 0 && strings.Contains(st.Cidr4, "/") {
			params.IPv4Prefix = prefixBits(st.Cidr4)
		}
	}
	if st.IPv6 != "" {
		params.IPv6 = addrOnly(st.IPv6)
		if p, err := netip.ParsePrefix(st.Cidr6); err == nil {
			params.IPv6Prefix = p.Bits()
		}
	}
	seen := map[string]bool{}
	add := func(cidr string) {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" || seen[cidr] {
			return
		}
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return
		}
		seen[cidr] = true
		params.Routes = append(params.Routes, cidr)
	}
	add(st.Cidr4)
	add(st.Cidr6)
	// 不安全路由（服务端下发的 routes.json）。
	if b, err := os.ReadFile(filepath.Join(stateDir, "routes.json")); err == nil {
		var routes []model.RouteSpec
		if json.Unmarshal(b, &routes) == nil {
			for _, r := range routes {
				add(r.CIDR)
			}
		}
	}
	seenDNS := map[string]bool{}
	addDNS := func(dns string) {
		for _, d := range strings.Split(dns, ",") {
			if d = strings.TrimSpace(d); d != "" && !seenDNS[d] {
				seenDNS[d] = true
				params.DNS = append(params.DNS, d)
			}
		}
	}
	addDNS(st.DNS)
	// 兼容旧 bundle：state 无 DNS 时从 Nebula 配置的 dns.server 读取。
	if len(params.DNS) == 0 {
		if raw, err := os.ReadFile(filepath.Join(stateDir, "config.yml")); err == nil {
			var cfg struct {
				DNS *struct {
					Server string `yaml:"server"`
				} `yaml:"dns"`
			}
			if yaml.Unmarshal(raw, &cfg) == nil && cfg.DNS != nil {
				addDNS(cfg.DNS.Server)
			}
		}
	}
	if params.MTU <= 0 {
		if raw, err := os.ReadFile(filepath.Join(stateDir, "config.yml")); err == nil {
			var cfg struct {
				Tun struct {
					MTU int `yaml:"mtu"`
				} `yaml:"tun"`
			}
			if yaml.Unmarshal(raw, &cfg) == nil && cfg.Tun.MTU > 0 {
				params.MTU = cfg.Tun.MTU
			}
		}
	}
	b, _ := json.Marshal(params)
	return string(b), nil
}

// Logs 返回最近日志（最多约 300 行）。
func (c *Client) Logs() string { return mobileLogs.String() }

// Version 返回客户端版本。
func (c *Client) Version() string { return version.Version }

type vpnParams struct {
	IPv4       string   `json:"ipv4,omitempty"`
	IPv4Prefix int      `json:"ipv4_prefix,omitempty"`
	IPv6       string   `json:"ipv6,omitempty"`
	IPv6Prefix int      `json:"ipv6_prefix,omitempty"`
	MTU        int      `json:"mtu"`
	Routes     []string `json:"routes"`
	DNS        []string `json:"dns"`
}

func (c *Client) setErr(err error) {
	c.mu.Lock()
	if err == nil {
		c.lastErr = ""
	} else {
		c.lastErr = err.Error()
		log.Printf("❌ %v", err)
	}
	c.mu.Unlock()
}

func (c *Client) startHeartbeat(st *client.State) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	api := client.New(st.Server, st.ClientID, st.Secret)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hbCtx, hbCancel := context.WithTimeout(ctx, 15*time.Second)
				if _, err := api.Heartbeat(hbCtx); err != nil {
					log.Printf("⚠️ 心跳失败: %v", err)
				}
				hbCancel()
			}
		}
	}()
	return cancel
}

func normalizeServer(server string) string {
	server = strings.TrimSpace(server)
	if server == "" {
		return ""
	}
	if !strings.Contains(server, "://") {
		server = "https://" + server
	}
	return strings.TrimRight(server, "/")
}

func addrOnly(s string) string {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i]
	}
	return strings.TrimSpace(s)
}

func prefixBits(s string) int {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		var n int
		if _, err := fmt.Sscanf(s[i+1:], "%d", &n); err == nil {
			return n
		}
	}
	return 0
}

// 日志环：Android 端通过 Logs() 展示。
var mobileLogs = newRingBuffer(600)

var logOnce sync.Once

func initLogging() {
	logOnce.Do(func() {
		log.SetFlags(log.Ltime)
		log.SetOutput(io.MultiWriter(os.Stderr, mobileLogs))
	})
}

func mobileLogger() *slog.Logger {
	return slog.New(&ringHandler{buf: mobileLogs, level: slog.LevelInfo})
}

type ringHandler struct {
	buf   *ringBuffer
	level slog.Level
}

func (h *ringHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }
func (h *ringHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Time.Format("15:04:05"))
	b.WriteByte(' ')
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(a.Value.String())
		return true
	})
	h.buf.Add(b.String())
	return nil
}
func (h *ringHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *ringHandler) WithGroup(_ string) slog.Handler      { return h }

type ringBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newRingBuffer(max int) *ringBuffer { return &ringBuffer{max: max} }

func (r *ringBuffer) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			r.Add(line)
		}
	}
	return len(p), nil
}

func (r *ringBuffer) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	if len(r.lines) > r.max {
		r.lines = r.lines[len(r.lines)-r.max:]
	}
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := 0
	if len(r.lines) > 300 {
		start = len(r.lines) - 300
	}
	return strings.Join(r.lines[start:], "\n")
}
