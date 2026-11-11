package handler

import (
	"net/netip"
	"testing"

	"github.com/iflyelf/iflygo/internal/config"
	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/svc"
)

func TestSubtractPrefixes(t *testing.T) {
	base := netip.MustParsePrefix("0.0.0.0/0")
	excludes := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.0.0/16"),
	}
	out := subtractPrefixes(base, excludes)
	if len(out) == 0 {
		t.Fatal("扣除后不应为空")
	}
	const publicIP = "8.8.8.8"
	const privateIP = "10.1.2.3"
	if !prefixListContains(out, netip.MustParseAddr(publicIP)) {
		t.Fatalf("公网地址 %s 应仍在路由集合内: %v", publicIP, out)
	}
	if prefixListContains(out, netip.MustParseAddr(privateIP)) {
		t.Fatalf("内网地址 %s 不应出现在路由集合: %v", privateIP, out)
	}
}

func TestFullEgressRoutesExcludesPrivate(t *testing.T) {
	cfg := config.Default()
	cfg.Server.ExternalURL = "https://127.0.0.1:8443"
	sc := &svc.ServiceContext{Config: cfg}
	net := &model.Network{Name: "n", Cidr4: "10.44.0.0/16", MTU: 1300}
	cfgRoutes, specs := fullEgressRoutes(sc, net, "10.44.0.1", "")
	if len(cfgRoutes) == 0 || len(specs) == 0 {
		t.Fatal("全量出口应生成路由")
	}
	for _, r := range cfgRoutes {
		if r.Route == "0.0.0.0/0" {
			t.Fatal("不应下发单一 0/0 路由")
		}
		p := netip.MustParsePrefix(r.Route)
		for _, ex := range []string{"10.0.0.0/8", "192.168.0.0/16", "10.44.0.0/16"} {
			if p.Overlaps(netip.MustParsePrefix(ex)) {
				t.Fatalf("路由 %s 与排除网段 %s 重叠", r.Route, ex)
			}
		}
	}
}

func prefixListContains(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
