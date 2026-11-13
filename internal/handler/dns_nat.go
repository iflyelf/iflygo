package handler

import (
	"encoding/json"
	"fmt"
	stdnet "net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/iflyelf/iflygo/internal/auth"
	"github.com/iflyelf/iflygo/internal/logic/task"
	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/pkg/audit"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/protocol"
	"github.com/iflyelf/iflygo/internal/svc"
)

// DNSNATScript 预览 NAT 脚本。
//
// 查询参数：
//   - mode=dns|full（默认 dns）
//   - dns=8.8.8.8,1.1.1.1（dns 模式可选；留空汇总全局/网络/每主机实际 DNS）
//   - gateway=10.77.0.1（可选，显式指定 overlay 网关，优先于网络配置与自动选择）
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
		mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
		if mode == "" {
			mode = "dns"
		}
		gwOverride := strings.TrimSpace(r.URL.Query().Get("gateway"))
		if gwOverride != "" && !gatewayInNetwork(net, gwOverride) {
			httpx.Fail(w, http.StatusBadRequest, "网关不在该网络的 overlay 网段内: "+gwOverride)
			return
		}

		if mode == "full" {
			gw4, gw6 := fullGateways(sc, net, gwOverride)
			if gw4 == "" && gw6 == "" {
				httpx.Fail(w, http.StatusBadRequest, "未找到可用网关：请选择网关节点或配置默认路由")
				return
			}
			httpx.OK(w, map[string]any{
				"mode": "full", "gateway4": gw4, "gateway6": gw6,
				"script": buildFullNATScript(net, gw4, gw6, egressExcludePrefixes(sc, net)),
			})
			return
		}
		if mode != "dns" {
			httpx.Fail(w, http.StatusBadRequest, "mode 只能是 dns / full")
			return
		}

		v4, v6, gw4, gw6, err := computeDNSNAT(sc, net, r.URL.Query().Get("dns"), gwOverride)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, err.Error())
			return
		}
		httpx.OK(w, map[string]any{
			"mode": "dns", "gateway4": gw4, "gateway6": gw6,
			"dns":    append(append([]string{}, v4...), v6...),
			"script": buildDNSNATScript(net, v4, v6, gw4, gw6),
		})
	}
}

// NATStatus 返回网络的自动 NAT 状态（含重试次数与最近错误）。
func NATStatus(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		rows, err := sc.Store.DB.Query(sc.Store.Rebind(`SELECT mode,COALESCE(status,''),COALESCE(retry_count,0),
			COALESCE(last_error,''),COALESCE(last_attempt_at,''),COALESCE(task_id,0),
			COALESCE(gateway,''),COALESCE(host_ip,''),COALESCE(alerted_at,'')
			FROM gateway_nat_state WHERE network_id = $1 ORDER BY mode`), id)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var mode, status, lastErr, lastAt, gateway, hostIP, alertedAt string
			var retries, taskID int64
			if err := rows.Scan(&mode, &status, &retries, &lastErr, &lastAt, &taskID, &gateway, &hostIP, &alertedAt); err != nil {
				httpx.FailErr(w, http.StatusInternalServerError, err)
				return
			}
			items = append(items, map[string]any{
				"mode": mode, "status": status, "retry_count": retries,
				"max_retries": maxNATRetries(sc), "last_error": lastErr,
				"last_attempt_at": lastAt, "task_id": taskID,
				"gateway": gateway, "host_ip": hostIP, "alerted_at": alertedAt,
			})
		}
		httpx.OK(w, map[string]any{"max_retries": maxNATRetries(sc), "items": items})
	}
}

type natApplyReq struct {
	Mode string `json:"mode"` // dns / full
	DNS  string `json:"dns"`
	// Gateway 显式 overlay 网关（可选）。
	Gateway string `json:"gateway"`
	// ServerNodeID 用于自动匹配网关节点对应的 Agent 主机（可选）。
	ServerNodeID int64 `json:"server_node_id"`
	// HostIP 显式指定执行脚本的 Agent 主机 IP/hostname（可选）。
	HostIP string `json:"host_ip"`
	// Start 为 true 时创建并立即启动 Agent 命令任务。
	Start bool `json:"start"`
}

