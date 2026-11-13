package handler

import (
	"strings"
	"testing"

	"github.com/iflyelf/iflygo/internal/model"
)

func TestNormalizeNetworkReq(t *testing.T) {
	ok := networkReq{
		Name: "home", Cidr4: "10.77.1.1/16", Cidr6: "fd00:77::1/64",
		Gateway4: "10.77.0.1", Gateway6: "fd00:77::1",
		DNS: "10.77.0.1, 1.1.1.1", MTU: 1280,
		DNSGateway: "10.77.0.1", EgressMode: "FULL",
		EgressExclude: "192.168.0.0/16",
	}
	if err := normalizeNetworkReq(&ok); err != nil {
		t.Fatalf("合法网络不应失败: %v", err)
	}
	if ok.Cidr4 != "10.77.0.0/16" || ok.Cidr6 != "fd00:77::/64" || ok.EgressMode != "full" {
		t.Fatalf("网段未归一化: %+v", ok)
	}

	cases := []struct {
		name string
		req  networkReq
	}{
		{"非法 cidr4", networkReq{Name: "x", Cidr4: "10.0.0.0/33", MTU: 1280}},
		{"cidr6 类型错误", networkReq{Name: "x", Cidr4: "10.0.0.0/16", Cidr6: "10.1.0.0/16", MTU: 1280}},
		{"网关不在网段", networkReq{Name: "x", Cidr4: "10.0.0.0/16", Gateway4: "10.9.0.1", MTU: 1280}},
		{"名称换行注入", networkReq{Name: "x\ntouch /tmp/pwn", Cidr4: "10.0.0.0/16", MTU: 1280}},
		{"DNS 非法", networkReq{Name: "x", Cidr4: "10.0.0.0/16", DNS: "1.1.1.1; rm -rf /", MTU: 1280}},
		{"出口模式非法", networkReq{Name: "x", Cidr4: "10.0.0.0/16", EgressMode: "all", MTU: 1280}},
		{"排除网段非法", networkReq{Name: "x", Cidr4: "10.0.0.0/16", EgressExclude: "10.0.0.0/8\nrm", MTU: 1280}},
	}
	for _, tc := range cases {
		if err := normalizeNetworkReq(&tc.req); err == nil {
			t.Fatalf("%s 应被拒绝", tc.name)
		}
	}
}

func TestBuildFullNATScriptSanitizesInput(t *testing.T) {
	net := &model.Network{
		Name:  "evil\ntouch /tmp/pwn",
		Cidr4: "10.0.0.0/8\nrm -rf /",
	}
	script := buildFullNATScript(net, "10.0.0.1", "", nil)
	if strings.Contains(script, "\ntouch /tmp/pwn") || strings.Contains(script, "\nrm -rf /") {
		t.Fatalf("脚本不应包含注入命令:\n%s", script)
	}
}

func TestEnrollCommandQuotesShellArgs(t *testing.T) {
	cmd := enrollCommand("https://x'; touch /tmp/pwn; '", "tok", "name'; touch /tmp/pwn; '")
	if strings.Contains(cmd, "; touch /tmp/pwn; '") && !strings.Contains(cmd, `'\''`) {
		t.Fatalf("安装命令未正确转义: %s", cmd)
	}
	if !strings.HasPrefix(cmd, "iflygo-client enroll --server '") {
		t.Fatalf("安装命令参数应单引号转义: %s", cmd)
	}
}

func TestNormalizeRouteReq(t *testing.T) {
	net := &model.Network{Cidr4: "10.44.0.0/16", Cidr6: "fd00:44::/64"}
	ok := createRouteReq{
		CIDR: "10.9.0.0/24", MTU: 1300, Mode: "active_active",
		Gateways: []gatewayReq{{OverlayIP: "10.44.0.1", Weight: 0}},
	}
	if err := normalizeRouteReq(net, &ok); err != nil {
		t.Fatalf("合法路由失败: %v", err)
	}
	if ok.Gateways[0].Weight != 1 {
		t.Fatalf("权重 0 应归一化为 1: %+v", ok.Gateways[0])
	}
	bad := []createRouteReq{
		{CIDR: "10.9.0.0/33", Gateways: []gatewayReq{{OverlayIP: "10.44.0.1"}}},
		{CIDR: "10.9.0.0/24", Gateways: []gatewayReq{{OverlayIP: "10.99.0.1"}}},
		{CIDR: "10.9.0.0/24", Mode: "bogus", Gateways: []gatewayReq{{OverlayIP: "10.44.0.1"}}},
		{CIDR: "10.9.0.0/24", AllDownPolicy: "bogus", Gateways: []gatewayReq{{OverlayIP: "10.44.0.1"}}},
	}
	for i := range bad {
		if err := normalizeRouteReq(net, &bad[i]); err == nil {
			t.Fatalf("非法路由 #%d 应被拒绝: %+v", i, bad[i])
		}
	}
}
