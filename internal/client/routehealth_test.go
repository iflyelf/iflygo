package client

import (
	"testing"

	"github.com/iflyelf/iflygo/internal/configgen"
	"github.com/iflyelf/iflygo/internal/model"
)

func gw(ip string, w, p int) model.GatewaySpec {
	return model.GatewaySpec{IP: ip, Weight: w, Priority: p}
}

func TestEffectiveActiveActiveRemoveDown(t *testing.T) {
	specs := []model.RouteSpec{{
		CIDR: "10.8.1.0/24", Mode: "active_active", AllDownPolicy: "fail_closed",
		Gateways: []model.GatewaySpec{gw("10.88.1.1", 10, 0), gw("10.88.2.1", 5, 0)},
	}}
	health := map[string]bool{"10.88.1.1": true, "10.88.2.1": false}

	routes := EffectiveUnsafeRoutes(specs, health)
	if len(routes) != 1 {
		t.Fatalf("want 1 route, got %d", len(routes))
	}
	via, ok := routes[0].Via.(string)
	if !ok || via != "10.88.1.1" {
		t.Fatalf("want single via 10.88.1.1, got %#v", routes[0].Via)
	}
}

func TestEffectiveActiveActiveBothUp(t *testing.T) {
	specs := []model.RouteSpec{{
		CIDR: "10.8.1.0/24", Mode: "active_active", AllDownPolicy: "fail_closed",
		Gateways: []model.GatewaySpec{gw("10.88.1.1", 10, 0), gw("10.88.2.1", 5, 0)},
	}}
	routes := EffectiveUnsafeRoutes(specs, map[string]bool{"10.88.1.1": true, "10.88.2.1": true})
	via, ok := routes[0].Via.([]configgen.Gateway)
	if !ok || len(via) != 2 {
		t.Fatalf("want 2 gateways, got %#v", routes[0].Via)
	}
}

func TestEffectiveActivePassiveFailover(t *testing.T) {
	specs := []model.RouteSpec{{
		CIDR: "10.8.1.0/24", Mode: "active_passive", AllDownPolicy: "fail_closed",
		Gateways: []model.GatewaySpec{gw("10.88.1.1", 10, 0), gw("10.88.2.1", 5, 1)},
	}}
	// 主网关 down，应切换到 priority=1 的备用
	health := map[string]bool{"10.88.1.1": false, "10.88.2.1": true}
	routes := EffectiveUnsafeRoutes(specs, health)
	via, ok := routes[0].Via.(string)
	if !ok || via != "10.88.2.1" {
		t.Fatalf("want backup 10.88.2.1, got %#v", routes[0].Via)
	}
}

func TestEffectiveAllDownFailClosed(t *testing.T) {
	specs := []model.RouteSpec{{
		CIDR: "10.8.1.0/24", Mode: "active_active", AllDownPolicy: "fail_closed",
		Gateways: []model.GatewaySpec{gw("10.88.1.1", 10, 0), gw("10.88.2.1", 5, 0)},
	}}
	routes := EffectiveUnsafeRoutes(specs, map[string]bool{"10.88.1.1": false, "10.88.2.1": false})
	if len(routes) != 1 {
		t.Fatalf("fail_closed 应保留路由，got %d", len(routes))
	}
	if _, ok := routes[0].Via.([]configgen.Gateway); !ok {
		t.Fatalf("fail_closed 应保留原网关，got %#v", routes[0].Via)
	}
}

func TestEffectiveAllDownFailOpen(t *testing.T) {
	specs := []model.RouteSpec{{
		CIDR: "10.8.1.0/24", Mode: "active_active", AllDownPolicy: "fail_open",
		Gateways: []model.GatewaySpec{gw("10.88.1.1", 10, 0)},
	}}
	routes := EffectiveUnsafeRoutes(specs, map[string]bool{"10.88.1.1": false})
	if len(routes) != 0 {
		t.Fatalf("fail_open 应移除路由，got %d", len(routes))
	}
}

func TestUnknownGatewayTreatedUp(t *testing.T) {
	specs := []model.RouteSpec{{
		CIDR: "10.8.1.0/24", Mode: "active_active", AllDownPolicy: "fail_closed",
		Gateways: []model.GatewaySpec{gw("10.88.1.1", 10, 0)},
	}}
	routes := EffectiveUnsafeRoutes(specs, map[string]bool{}) // 未探测 → 视为 up
	if len(routes) != 1 {
		t.Fatalf("未知网关应视为 up，got %d routes", len(routes))
	}
}
