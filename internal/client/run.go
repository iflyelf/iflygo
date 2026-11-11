package client

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/overlay"
	"github.com/iflyelf/iflygo/internal/tcprelay"
	"gopkg.in/yaml.v3"
)

// RunOptions 客户端运行参数。
//
// 优先级：命令行参数 > 配置文件（默认 <state-dir>/client.yaml，支持热加载）
// > 服务端心跳下发 > 内置默认。
type RunOptions struct {
	Dir           string
	Interval      time.Duration // <=0 时使用配置文件/默认 30s
	Mode          string        // auto / tcp / udp；空则由配置文件/服务端决定
	TCPRelay      string        // 中继地址；"off" 等价 udp；空则自动发现
	TCPRelayToken string
	ConfigPath    string // 空则 <state-dir>/client.yaml
}

// Run 启动客户端主循环：心跳 + 配置热更新 + overlay + 客户端配置文件热加载。
func Run(opts RunOptions) error {
	dir := opts.Dir
	st, err := LoadState(dir)
	if err != nil {
		return errors.New("未找到状态文件，请先执行 enroll")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		cfgPath = DefaultFileConfigPath(dir)
	}
	fileCfg, err := LoadFileConfig(cfgPath)
	if err != nil {
		log.Printf("⚠️ %v（改用默认/服务端下发配置）", err)
		fileCfg = &FileConfig{}
	}

	interval := opts.Interval
	if interval <= 0 {
		interval = fileCfg.IntervalDuration(30 * time.Second)
	}

	c := New(st.Server, st.ClientID, st.Secret)
	ov := overlay.New(slog.Default())

	log.Printf("🚀 iflygo-client 运行中: #%d %s (%s) 服务器 %s", st.ClientID, st.Name, st.IPv4, st.Server)
	log.Printf("⚙️ 客户端配置: %s（支持热加载）", cfgPath)

	// 首次心跳获取服务端下发的数据面策略，再拉取配置。
	hbInfo, hbErr := c.Heartbeat(ctx)
	if hbErr != nil {
		log.Printf("⚠️ 心跳失败: %v", hbErr)
	}
	if err := fetchAndApply(c, st, dir); err != nil {
		log.Printf("⚠️ 拉取配置失败: %v", err)
	} else if fc, lerr := LoadFileConfig(cfgPath); lerr == nil {
		// ApplyBundle 可能刚写入服务端下发的 client.yaml，立即生效。
		fileCfg = fc
		if opts.Interval <= 0 {
			if d := fc.IntervalDuration(30 * time.Second); d > 0 {
				interval = d
			}
		}
	}

	relay := ResolveRelay(opts.Mode, opts.TCPRelay, opts.TCPRelayToken, fileCfg, hbInfo)
	logRelay(relay)

	// TCP 中继桥的生命周期与 overlay 一致：热加载时重建，退出时关闭。
	bridges := newBridgeScope()
	defer func() { bridges.Close() }()

	// 尝试启动 overlay（需要 TUN 设备与 NET_ADMIN）
	if err := startOverlay(ov, dir, relay, bridges.ctx); err != nil {
		log.Printf("⚠️ 启动 overlay 失败（需要 root/TUN）: %v", err)
		// overlay 未启动，桥也无用，回收后换一个新作用域供后续热加载使用。
		bridges.Close()
		bridges = newBridgeScope()
	}
	defer ov.Stop()

	// 不安全路由健康管理（探测网关 → 动态热加载）
	rm := NewRouteManager(dir, ov)
	if err := rm.Load(); err != nil {
		log.Printf("⚠️ 加载路由失败: %v", err)
	}
	go rm.Run(ctx, interval)

	hbTicker := time.NewTicker(interval)
	defer hbTicker.Stop()
	peersTicker := time.NewTicker(15 * time.Second)
	defer peersTicker.Stop()
	cfgTicker := time.NewTicker(2 * time.Second)
	defer cfgTicker.Stop()

	cfgDigest := fileDigest(cfgPath)

	// restartRelay 用新的数据面策略重建桥并重启 overlay。
	restartRelay := func(next RelaySettings) {
		if next == relay {
			return
		}
		scope := newBridgeScope()
		if err := restartOverlay(ov, dir, next, scope.ctx); err != nil {
			scope.Close()
			bridges.Close() // overlay 已停止，旧桥不再有用，回收避免泄漏
			log.Printf("⚠️ 切换数据面模式失败: %v", err)
			return
		}
		bridges.Close()
		bridges = scope
		relay = next
		if err := rm.Reapply(); err != nil {
			log.Printf("⚠️ 重新应用不安全路由失败: %v", err)
		}
		logRelay(relay)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("正在退出...")
			return nil
		case <-hbTicker.C:
			hb, err := c.Heartbeat(ctx)
			if err != nil {
				log.Printf("⚠️ 心跳失败: %v", err)
				continue
			}
			hbInfo = hb
			configChanged := hb.ConfigRev != st.ConfigRev
			oldIPv4, oldIPv6 := st.IPv4, st.IPv6
			if configChanged {
				if err := fetchAndApply(c, st, dir); err != nil {
					log.Printf("⚠️ 更新配置失败: %v", err)
					continue
				}
				// ApplyBundle 会重写 client.yaml；立即重新加载，避免继续使用旧参数。
				if fc, lerr := LoadFileConfig(cfgPath); lerr == nil {
					fileCfg = fc
					if opts.Interval <= 0 {
						if d := fc.IntervalDuration(0); d > 0 && d != interval {
							interval = d
							hbTicker.Reset(interval)
							log.Printf("🔄 客户端心跳间隔已更新为 %s", interval)
						}
					}
				}
			}
			next := ResolveRelay(opts.Mode, opts.TCPRelay, opts.TCPRelayToken, fileCfg, hb)
			if next != relay {
				restartRelay(next)
				continue
			}
			if configChanged {
				// 用新配置重建 TCP 桥；成功后再回收旧桥，失败则保留旧桥运行。
				scope := newBridgeScope()
				// 地址/证书变化时重启 overlay，确保 Nebula 使用新的 IP 与证书；
				// 其余参数变化用热加载即可。
				var rerr error
				if st.IPv4 != oldIPv4 || st.IPv6 != oldIPv6 {
					rerr = restartOverlay(ov, dir, relay, scope.ctx)
				} else {
					rerr = reloadOverlay(ov, dir, relay, scope.ctx)
				}
				if rerr != nil {
					scope.Close()
					log.Printf("⚠️ 热加载失败: %v", rerr)
				} else {
					bridges.Close()
					bridges = scope
					if err := rm.Reapply(); err != nil {
						log.Printf("⚠️ 重新应用不安全路由失败: %v", err)
					}
				}
			}
		case <-cfgTicker.C:
			digest := fileDigest(cfgPath)
			if digest == cfgDigest {
				continue
			}
			cfgDigest = digest
			next, err := LoadFileConfig(cfgPath)
			if err != nil {
				log.Printf("⚠️ 客户端配置热加载失败: %v", err)
				continue
			}
			fileCfg = next
			if d := next.IntervalDuration(0); d > 0 && d != interval {
				interval = d
				hbTicker.Reset(interval)
				log.Printf("🔄 客户端心跳间隔已更新为 %s", interval)
			}
			restartRelay(ResolveRelay(opts.Mode, opts.TCPRelay, opts.TCPRelayToken, fileCfg, hbInfo))
			log.Printf("✅ 客户端配置已热加载: %s", cfgPath)
		case <-peersTicker.C:
			for _, p := range ov.Peers() {
				log.Printf("🔗 隧道: %s <-> %s (relay=%v)", p.VPNIP, p.Remote, p.Relays)
			}
		}
	}
}

