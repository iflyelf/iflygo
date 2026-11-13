package handler

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/svc"
)

type gatewayReq struct {
	NodeID       int64  `json:"node_id"`
	OverlayIP    string `json:"overlay_ip"`
	Weight       int    `json:"weight"`
	Priority     int    `json:"priority"`
	HealthTarget string `json:"health_target"`
	HealthPort   int    `json:"health_port"`
}

type createRouteReq struct {
	CIDR          string       `json:"cidr"`
	MTU           int          `json:"mtu"`
	Metric        int          `json:"metric"`
	Mode          string       `json:"mode"`
	AllDownPolicy string       `json:"all_down_policy"`
	Description   string       `json:"description"`
	Gateways      []gatewayReq `json:"gateways"`
}

// ListRoutes 列出网络的不安全路由（含网关）。
func ListRoutes(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nid, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		routes, err := loadRoutes(sc, nid)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, routes)
	}
}

// normalizeRouteReq 校验路由 CIDR、网关 IP 与模式，防止非法值进入客户端配置。
func normalizeRouteReq(net *model.Network, req *createRouteReq) error {
	req.CIDR = strings.TrimSpace(req.CIDR)
	if _, err := netip.ParsePrefix(req.CIDR); err != nil {
		return fmt.Errorf("cidr 非法: %s", req.CIDR)
	}
	if len(req.Gateways) == 0 {
		return fmt.Errorf("gateways 必填")
	}
	if req.MTU < 0 || req.MTU > 9000 {
		return fmt.Errorf("MTU 应在 0-9000 之间")
	}
	if req.Mode == "" {
		req.Mode = "active_active"
	}
	switch req.Mode {
	case "active_active", "active_passive":
	default:
		return fmt.Errorf("mode 只能是 active_active / active_passive")
	}
	if req.AllDownPolicy == "" {
		req.AllDownPolicy = "fail_closed"
	}
	switch req.AllDownPolicy {
	case "fail_closed", "fail_open":
	default:
		return fmt.Errorf("all_down_policy 只能是 fail_closed / fail_open")
	}
	for i := range req.Gateways {
		g := &req.Gateways[i]
		g.OverlayIP = strings.TrimSpace(g.OverlayIP)
		a, err := netip.ParseAddr(g.OverlayIP)
		if err != nil {
			return fmt.Errorf("网关 IP 非法: %s", g.OverlayIP)
		}
		if !prefixContains(net.Cidr4, a) && !prefixContains(net.Cidr6, a) {
			return fmt.Errorf("网关 %s 不在该网络 overlay 网段内", g.OverlayIP)
		}
		if g.Weight <= 0 {
			g.Weight = 1
		}
		if g.Priority < 0 {
			return fmt.Errorf("网关优先级不能为负数")
		}
		if g.HealthPort < 0 || g.HealthPort > 65535 {
			return fmt.Errorf("健康检查端口非法: %d", g.HealthPort)
		}
	}
	return nil
}

// CreateRoute 创建不安全路由及其网关。
func CreateRoute(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nid, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		var req createRouteReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		net, err := getNetwork(sc, nid)
		if err != nil {
			httpx.Fail(w, http.StatusNotFound, "网络不存在")
			return
		}
		if err := normalizeRouteReq(net, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, err.Error())
			return
		}

		now := nowStr()
		var rid int64
		err = sc.Store.DB.QueryRow(sc.Store.Rebind(`INSERT INTO subnet_routes (network_id,cidr,mtu,metric,mode,all_down_policy,enabled,description,created_at) VALUES ($1,$2,$3,$4,$5,$6,1,$7,$8) RETURNING id`),
			nid, req.CIDR, req.MTU, req.Metric, req.Mode, req.AllDownPolicy, req.Description, now).Scan(&rid)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		for _, g := range req.Gateways {
			if g.Weight == 0 {
				g.Weight = 1
			}
			if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`INSERT INTO route_gateways (route_id,node_id,overlay_ip,weight,priority,health_target,health_port,enabled,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8)`),
				rid, g.NodeID, g.OverlayIP, g.Weight, g.Priority, g.HealthTarget, g.HealthPort, now); err != nil {
				httpx.FailErr(w, http.StatusInternalServerError, err)
				return
			}
		}

		routes, err := loadRoutes(sc, nid)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		bumpConfigRev(sc, nid)
		for _, rt := range routes {
			if rt.ID == rid {
				httpx.OK(w, rt)
				return
			}
		}
		httpx.OK(w, nil)
	}
}

