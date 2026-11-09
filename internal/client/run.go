package client

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/overlay"
	"github.com/iflyelf/iflygo/internal/tcprelay"
)

// Run 启动客户端主循环：心跳 + 配置热更新 + overlay。
//
// tcpRelay 非空时（如 "relay.example.com:8443"），会在启动/热加载 overlay 前
// 把 Nebula 配置里的 static_host_map 改写为本地 UDP 桥，桥经 TCP 连到中继服务端，
// 用于 UDP 被完全封锁的网络。
func Run(dir string, interval time.Duration, tcpRelay, tcpRelayToken string) error {
	st, err := LoadState(dir)
	if err != nil {
		return errors.New("未找到状态文件，请先执行 enroll")
	}
	c := New(st.Server, st.ClientID, st.Secret)
	ov := overlay.New(slog.Default())

	log.Printf("🚀 iflygo-client 运行中: #%d %s (%s) 服务器 %s", st.ClientID, st.Name, st.IPv4, st.Server)
	if tcpRelay != "" {
		log.Printf("🧵 TCP 中继已启用: %s（overlay 流量将经 TCP 承载）", tcpRelay)
	}

	// TCP 中继桥的生命周期与 overlay 一致：热加载时重建，退出时关闭。
	bridges := newBridgeScope()
	defer func() { bridges.Close() }()

	// 首次拉取配置（含证书与 Nebula 配置）
	if err := fetchAndApply(c, st, dir); err != nil {
		log.Printf("⚠️ 拉取配置失败: %v", err)
	}
	// 尝试启动 overlay（需要 TUN 设备与 NET_ADMIN）
	if err := startOverlay(ov, dir, tcpRelay, tcpRelayToken, bridges.ctx); err != nil {
		log.Printf("⚠️ 启动 overlay 失败（需要 root/TUN）: %v", err)
		// overlay 未启动，桥也无用，回收后换一个新作用域供后续热加载使用。
		bridges.Close()
		bridges = newBridgeScope()
	}
	defer ov.Stop()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 不安全路由健康管理（探测网关 → 动态热加载）
	rm := NewRouteManager(dir, ov)
	if err := rm.Load(); err != nil {
		log.Printf("⚠️ 加载路由失败: %v", err)
	}
	go rm.Run(ctx, interval)

	hb := time.NewTicker(interval)
	defer hb.Stop()
	peers := time.NewTicker(15 * time.Second)
	defer peers.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("正在退出...")
			return nil
		case <-hb.C:
			rev, err := c.Heartbeat(ctx)
			if err != nil {
				log.Printf("⚠️ 心跳失败: %v", err)
				continue
			}
			if rev != st.ConfigRev {
				if err := fetchAndApply(c, st, dir); err != nil {
					log.Printf("⚠️ 更新配置失败: %v", err)
				} else {
					// 用新配置重建 TCP 桥；成功后再回收旧桥，失败则保留旧桥运行。
					next := newBridgeScope()
					if err := reloadOverlay(ov, dir, tcpRelay, tcpRelayToken, next.ctx); err != nil {
						next.Close()
						log.Printf("⚠️ 热加载失败: %v", err)
					} else {
						bridges.Close()
						bridges = next
					}
				}
			}
		case <-peers.C:
			for _, p := range ov.Peers() {
				log.Printf("🔗 隧道: %s <-> %s (relay=%v)", p.VPNIP, p.Remote, p.Relays)
			}
		}
	}
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
	st.Network = bundle.Network
	st.IPv4 = bundle.IPv4
	st.IPv6 = bundle.IPv6
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
	log.Printf("✅ 配置已应用到 %s", dir)
	return nil
}

func startOverlay(ov *overlay.Overlay, dir, tcpRelay, tcpRelayToken string, bridgeCtx context.Context) error {
	cfg, err := readNebulaConfig(dir)
	if err != nil {
		return err
	}
	cfg, err = rewriteForRelay(bridgeCtx, tcpRelay, tcpRelayToken, cfg)
	if err != nil {
		return err
	}
	return ov.Start(cfg)
}

func reloadOverlay(ov *overlay.Overlay, dir, tcpRelay, tcpRelayToken string, bridgeCtx context.Context) error {
	cfg, err := readNebulaConfig(dir)
	if err != nil {
		return err
	}
	cfg, err = rewriteForRelay(bridgeCtx, tcpRelay, tcpRelayToken, cfg)
	if err != nil {
		return err
	}
	return ov.Reload(cfg)
}

// rewriteForRelay 在启用 TCP 中继时改写 Nebula 配置中的 static_host_map。
// tcpRelay 为空则原样返回（保持原生 UDP 行为）。
func rewriteForRelay(ctx context.Context, tcpRelay, tcpRelayToken string, cfg []byte) ([]byte, error) {
	if tcpRelay == "" {
		return cfg, nil
	}
	return tcprelay.RewriteConfig(ctx, tcpRelay, tcpRelayToken, cfg)
}

func readNebulaConfig(dir string) ([]byte, error) {
	cfg, err := os.ReadFile(filepath.Join(dir, "config.yml"))
	if err != nil {
		return nil, err
	}
	caP, certP, keyP := overlay.PKIPaths(dir)
	return overlay.RewritePKIPaths(cfg, caP, certP, keyP)
}
