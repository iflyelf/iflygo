package setting

import (
	"testing"

	"github.com/iflyelf/iflygo/internal/config"
)

func TestRelayRegistry(t *testing.T) {
	c := config.Default()
	mode, ok := registryMap["tcp_relay.mode"]
	if !ok {
		t.Fatalf("缺少 tcp_relay.mode")
	}
	if got := mode.Get(c); got != "auto" {
		t.Fatalf("默认模式应为 auto，got %s", got)
	}
	if err := mode.Validate("tcp"); err != nil {
		t.Fatalf("tcp 应合法: %v", err)
	}
	if err := mode.Validate("nope"); err == nil {
		t.Fatalf("非法模式应被拒绝")
	}
	mode.Set(c, "tcp")
	if c.TCPRelay.Mode != "tcp" {
		t.Fatalf("Set 未生效: %s", c.TCPRelay.Mode)
	}

	listen := registryMap["tcp_relay.listen"]
	for _, v := range []string{"", "off", ":8443", "0.0.0.0:8443"} {
		if err := listen.Validate(v); err != nil {
			t.Fatalf("listen %q 应合法: %v", v, err)
		}
	}
	if err := listen.Validate("not a listen addr"); err == nil {
		t.Fatalf("非法监听地址应被拒绝")
	}

	adv := registryMap["tcp_relay.advertise"]
	if err := adv.Validate("relay.example.com:8443"); err != nil {
		t.Fatalf("advertise 应合法: %v", err)
	}
	if err := adv.Validate("bad addr/x"); err == nil {
		t.Fatalf("非法 advertise 应被拒绝")
	}

	if !NeedsTCPRelayReload([]string{"tcp_relay.mode"}) {
		t.Fatalf("应触发中继热更新")
	}
	if NeedsTCPRelayReload([]string{"server.external_url"}) {
		t.Fatalf("无关设置不应触发中继热更新")
	}
}