// NATApply 生成 NAT 脚本，并可通过 Agent 自动执行。
//
// POST /api/networks/{id}/nat/apply
// 仅在开启 agent.allow_exec_mode 且操作者为管理员时可执行。
func NATApply(sc *svc.ServiceContext) http.HandlerFunc {
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
		var req natApplyReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		mode := strings.ToLower(strings.TrimSpace(req.Mode))
		if mode == "" {
			mode = "dns"
		}
		req.Gateway = strings.TrimSpace(req.Gateway)
		if req.Gateway != "" && !gatewayInNetwork(net, req.Gateway) {
			httpx.Fail(w, http.StatusBadRequest, "网关不在该网络的 overlay 网段内: "+req.Gateway)
			return
		}

		var script, label string
		var gw4, gw6 string
		switch mode {
		case "dns":
			v4, v6, g4, g6, cerr := computeDNSNAT(sc, net, req.DNS, req.Gateway)
			if cerr != nil {
				httpx.Fail(w, http.StatusBadRequest, cerr.Error())
				return
			}
			gw4, gw6 = g4, g6
			script = buildDNSNATScript(net, v4, v6, gw4, gw6)
			label = "DNS NAT"
		case "full":
			gw4, gw6 = fullGateways(sc, net, req.Gateway)
			if gw4 == "" && gw6 == "" {
				httpx.Fail(w, http.StatusBadRequest, "未找到可用网关：请选择网关节点或配置默认路由")
				return
			}
			script = buildFullNATScript(net, gw4, gw6, egressExcludePrefixes(sc, net))
			label = "全量出口 NAT"
		default:
			httpx.Fail(w, http.StatusBadRequest, "mode 只能是 dns / full")
			return
		}

		// 应用 NAT 时同步网络出口模式：dns 模式开启 DNS 路由；full 模式启用全量出口。
		egressChanged := false
		targetMode := ""
		switch mode {
		case "full":
			targetMode = "full"
		case "dns":
			if net.EgressMode == "" || net.EgressMode == "off" {
				targetMode = "dns"
			}
		}
		if targetMode != "" && net.EgressMode != targetMode {
			if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE networks SET egress_mode = $1 WHERE id = $2`), targetMode, net.ID); err == nil {
				net.EgressMode = targetMode
				bumpConfigRev(sc, net.ID)
				egressChanged = true
			}
		}

		resp := map[string]any{
			"mode": mode, "gateway4": gw4, "gateway6": gw6,
			"script": script, "egress_mode_changed": egressChanged,
		}
		if !req.Start {
			httpx.OK(w, resp)
			return
		}

		// 必须显式指定或能从服务端节点 public_host 匹配到带 Agent 的主机。
		host, herr := resolveNATHost(sc, net, req.HostIP, req.ServerNodeID)
		if herr != nil {
			httpx.Fail(w, http.StatusBadRequest, herr.Error())
			return
		}
		u, _ := auth.FromContext(r.Context())
		createdBy := ""
		if u != nil {
			createdBy = u.Username
		}
		treq := task.CreateRequest{
			Kind:      task.KindCommand,
			Name:      fmt.Sprintf("iflygo %s · %s", label, net.Name),
			Version:   "command",
			Command:   protocol.CommandSpec{Script: script, Interpreter: "/bin/sh", Timeout: "5m"},
			Selector:  task.Selector{IPExpr: host.IP},
			Strategy:  task.Strategy{Mode: "count", BatchSize: 1, Concurrency: 1, FailureThreshold: "1"},
			CreatedBy: createdBy,
		}
		treq.SetActor(u)
		taskID, terr := sc.Task.Create(r.Context(), reqScope(r), treq)
		if terr != nil {
			httpx.Fail(w, http.StatusBadRequest, "创建 Agent 任务失败: "+terr.Error())
			return
		}
		if err := sc.Task.Start(r.Context(), reqScope(r), taskID); err != nil {
			httpx.Fail(w, http.StatusBadRequest, fmt.Sprintf("任务 #%d 已创建但启动失败: %v", taskID, err))
			return
		}
		auditOp(sc, r, audit.ActionTaskCreate, "task", strconv.FormatInt(taskID, 10), "nat:"+mode, nil)
		resp["task_id"] = taskID
		resp["host_ip"] = host.IP
		resp["host_name"] = host.Hostname
		resp["started"] = true
		httpx.OK(w, resp)
	}
}

func gatewayInNetwork(net *model.Network, gw string) bool {
	a, err := netip.ParseAddr(strings.TrimSpace(gw))
	if err != nil {
		return false
	}
	return prefixContains(net.Cidr4, a) || prefixContains(net.Cidr6, a)
}

// fullGateways 返回全量公网出口使用的网关（IPv4/IPv6 可各一个）。
func fullGateways(sc *svc.ServiceContext, net *model.Network, override string) (string, string) {
	gw4, gw6 := "", ""
	if override != "" {
		if a, err := netip.ParseAddr(override); err == nil {
			if a.Is4() {
				gw4 = override
			} else {
				gw6 = override
			}
		}
	}
	if gw4 == "" && (net.Cidr4 != "" || override == "") {
		gw4 = resolveDNSGateway(sc, net, true)
	}
	if gw6 == "" && net.Cidr6 != "" {
		gw6 = resolveDNSGateway(sc, net, false)
	}
	return gw4, gw6
}

// computeDNSNAT 计算需要 NAT 的公网 DNS 与对应网关。
func computeDNSNAT(sc *svc.ServiceContext, net *model.Network, rawDNS, gwOverride string) (v4, v6 []string, gw4, gw6 string, err error) {
	dnsList := splitList(rawDNS)
	if strings.TrimSpace(rawDNS) == "" {
		dnsList = collectNetworkDNS(sc, net)
	}
	seen := map[string]bool{}
	for _, item := range dnsList {
		addr, perr := netip.ParseAddr(strings.TrimSpace(item))
		if perr != nil {
			return nil, nil, "", "", fmt.Errorf("DNS 含非法 IP: %s", item)
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
		return nil, nil, "", "", fmt.Errorf("未找到需要经网关 NAT 的公网 DNS（overlay 内 DNS 无需 NAT）")
	}
	if len(v4) > 0 {
		gw4 = gatewayForFamily(sc, net, gwOverride, true)
		if gw4 == "" {
			return nil, nil, "", "", fmt.Errorf("未找到 IPv4 网关：请选择网关节点、配置 DNS 网关或默认路由")
		}
	}
	if len(v6) > 0 {
		gw6 = gatewayForFamily(sc, net, gwOverride, false)
		if gw6 == "" {
			return nil, nil, "", "", fmt.Errorf("未找到 IPv6 网关：请选择网关节点、配置 DNS 网关或默认路由")
		}
	}
	return v4, v6, gw4, gw6, nil
}

func gatewayForFamily(sc *svc.ServiceContext, net *model.Network, override string, ipv4 bool) string {
	if override != "" {
		if a, err := netip.ParseAddr(override); err == nil && a.Is4() == ipv4 {
			return override
		}
	}
	return resolveDNSGateway(sc, net, ipv4)
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
		add(sc.ConfigSnapshot().Client.DNS)
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

// natHost 可执行 NAT 脚本的 Agent 主机。
type natHost struct {
	ID       int64
	IP       string
	Hostname string
	AgentID  string
}

// resolveNATHost 解析目标 Agent 主机：优先显式 host_ip，其次网关节点 public_host。
func resolveNATHost(sc *svc.ServiceContext, net *model.Network, hostIP string, serverNodeID int64) (*natHost, error) {
	candidate := strings.TrimSpace(hostIP)
	if candidate == "" && serverNodeID > 0 {
		var pub string
		_ = sc.Store.DB.QueryRow(sc.Store.Rebind(`SELECT public_host FROM server_nodes WHERE id = $1 AND network_id = $2`),
			serverNodeID, net.ID).Scan(&pub)
		pub = strings.TrimSpace(pub)
		if h, _, err := stdnet.SplitHostPort(pub); err == nil {
			pub = h
		}
		candidate = pub
	}
	if candidate == "" {
		return nil, fmt.Errorf("请选择执行脚本的 Agent 主机（或为网关节点填写 public_host）")
	}
	var h natHost
	q := sc.Store.Rebind(`SELECT id, ip, COALESCE(hostname,''), COALESCE(agent_id,'') FROM hosts
		WHERE (ip = $1 OR hostname = $2 OR exit_ip = $3 OR tunnel_ip = $4) AND COALESCE(agent_id,'') <> ''
		ORDER BY id LIMIT 1`)
	if err := sc.Store.DB.QueryRow(q, candidate, candidate, candidate, candidate).Scan(&h.ID, &h.IP, &h.Hostname, &h.AgentID); err != nil {
		return nil, fmt.Errorf("未找到与 %s 匹配的 Agent 主机，请在页面选择目标主机", candidate)
	}
	return &h, nil
}

// safeScriptCIDR 只允许合法 CIDR 进入 shell 脚本，防止历史脏数据造成命令注入。
func safeScriptCIDR(raw string) string {
	p, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return p.Masked().String()
}

// scriptComment 清理注释中的换行/控制字符，避免脚本注释被用于注入命令。
func scriptComment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\r' || r == '\n' {
			b.WriteByte(' ')
			continue
		}
		if r < 0x20 {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func buildDNSNATScript(net *model.Network, v4, v6 []string, gw4, gw6 string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# iflygo overlay 网关 DNS NAT 策略（需 root 执行）\n")
	b.WriteString("# 网络: " + scriptComment(net.Name) + "\n")
	if gw4 != "" {
		b.WriteString("# IPv4 网关: " + scriptComment(gw4) + "\n")
	}
	if gw6 != "" {
		b.WriteString("# IPv6 网关: " + scriptComment(gw6) + "\n")
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
		src := safeScriptCIDR(net.Cidr4)
		if src == "" {
			b.WriteString("# IPv4 overlay CIDR 非法，已跳过 DNS NAT 规则\n")
		} else {
			b.WriteString("# 2) IPv4：放行 overlay → DNS，并对回包做源地址伪装\n")
			for _, d := range v4 {
				dst := d + "/32"
				fmt.Fprintf(&b, "iptables -C FORWARD -s %s -d %s -j ACCEPT 2>/dev/null || iptables -A FORWARD -s %s -d %s -j ACCEPT\n", src, dst, src, dst)
				fmt.Fprintf(&b, "iptables -t nat -C POSTROUTING -s %s -d %s -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s %s -d %s -j MASQUERADE\n", src, dst, src, dst)
			}
		}
		b.WriteString("\n")
	}
	if len(v6) > 0 {
		src := safeScriptCIDR(net.Cidr6)
		if src == "" {
			b.WriteString("# IPv6 overlay CIDR 非法，已跳过 DNS NAT 规则\n")
		} else {
			b.WriteString("# 3) IPv6：放行 overlay → DNS，并对回包做源地址伪装\n")
			for _, d := range v6 {
				dst := d + "/128"
				fmt.Fprintf(&b, "ip6tables -C FORWARD -s %s -d %s -j ACCEPT 2>/dev/null || ip6tables -A FORWARD -s %s -d %s -j ACCEPT\n", src, dst, src, dst)
				fmt.Fprintf(&b, "ip6tables -t nat -C POSTROUTING -s %s -d %s -j MASQUERADE 2>/dev/null || ip6tables -t nat -A POSTROUTING -s %s -d %s -j MASQUERADE\n", src, dst, src, dst)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("# 持久化（按发行版选择其一）\n")
	b.WriteString("# Debian/Ubuntu: apt-get install -y iptables-persistent && netfilter-persistent save\n")
	b.WriteString("# RHEL/CentOS:   yum install -y iptables-services && service iptables save\n")
	b.WriteString("# nftables:      使用 iptables-nft 兼容层，或把上述规则转换为 nft 规则后 nft list ruleset > /etc/nftables.conf\n")
	return b.String()
}

// writeNATExcludeRules 为全量出口写入 RETURN 规则，避免对排除网段做源 NAT。
func writeNATExcludeRules(b *strings.Builder, bin, src string, excludes []netip.Prefix, ipv4 bool) {
	for _, ex := range excludes {
		if ex.Addr().Is4() != ipv4 {
			continue
		}
		fmt.Fprintf(b, "%s -t nat -C POSTROUTING -s %s -d %s -j RETURN 2>/dev/null || %s -t nat -A POSTROUTING -s %s -d %s -j RETURN\n",
			bin, src, ex.String(), bin, src, ex.String())
	}
}

// buildFullNATScript 生成全量公网出口 NAT 脚本（overlay → 任意公网地址）。
func buildFullNATScript(net *model.Network, gw4, gw6 string, excludes []netip.Prefix) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# iflygo 全量公网出口 NAT 策略（需 root 执行）\n")
	b.WriteString("# 网络: " + scriptComment(net.Name) + "\n")
	if gw4 != "" {
		b.WriteString("# IPv4 网关: " + scriptComment(gw4) + "\n")
	}
	if gw6 != "" {
		b.WriteString("# IPv6 网关: " + scriptComment(gw6) + "\n")
	}
	b.WriteString("# 注意：该脚本会把 overlay 网段的所有出公网流量 NAT 到本网关，请确认安全组策略。\n")
	b.WriteString("set -e\n\n")
	if src4 := safeScriptCIDR(net.Cidr4); src4 != "" {
		b.WriteString("sysctl -w net.ipv4.ip_forward=1\n")
		b.WriteString("iptables -C FORWARD -s " + src4 + " -j ACCEPT 2>/dev/null || iptables -A FORWARD -s " + src4 + " -j ACCEPT\n")
		b.WriteString("iptables -C FORWARD -d " + src4 + " -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || iptables -A FORWARD -d " + src4 + " -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n")
		writeNATExcludeRules(&b, "iptables", src4, excludes, true)
		b.WriteString("iptables -t nat -C POSTROUTING -s " + src4 + " -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s " + src4 + " -j MASQUERADE\n\n")
	}
	if src6 := safeScriptCIDR(net.Cidr6); src6 != "" {
		b.WriteString("sysctl -w net.ipv6.conf.all.forwarding=1\n")
		b.WriteString("ip6tables -C FORWARD -s " + src6 + " -j ACCEPT 2>/dev/null || ip6tables -A FORWARD -s " + src6 + " -j ACCEPT\n")
		b.WriteString("ip6tables -C FORWARD -d " + src6 + " -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || ip6tables -A FORWARD -d " + src6 + " -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n")
		writeNATExcludeRules(&b, "ip6tables", src6, excludes, false)
		b.WriteString("ip6tables -t nat -C POSTROUTING -s " + src6 + " -j MASQUERADE 2>/dev/null || ip6tables -t nat -A POSTROUTING -s " + src6 + " -j MASQUERADE\n\n")
	}
	b.WriteString("# 持久化（按发行版选择其一）\n")
	b.WriteString("# Debian/Ubuntu: apt-get install -y iptables-persistent && netfilter-persistent save\n")
	b.WriteString("# RHEL/CentOS:   yum install -y iptables-services && service iptables save\n")
	return b.String()
}