// logRelay 输出当前数据面策略。
func logRelay(r RelaySettings) {
	switch r.Mode {
	case "tcp":
		if r.Addr == "" {
			log.Printf("⚠️ 数据面=tcp，但未获取到中继地址，暂按 UDP 运行")
			return
		}
		log.Printf("🧵 数据面=tcp（始终经 TCP 中继 %s）", r.Addr)
	case "udp":
		log.Printf("📡 数据面=udp（仅原生 UDP）")
	default:
		if r.Addr == "" {
			log.Printf("🧠 数据面=auto（未发现 TCP 中继，仅 UDP）")
			return
		}
		log.Printf("🧠 数据面=auto（UDP 优先，%s 自动兜底 TCP）", r.Addr)
	}
}

// fileDigest 返回文件内容摘要；不存在返回空串。
//
// 用内容而非 mtime 判断变化：避免同一秒内连续保存（部分文件系统 mtime 精度为 1s）
// 或「大小不变但内容变化」时漏掉热加载。
func fileDigest(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := fnv.New64a()
	_, _ = h.Write(b)
	return strconv.FormatUint(h.Sum64(), 16) + ":" + strconv.Itoa(len(b))
}

// bridgeScope 管理一组 TCP 中继桥的生命周期（一个 overlay 配置对应一组桥）。
type bridgeScope struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func newBridgeScope() *bridgeScope {
	ctx, cancel := context.WithCancel(context.Background())
	return &bridgeScope{ctx: ctx, cancel: cancel}
}

// Close 关闭该作用域下的所有桥（幂等）。
func (b *bridgeScope) Close() {
	if b == nil || b.cancel == nil {
		return
	}
	b.cancel()
	b.cancel = nil
}

