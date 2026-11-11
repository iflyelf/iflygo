package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/svc"
)

// DNSNATScript 生成 overlay 网关节点上的 NAT 策略脚本。
//
// 使用场景：客户端 DNS 为公网地址（如 8.8.8.8），且开启「DNS 主机路由」后，
// DNS 报文会经 overlay 发到网关；网关需开启转发并做源地址伪装（MASQUERADE），
// 否则公网 DNS 的回包无法返回 overlay 客户端。
//
// 查询参数 dns 可选；留空时汇总全局/网络/每主机实际使用的 DNS。
func DNSNATScript(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		net, err := getNetwork(sc, id)
		if err != nil {
			httpx.Fail(w, http.StatusNotFound, "网络不存在")
			return
		}

		raw := strings.TrimSpace(r.URL.Query().Get("dns"))
		dnsList := splitList(raw)
		if len(dnsList) == 0 {
			dnsList = collectNetworkDNS(sc, net)
		}

		var v4, v6 []string
		seen := map[string]bool{}
		for _, item := range dnsList {
			addr, err := netip.ParseAddr(strings.TrimSpace(item))
			if err != nil {
				httpx.Fail(w, http.StatusBadRequest, "DNS 含非法 IP: "+item)
				return
			}
			// overlay 内 DNS 直接可达，不需要 NAT。
			if prefixContains(net.Cidr4, addr) || prefixContains(net.Cidr6, addr) {
				continue
			}
			s := addr.String()
			if seen[s] {
				continue
			}
			seen[s] = true
			if addr.Is4() {
				v4 = append(v4, s)
			} else {
				v6 = append(v6, s)
			}
		}
		if len(v4) == 0 && len(v6) == 0 {
			httpx.Fail(w, http.StatusBadRequest, "未找到需要经网关 NAT 的公网 DNS（overlay 内 DNS 无需 NAT）")
			return
		}

		gw4, gw6 := "", ""
		if len(v4) > 0 {
			gw4 = resolveDNSGateway(sc, net, true)
			if gw4 == "" {
				httpx.Fail(w, http.StatusBadRequest, "未找到 IPv4 网关：请配置默认路由、gateway 角色节点或网络网关")
				return
			}
		}
		if len(v6) > 0 {
			gw6 = resolveDNSGateway(sc, net, false)
			if gw6 == "" {
				httpx.Fail(w, http.StatusBadRequest, "未找到 IPv6 网关：请配置默认路由、gateway 角色节点或网络网关")
				return
			}
		}

		script := buildDNSNATScript(net, v4, v6, gw4, gw6)
		httpx.OK(w, map[string]any{
			"gateway4": gw4,
			"gateway6": gw6,
			"dns":      append(append([]string{}, v4...), v6...),
			"script":   script,
		})
	}
}

// collectNetworkDNS 汇总该网络实际可能用到的 DNS（全局 + 网络级 + 每主机覆盖）。
func collectNetworkDNS(sc *svc.ServiceContext, net *model.Network) []string {
	seen := map[string]bool{}
	var out []string
	add := func(csv string) {
		for _, s := range strings.Split(csv, ",") {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	if sc.Settings != nil {
		add(sc.Settings.Snapshot().Client.DNS)
	} else if sc.Config != nil {
		add(sc.Config.Client.DNS)
	}
	add(net.DNS)
	rows, err := sc.Store.DB.Query(sc.Store.Rebind(`SELECT client_settings FROM client_nodes WHERE network_id = $1`), net.ID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) != nil || raw == "" {
			continue
		}
		var ov model.ClientOverride
		if json.Unmarshal([]byte(raw), &ov) == nil {
			add(ov.DNS)
		}
	}
	return out
}

func buildDNSNATScript(net *model.Network, v4, v6 []string, gw4, gw6 string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# iflygo overlay 网关 DNS NAT 策略（需 root 执行）\n")
	b.WriteString("# 网络: " + net.Name + "\n")
	if gw4 != "" {
		b.WriteString("# IPv4 网关: " + gw4 + "\n")
	}
	if gw6 != "" {
		b.WriteString("# IPv6 网关: " + gw6 + "\n")
	}
	b.WriteString("set -e\n\n")
	b.WriteString("# 1) 开启内核转发\n")
	if len(v4) > 0 {
		b.WriteString("sysctl -w net.ipv4.ip_forward=1\n")
	}
	if len(v6) > 0 {
		b.WriteString("sysctl -w net.ipv6.conf.all.forwarding=1\n")
	}
	b.WriteString("\n")
	if len(v4) > 0 {
		src := net.Cidr4
		if src == "" {
			src = "0.0.0.0/0"
		}
		b.WriteString("# 2) IPv4：放行 overlay → DNS，并对回包做源地址伪装\n")
		for _, d := range v4 {
			dst := d + "/32"
			fmt.Fprintf(&b, "iptables -C FORWARD -s %s -d %s -j ACCEPT 2>/dev/null || iptables -A FORWARD -s %s -d %s -j ACCEPT\n", src, dst, src, dst)
			fmt.Fprintf(&b, "iptables -t nat -C POSTROUTING -s %s -d %s -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s %s -d %s -j MASQUERADE\n", src, dst, src, dst)
		}
		b.WriteString("\n")
	}
	if len(v6) > 0 {
		src := net.Cidr6
		if src == "" {
			src = "::/0"
		}
		b.WriteString("# 3) IPv6：放行 overlay → DNS，并对回包做源地址伪装\n")
		for _, d := range v6 {
			dst := d + "/128"
			fmt.Fprintf(&b, "ip6tables -C FORWARD -s %s -d %s -j ACCEPT 2>/dev/null || ip6tables -A FORWARD -s %s -d %s -j ACCEPT\n", src, dst, src, dst)
			fmt.Fprintf(&b, "ip6tables -t nat -C POSTROUTING -s %s -d %s -j MASQUERADE 2>/dev/null || ip6tables -t nat -A POSTROUTING -s %s -d %s -j MASQUERADE\n", src, dst, src, dst)
		}
		b.WriteString("\n")
	}
	b.WriteString("# 持久化（按发行版选择其一）\n")
	b.WriteString("# Debian/Ubuntu: apt-get install -y iptables-persistent && netfilter-persistent save\n")
	b.WriteString("# RHEL/CentOS:   yum install -y iptables-services && service iptables save\n")
	b.WriteString("# nftables:      使用 iptables-nft 兼容层，或把上述规则转换为 nft 规则后 nft list ruleset > /etc/nftables.conf\n")
	return b.String()
}
