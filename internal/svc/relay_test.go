package svc

import (
	"context"
	"testing"

	"github.com/iflyelf/iflygo/internal/config"
)

func TestRelayRuntimeApply(t *testing.T) {
	c := &config.Config{Server: config.ServerConfig{Host: "127.0.0.1", Port: 8090}}
	r := NewRelayRuntime(c)
	ctx := context.Background()
	defer r.Stop()

	if err := r.Apply(ctx, "auto", "127.0.0.1:0", "relay.example.com:8443", "tok1"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if r.Port() == 0 {
		t.Fatalf("应已监听并分配端口")
	}
	if r.Mode() != "auto" || r.Token() != "tok1" || r.Advertise() != "relay.example.com:8443" {
		t.Fatalf("unexpected: mode=%s token=%s adv=%s", r.Mode(), r.Token(), r.Advertise())
	}
	firstPort := r.Port()

	// 地址与令牌都未变：复用同一监听（端口不变）。
	if err := r.Apply(ctx, "auto", "127.0.0.1:0", "", "tok1"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if r.Port() != firstPort {
		t.Fatalf("未变化时应复用监听: %d -> %d", firstPort, r.Port())
	}

	// 令牌变化：重新监听并应用新令牌。
	if err := r.Apply(ctx, "auto", "127.0.0.1:0", "", "tok2"); err != nil {
		t.Fatalf("Apply(token): %v", err)
	}
	if r.Token() != "tok2" || r.Port() == 0 {
		t.Fatalf("令牌应更新且监听保持可用: %s %d", r.Token(), r.Port())
	}

	// tcp 模式仍监听，供客户端强制中继。
	if err := r.Apply(ctx, "tcp", "127.0.0.1:0", "", "tok2"); err != nil {
		t.Fatalf("Apply(tcp): %v", err)
	}
	if r.Mode() != "tcp" || r.Port() == 0 {
		t.Fatalf("tcp 模式应可用: %s %d", r.Mode(), r.Port())
	}

	// udp 模式：关闭监听。
	if err := r.Apply(ctx, "udp", "127.0.0.1:0", "", "tok2"); err != nil {
		t.Fatalf("Apply(udp): %v", err)
	}
	if r.Port() != 0 || r.Mode() != "udp" {
		t.Fatalf("udp 模式应关闭监听: %s %d", r.Mode(), r.Port())
	}

	// 显式 off：保持关闭。
	if err := r.Apply(ctx, "auto", "127.0.0.1:0", "", "tok2"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := r.Apply(ctx, "auto", "off", "", "tok2"); err != nil {
		t.Fatalf("Apply(off): %v", err)
	}
	if r.Port() != 0 {
		t.Fatalf("off 应关闭监听")
	}
}

func TestRelayRuntimeDefaultListen(t *testing.T) {
	// 留空 listen 时按控制面端口 +1 推导；端口占用时返回错误而不是 panic。
	c := &config.Config{Server: config.ServerConfig{Host: "127.0.0.1", Port: 39090}}
	r := NewRelayRuntime(c)
	defer r.Stop()
	if err := r.Apply(context.Background(), "auto", "", "", ""); err == nil {
		// 端口可能空闲，成功则校验端口为控制面端口 +1
		if r.Port() != 39091 {
			t.Fatalf("默认端口应为 39091，got %d", r.Port())
		}
	}
}
