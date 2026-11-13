package handler

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/iflyelf/iflygo/internal/configgen"
	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/svc"
)

// defaultEgressExcludes 内置全量出口排除网段：内网、回环、链路本地、组播、保留地址。
//
// 这些网段不进入 VPN，避免全量出口把局域网、控制面、本地服务流量也绕到网关。
func defaultEgressExcludes() []string {
	return []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
		"169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16",
		"198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
		"::1/128", "fc00::/7", "fe80::/10", "ff00::/8",
	}
}

type cpIPCacheEntry struct {
	ips []netip.Addr
	at  time.Time
}

var cpIPCache sync.Map // host -> cpIPCacheEntry

// controlPlaneExcludes 把控制面 external_url 的主机地址加入排除列表，
// 确保全量出口不会把控制面流量绕到网关（避免客户端失联）。
func controlPlaneExcludes(sc *svc.ServiceContext) []netip.Addr {
	raw := ""
	if sc.Settings != nil {
		raw = sc.Settings.Snapshot().Server.ExternalURL
	} else if sc.Config != nil {
		raw = sc.ConfigSnapshot().Server.ExternalURL
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	host := u.Hostname()
	if a, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{a}
	}
	if v, ok := cpIPCache.Load(host); ok {
		e := v.(cpIPCacheEntry)
		if time.Since(e.at) < 5*time.Minute {
			return e.ips
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil
	}
	out := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.Unmap())
	}
	cpIPCache.Store(host, cpIPCacheEntry{ips: out, at: time.Now()})
	return out
}

// egressExcludePrefixes 汇总内置、全局、网络与控制面排除网段。
func egressExcludePrefixes(sc *svc.ServiceContext, net *model.Network) []netip.Prefix {
	seen := map[string]bool{}
	var out []netip.Prefix
	add := func(raw string) {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			p, err := netip.ParsePrefix(s)
			if err != nil {
				continue
			}
			p = p.Masked()
			if seen[p.String()] {
				continue
			}
			seen[p.String()] = true
			out = append(out, p)
		}
	}
	for _, s := range defaultEgressExcludes() {
		add(s)
	}
	if sc.Settings != nil {
		add(sc.Settings.Snapshot().Client.EgressExclude)
	} else if sc.Config != nil {
		add(sc.ConfigSnapshot().Client.EgressExclude)
	}
	add(net.EgressExclude)
	// overlay 自身必须排除，避免全量路由覆盖 overlay 内网。
	add(net.Cidr4)
	add(net.Cidr6)
	for _, a := range controlPlaneExcludes(sc) {
		bits := 128
		if a.Is4() {
			bits = 32
		}
		add(netip.PrefixFrom(a, bits).String())
	}
	return out
}

// fullEgressRoutes 计算「0.0.0.0/0 减去排除网段」后的公网路由集合。
//
// 不使用单一 0/0 路由：否则 VpnService / Nebula 会把内网、控制面流量也送入隧道。
func fullEgressRoutes(sc *svc.ServiceContext, net *model.Network, gw4, gw6 string) ([]configgen.Route, []model.RouteSpec) {
	if gw4 == "" && gw6 == "" {
		return nil, nil
	}
	excludes := egressExcludePrefixes(sc, net)
	var cfgRoutes []configgen.Route
	var specs []model.RouteSpec
	addBase := func(base string, gw string) {
		if gw == "" {
			return
		}
		p, err := netip.ParsePrefix(base)
		if err != nil {
			return
		}
		for _, allowed := range subtractPrefixes(p, excludes) {
			cidr := allowed.String()
			cfgRoutes = append(cfgRoutes, configgen.Route{Route: cidr, Via: gw, MTU: net.MTU})
			specs = append(specs, model.RouteSpec{
				CIDR: cidr, MTU: net.MTU, Mode: "active_active", AllDownPolicy: "fail_closed",
				Gateways: []model.GatewaySpec{{IP: gw, Weight: 1}},
			})
		}
	}
	addBase("0.0.0.0/0", gw4)
	addBase("::/0", gw6)
	return cfgRoutes, specs
}

// subtractPrefixes 从 base 中扣除 excludes，返回覆盖剩余地址空间的 CIDR 列表。
func subtractPrefixes(base netip.Prefix, excludes []netip.Prefix) []netip.Prefix {
	base = base.Masked()
	out := []netip.Prefix{base}
	for _, ex := range excludes {
		ex = ex.Masked()
		if ex.Addr().Is4() != base.Addr().Is4() {
			continue
		}
		var next []netip.Prefix
		for _, p := range out {
			next = append(next, subtractOne(p, ex)...)
		}
		out = next
		if len(out) == 0 {
			break
		}
	}
	return out
}

func subtractOne(p, ex netip.Prefix) []netip.Prefix {
	if !p.Overlaps(ex) {
		return []netip.Prefix{p}
	}
	// ex 覆盖 p：整段扣除。
	if ex.Bits() <= p.Bits() && p.Contains(ex.Addr()) {
		return nil
	}
	// p 已是单地址且仍重叠（理论上不会发生）→ 扣除。
	if p.Bits() >= p.Addr().BitLen() {
		return nil
	}
	left := netip.PrefixFrom(p.Addr(), p.Bits()+1)
	right := netip.PrefixFrom(halfAddr(p), p.Bits()+1)
	var out []netip.Prefix
	out = append(out, subtractOne(left.Masked(), ex)...)
	out = append(out, subtractOne(right.Masked(), ex)...)
	return out
}

// halfAddr 返回 p 的后半段起始地址（把 host 部分最高位置 1）。
func halfAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().AsSlice()
	if len(b) == 0 {
		return p.Addr()
	}
	bit := p.Bits()
	idx := bit / 8
	if idx >= len(b) {
		return p.Addr()
	}
	b[idx] |= 1 << (7 - uint(bit%8))
	if a, ok := netip.AddrFromSlice(b); ok {
		return a
	}
	return p.Addr()
}
