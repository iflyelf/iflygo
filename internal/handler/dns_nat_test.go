package handler

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/iflyelf/iflygo/internal/model"
)

func TestBuildFullNATScript(t *testing.T) {
	net := &model.Network{Name: "home", Cidr4: "10.44.0.0/16", Cidr6: "fd00:44::/64"}
	script := buildFullNATScript(net, "10.44.0.1", "fd00:44::1", []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
	for _, want := range []string{
		"iptables -A FORWARD -s 10.44.0.0/16 -j ACCEPT",
		"iptables -t nat -A POSTROUTING -s 10.44.0.0/16 -d 10.0.0.0/8 -j RETURN",
		"iptables -t nat -A POSTROUTING -s 10.44.0.0/16 -j MASQUERADE",
		"ip6tables -t nat -A POSTROUTING -s fd00:44::/64 -j MASQUERADE",
		"# IPv4 网关: 10.44.0.1",
		"# IPv6 网关: fd00:44::1",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("全量出口脚本缺少 %q:\n%s", want, script)
		}
	}
}

func TestGatewayInNetwork(t *testing.T) {
	net := &model.Network{Cidr4: "10.44.0.0/16", Cidr6: "fd00:44::/64"}
	if !gatewayInNetwork(net, "10.44.0.1") {
		t.Fatal("overlay IPv4 网关应命中")
	}
	if !gatewayInNetwork(net, "fd00:44::1") {
		t.Fatal("overlay IPv6 网关应命中")
	}
	if gatewayInNetwork(net, "10.99.0.1") {
		t.Fatal("网段外地址不应命中")
	}
}

func TestBuildDNSNATScript(t *testing.T) {
	net := &model.Network{Name: "home", Cidr4: "10.44.0.0/16", Cidr6: "fd00:44::/64"}
	script := buildDNSNATScript(net, []string{"8.8.8.8"}, []string{"2001:4860:4860::8888"}, "10.44.0.1", "fd00:44::1")
	for _, want := range []string{
		"sysctl -w net.ipv4.ip_forward=1",
		"sysctl -w net.ipv6.conf.all.forwarding=1",
		"-s 10.44.0.0/16 -d 8.8.8.8/32 -j ACCEPT",
		"-s 10.44.0.0/16 -d 8.8.8.8/32 -j MASQUERADE",
		"ip6tables -C FORWARD -s fd00:44::/64 -d 2001:4860:4860::8888/128",
		"ip6tables -t nat -A POSTROUTING -s fd00:44::/64 -d 2001:4860:4860::8888/128 -j MASQUERADE",
		"# IPv4 网关: 10.44.0.1",
		"# IPv6 网关: fd00:44::1",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("脚本缺少 %q:\n%s", want, script)
		}
	}
}
