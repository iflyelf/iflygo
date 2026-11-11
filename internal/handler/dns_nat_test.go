package handler

import (
	"strings"
	"testing"

	"github.com/iflyelf/iflygo/internal/model"
)

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
