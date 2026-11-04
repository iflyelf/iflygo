package client

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/iflyelf/iflygo/internal/model"
)

// Run 启动客户端主循环：心跳 + 配置热更新。
func Run(dir string, interval time.Duration) error {
	st, err := LoadState(dir)
	if err != nil {
		return errors.New("未找到状态文件，请先执行 enroll")
	}
	c := New(st.Server, st.ClientID, st.Secret)

	log.Printf("🚀 iflygo-client 运行中: #%d %s (%s) 服务器 %s", st.ClientID, st.Name, st.IPv4, st.Server)

	// 首次拉取配置
	if err := fetchAndApply(c, st, dir); err != nil {
		log.Printf("⚠️ 拉取配置失败: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("正在退出...")
			return nil
		case <-ticker.C:
			rev, err := c.Heartbeat(ctx)
			if err != nil {
				log.Printf("⚠️ 心跳失败: %v", err)
				continue
			}
			if rev != st.ConfigRev {
				if err := fetchAndApply(c, st, dir); err != nil {
					log.Printf("⚠️ 更新配置失败: %v", err)
				}
			}
		}
	}
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

// ApplyBundle 将配置包写入状态目录（供 overlay 使用）。
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
	log.Printf("✅ 配置已应用到 %s", dir)
	return nil
}