// UpdateRoute 更新不安全路由及其网关。
func UpdateRoute(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		var nid int64
		if err := sc.Store.DB.QueryRow(sc.Store.Rebind(`SELECT network_id FROM subnet_routes WHERE id = $1`), id).Scan(&nid); err != nil {
			httpx.Fail(w, http.StatusNotFound, "路由不存在")
			return
		}
		var req createRouteReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		net, nerr := getNetwork(sc, nid)
		if nerr != nil {
			httpx.Fail(w, http.StatusNotFound, "网络不存在")
			return
		}
		if err := normalizeRouteReq(net, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE subnet_routes SET cidr=$1,mtu=$2,metric=$3,mode=$4,all_down_policy=$5,description=$6 WHERE id=$7`),
			req.CIDR, req.MTU, req.Metric, req.Mode, req.AllDownPolicy, req.Description, id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`DELETE FROM route_gateways WHERE route_id = $1`), id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		now := nowStr()
		for _, g := range req.Gateways {
			if g.Weight == 0 {
				g.Weight = 1
			}
			if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`INSERT INTO route_gateways (route_id,node_id,overlay_ip,weight,priority,health_target,health_port,enabled,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8)`),
				id, g.NodeID, g.OverlayIP, g.Weight, g.Priority, g.HealthTarget, g.HealthPort, now); err != nil {
				httpx.FailErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		bumpConfigRev(sc, nid)
		routes, err := loadRoutes(sc, nid)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		for _, rt := range routes {
			if rt.ID == id {
				httpx.OK(w, rt)
				return
			}
		}
		httpx.OK(w, nil)
	}
}

// DeleteRoute 删除路由。
func DeleteRoute(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		var nid int64
		_ = sc.Store.DB.QueryRow(sc.Store.Rebind(`SELECT network_id FROM subnet_routes WHERE id = $1`), id).Scan(&nid)
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`DELETE FROM subnet_routes WHERE id = $1`), id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		if nid != 0 {
			bumpConfigRev(sc, nid)
		}
		httpx.OK(w, nil)
	}
}

// loadRoutes 加载网络的路由及其网关。
func loadRoutes(sc *svc.ServiceContext, networkID int64) ([]model.SubnetRoute, error) {
	q := sc.Store.Rebind(`SELECT id,network_id,cidr,mtu,metric,mode,all_down_policy,enabled,description,created_at FROM subnet_routes WHERE network_id = $1 AND enabled = 1 ORDER BY id`)
	rows, err := sc.Store.DB.Query(q, networkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []model.SubnetRoute
	for rows.Next() {
		var rt model.SubnetRoute
		var enabled int64
		if err := rows.Scan(&rt.ID, &rt.NetworkID, &rt.CIDR, &rt.MTU, &rt.Metric, &rt.Mode, &rt.AllDownPolicy, &enabled, &rt.Description, &rt.CreatedAt); err != nil {
			return nil, err
		}
		rt.Enabled = enabled != 0
		routes = append(routes, rt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range routes {
		gws, err := loadGateways(sc, routes[i].ID)
		if err != nil {
			return nil, err
		}
		routes[i].Gateways = gws
	}
	return routes, nil
}

func loadGateways(sc *svc.ServiceContext, routeID int64) ([]model.RouteGateway, error) {
	q := sc.Store.Rebind(`SELECT id,route_id,COALESCE(node_id,0),overlay_ip,weight,priority,health_target,health_port,enabled,created_at FROM route_gateways WHERE route_id = $1 AND enabled = 1 ORDER BY priority, id`)
	rows, err := sc.Store.DB.Query(q, routeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RouteGateway
	for rows.Next() {
		var g model.RouteGateway
		var enabled int64
		if err := rows.Scan(&g.ID, &g.RouteID, &g.NodeID, &g.OverlayIP, &g.Weight, &g.Priority, &g.HealthTarget, &g.HealthPort, &enabled, &g.CreatedAt); err != nil {
			return nil, err
		}
		g.Enabled = enabled != 0
		out = append(out, g)
	}
	return out, rows.Err()
}