// FetchAndApply 拉取并应用配置包。
func FetchAndApply(dir string) error {
	st, err := LoadState(dir)
	if err != nil {
		return errors.New("未找到状态文件，请先执行 enroll")
	}
	c := New(st.Server, st.ClientID, st.Secret)
	return fetchAndApply(c, st, dir)
}

func fetchAndApply(c *Client, st *State, dir string) error {
	bundle, err := c.Config(context.Background())
	if err != nil {
		return err
	}
	if err := ApplyBundle(dir, bundle); err != nil {
		return err
	}
	st.ConfigRev = bundle.ConfigRev
	st.Name = bundle.Name
	st.Network = bundle.Network
	st.Cidr4 = bundle.Cidr4
	st.Cidr6 = bundle.Cidr6
	st.IPv4 = bundle.IPv4
	st.IPv6 = bundle.IPv6
	st.MTU = bundle.MTU
	st.DNS = bundle.DNS
	return st.Save(dir)
}

// ApplyBundle 将配置包写入状态目录（证书 + Nebula 配置 + 路由描述）。
func ApplyBundle(dir string, b *model.Bundle) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	files := map[string]struct {
		data string
		mode os.FileMode
	}{
		"ca.crt":     {b.CA, 0o644},
		"node.crt":   {b.Cert, 0o644},
		"node.key":   {b.Key, 0o600},
		"config.yml": {b.Config, 0o600},
	}
	for name, f := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(f.data), f.mode); err != nil {
			return err
		}
	}
	if len(b.Routes) > 0 {
		routesJSON, err := json.MarshalIndent(b.Routes, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "routes.json"), routesJSON, 0o600); err != nil {
			return err
		}
	} else {
		_ = os.Remove(filepath.Join(dir, "routes.json"))
	}
	// 服务端下发的客户端运行参数写入 client.yaml（数据面模式/中继/心跳）。
	// 这样 enroll 后客户端无需手工维护配置。
	if b.Client.DataPlaneMode != "" || b.Client.Interval != "" ||
		b.Client.TCPRelay != "" || b.Client.TCPRelayToken != "" {
		fc := FileConfig{
			Interval: b.Client.Interval,
			DataPlane: DataPlaneConfig{
				Mode:          b.Client.DataPlaneMode,
				TCPRelay:      b.Client.TCPRelay,
				TCPRelayToken: b.Client.TCPRelayToken,
			},
		}
		data, err := yaml.Marshal(fc)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "client.yaml"), data, 0o600); err != nil {
			return err
		}
	}
	log.Printf("✅ 配置已应用到 %s", dir)
	return nil
}

func startOverlay(ov *overlay.Overlay, dir string, relay RelaySettings, bridgeCtx context.Context) error {
	cfg, err := readNebulaConfig(dir)
	if err != nil {
		return err
	}
	cfg, err = rewriteForRelay(bridgeCtx, relay, cfg)
	if err != nil {
		return err
	}
	return ov.Start(cfg)
}

func reloadOverlay(ov *overlay.Overlay, dir string, relay RelaySettings, bridgeCtx context.Context) error {
	cfg, err := readNebulaConfig(dir)
	if err != nil {
		return err
	}
	cfg, err = rewriteForRelay(bridgeCtx, relay, cfg)
	if err != nil {
		return err
	}
	return ov.Reload(cfg)
}

// restartOverlay 数据面模式变化时重启 overlay（static_host_map 变更用重启最稳妥）。
func restartOverlay(ov *overlay.Overlay, dir string, relay RelaySettings, bridgeCtx context.Context) error {
	_ = ov.Stop()
	return startOverlay(ov, dir, relay, bridgeCtx)
}

// rewriteForRelay 按数据面模式改写 Nebula 配置：
//   - udp：不改写，保持原生 UDP；
//   - auto：改写为本地桥（UDP 优先，自动兜底 TCP）；
//   - tcp：改写为本地桥并强制经 TCP 中继。
func rewriteForRelay(ctx context.Context, relay RelaySettings, cfg []byte) ([]byte, error) {
	if relay.Mode == "udp" || relay.Addr == "" {
		return cfg, nil
	}
	return tcprelay.RewriteConfigWithOptions(ctx, relay.Addr, relay.Token, tcprelay.RewriteOptions{
		ForceRelay: relay.Mode == "tcp",
	}, cfg)
}

func readNebulaConfig(dir string) ([]byte, error) {
	cfg, err := os.ReadFile(filepath.Join(dir, "config.yml"))
	if err != nil {
		return nil, err
	}
	caP, certP, keyP := overlay.PKIPaths(dir)
	return overlay.RewritePKIPaths(cfg, caP, certP, keyP)
}
