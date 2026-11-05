package handler

import (
	"fmt"
	"strings"

	"github.com/iflyelf/iflygo/internal/configgen"
	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/svc"
)

// loadLighthouses 读取网络的 lighthouse 节点，返回 hosts 与 static_host_map。
func loadLighthouses(sc *svc.ServiceContext, networkID int64) (hosts []string, staticMap map[string][]string, err error) {
	staticMap = map[string][]string{}
	q := sc.Store.Rebind(`SELECT overlay_ip4, overlay_ip6, public_host, public_port FROM server_nodes WHERE network_id = $1 AND roles LIKE '%' || $2 || '%'`)
	rows, err := sc.Store.DB.Query(q, networkID, "lighthouse")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ip4, ip6, host string
		var port int
		if err := rows.Scan(&ip4, &ip6, &host, &port); err != nil {
			return nil, nil, err
		}
		if host == "" || port <= 0 {
			continue
		}
		endpoint := fmt.Sprintf("%s:%d", host, port)
		if ip4 != "" {
			hosts = append(hosts, ip4)
			staticMap[ip4] = append(staticMap[ip4], endpoint)
		}
		if ip6 != "" {
			hosts = append(hosts, ip6)
			staticMap[ip6] = append(staticMap[ip6], endpoint)
		}
	}
	return hosts, staticMap, rows.Err()
}

// loadRelayIPs 读取网络的 relay 节点 overlay IP。
func loadRelayIPs(sc *svc.ServiceContext, networkID int64) ([]string, error) {
	q := sc.Store.Rebind(`SELECT overlay_ip4, overlay_ip6 FROM server_nodes WHERE network_id = $1 AND roles LIKE '%' || $2 || '%'`)
	rows, err := sc.Store.DB.Query(q, networkID, "relay")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ip4, ip6 string
		if err := rows.Scan(&ip4, &ip6); err != nil {
			return nil, err
		}
		if ip4 != "" {
			out = append(out, ip4)
		}
		if ip6 != "" {
			out = append(out, ip6)
		}
	}
	return out, rows.Err()
}

// buildNebulaInput 组装 Nebula 配置输入（不硬编码任何 IP/CIDR/端口）。
func buildNebulaInput(sc *svc.ServiceContext, networkID int64, isLighthouse bool, listenPort int, amRelay bool) (*configgen.Input, error) {
	net, err := getNetwork(sc, networkID)
	if err != nil {
		return nil, err
	}
	settings, err := getNebulaSettings(sc, networkID)
	if err != nil {
		return nil, err
	}
	hosts, staticMap, err := loadLighthouses(sc, networkID)
	if err != nil {
		return nil, err
	}
	relayIPs, err := loadRelayIPs(sc, networkID)
	if err != nil {
		return nil, err
	}
	relays := append([]string{}, settings.Relays...)
	relays = append(relays, relayIPs...)

	unsafeRoutes, _, err := loadRouteConfig(sc, networkID)
	if err != nil {
		return nil, err
	}

	var dns []string
	if net.DNS != "" {
		dns = strings.Split(net.DNS, ",")
	}

	return &configgen.Input{
		CAPath:               "ca.crt",
		CertPath:             "node.crt",
		KeyPath:              "node.key",
		IsLighthouse:         isLighthouse,
		ListenPort:           listenPort,
		Lighthouses:          hosts,
		StaticHostMap:        staticMap,
		AmRelay:              amRelay,
		UseRelays:            settings.UseRelays,
		Relays:               relays,
		Cidr4:                net.Cidr4,
		Cidr6:                net.Cidr6,
		MTU:                  net.MTU,
		DNS:                  dns,
		Cipher:               settings.Cipher,
		TunDevice:            settings.TunDevice,
		TunDisabled:          settings.TunDisabled,
		PreferredRanges:      settings.PreferredRanges,
		RemoteAllowList:      settings.RemoteAllowList,
		Punch:                settings.Punch,
		PunchRespond:         settings.PunchRespond,
		PunchDelay:           settings.PunchDelay,
		HandshakeTryInterval: settings.HandshakeTryInterval,
		HandshakeRetries:     settings.HandshakeRetries,
		LogLevel:             settings.LogLevel,
		LogFormat:            settings.LogFormat,
		StatsType:            settings.StatsType,
		StatsListen:          settings.StatsListen,
		UnsafeRoutes:         unsafeRoutes,
	}, nil
}

// loadRouteConfig 将路由表转换为 configgen 配置与客户端 RouteSpec。
func loadRouteConfig(sc *svc.ServiceContext, networkID int64) ([]configgen.Route, []model.RouteSpec, error) {
	routes, err := loadRoutes(sc, networkID)
	if err != nil {
		return nil, nil, err
	}
	var cfgRoutes []configgen.Route
	var specs []model.RouteSpec
	for _, rt := range routes {
		var via any
		if len(rt.Gateways) == 1 {
			via = rt.Gateways[0].OverlayIP
		} else {
			gws := make([]configgen.Gateway, 0, len(rt.Gateways))
			for _, g := range rt.Gateways {
				gws = append(gws, configgen.Gateway{Gateway: g.OverlayIP, Weight: g.Weight})
			}
			via = gws
		}
		cfgRoutes = append(cfgRoutes, configgen.Route{Route: rt.CIDR, Via: via, MTU: rt.MTU, Metric: rt.Metric})

		spec := model.RouteSpec{
			CIDR: rt.CIDR, MTU: rt.MTU, Metric: rt.Metric, Mode: rt.Mode, AllDownPolicy: rt.AllDownPolicy,
		}
		for _, g := range rt.Gateways {
			spec.Gateways = append(spec.Gateways, model.GatewaySpec{
				IP: g.OverlayIP, Weight: g.Weight, Priority: g.Priority, HealthTarget: g.HealthTarget, HealthPort: g.HealthPort,
			})
		}
		specs = append(specs, spec)
	}
	return cfgRoutes, specs, nil
}
