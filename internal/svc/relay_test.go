package svc

import (
	"context"
	"testing"
)

func TestRelayRuntimeApply(t *testing.T) {
	r := NewRelayRuntime()
	ctx := context.Background()
	defer r.Stop()

	if err := r.Apply(ctx, "auto", "127.0.0.1:0", "relay.example.com:8443", "tok1"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if r.Port() == 0 {
		t.Fatalf("应已监听并分配端口")
	}
	info := r.Info()
	if info.Mode != "auto" || info.Token != "tok1" || info.Advertise != "relay.example.com:8443" {
		t.Fatalf("unexpected: %+v", info)
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

func TestDefaultRelayListen(t *testing.T) {
	if got := defaultRelayListen("127.0.0.1", 8090); got != "127.0.0.1:8091" {
		t.Fatalf("got %s", got)
	}
	if got := defaultRelayListen("", 9000); got != "0.0.0.0:9001" {
		t.Fatalf("got %s", got)
	}
}
