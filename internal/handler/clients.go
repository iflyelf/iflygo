package handler

import (
	"encoding/json"
	stdnet "net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/configgen"
	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/pki"
	"github.com/iflyelf/iflygo/internal/svc"
)

// clientRow 完整的客户端记录（含证书/私钥/密钥哈希）。
type clientRow struct {
	model.ClientNode
	PublicKey  string
	CertPEM    string
	KeyPEM     string
	SecretHash string
}

type createClientReq struct {
	NetworkID int64  `json:"network_id"`
	Name      string `json:"name"`
	IPv4      string `json:"ipv4"`
	IPv6      string `json:"ipv6"`
	Groups    string `json:"groups"`
	Subnets   string `json:"subnets"`
	// ClientSettings 每主机客户端参数覆盖（可选）。
	ClientSettings *model.ClientOverride `json:"client_settings"`
}

// UpdateClient 更新客户端：名称/IP/分组/子网与每主机客户端参数。
// IP 变更会重新签发证书、更新 IPAM 绑定并递增 config_rev，
// 客户端下一次心跳自动拉取新配置（无需手工改客户端文件）。
func UpdateClient(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		row, err := getClientRow(sc, id)
		if err != nil {
			httpx.Fail(w, http.StatusNotFound, "客户端不存在")
			return
		}
		net, err := getNetwork(sc, row.NetworkID)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		var req struct {
			Name           *string               `json:"name"`
			IPv4           *string               `json:"ipv4"`
			IPv6           *string               `json:"ipv6"`
			Groups         *string               `json:"groups"`
			Subnets        *string               `json:"subnets"`
			ClientSettings *model.ClientOverride `json:"client_settings"`
		}
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}

		name := row.Name
		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			name = strings.TrimSpace(*req.Name)
		}
		groups := row.Groups
		if req.Groups != nil {
			groups = strings.TrimSpace(*req.Groups)
		}
		subnets := row.Subnets
		if req.Subnets != nil {
			subnets = strings.TrimSpace(*req.Subnets)
		}
		settings := row.ClientSettings
		if req.ClientSettings != nil {
			settings = *req.ClientSettings
		}
		if err := validateClientOverride(&settings); err != nil {
			httpx.Fail(w, http.StatusBadRequest, err.Error())
			return
		}

		// ---- IP 变更：先分配新地址，成功后再释放旧地址 ----
		newIPv4, newIPv6 := row.IPv4, row.IPv6
		var allocIPv4, allocIPv6 string
		if req.IPv4 != nil {
			want := strings.TrimSpace(*req.IPv4)
			if want != row.IPv4 {
				if net.Cidr4 == "" {
					if want != "" {
						httpx.Fail(w, http.StatusBadRequest, "该网络未配置 IPv4 网段")
						return
					}
				} else {
					ip, aerr := sc.IPAM.Allocate(net.ID, net.Cidr4, want, []string{net.Gateway4})
					if aerr != nil {
						httpx.Fail(w, http.StatusBadRequest, aerr.Error())
						return
					}
					allocIPv4 = ip
					newIPv4 = ip
				}
			}
		}
		if req.IPv6 != nil {
			want := strings.TrimSpace(*req.IPv6)
			if want != row.IPv6 {
				if net.Cidr6 == "" {
					if want != "" {
						rollbackIPs(sc, net.ID, allocIPv4)
						httpx.Fail(w, http.StatusBadRequest, "该网络未配置 IPv6 网段")
						return
					}
				} else {
					ip, aerr := sc.IPAM.Allocate(net.ID, net.Cidr6, want, []string{net.Gateway6})
					if aerr != nil {
						rollbackIPs(sc, net.ID, allocIPv4)
						httpx.Fail(w, http.StatusBadRequest, aerr.Error())
						return
					}
					allocIPv6 = ip
					newIPv6 = ip
				}
			}
		}

		// 证书与地址/名称/分组绑定：相关字段变化时重新签发。
		certChanged := name != row.Name || newIPv4 != row.IPv4 || newIPv6 != row.IPv6 ||
			groups != row.Groups || subnets != row.Subnets
		certPEM := row.CertPEM
		if certChanged {
			caCert, caKey, cerr := getCA(sc, net.ID)
			if cerr != nil {
				rollbackIPs(sc, net.ID, allocIPv4, allocIPv6)
				httpx.FailErr(w, http.StatusInternalServerError, cerr)
				return
			}
			certPEM, cerr = pki.IssueNodeCert(
				&pki.CA{CertPEM: caCert, KeyPEM: caKey},
				&pki.NodeKey{PublicKeyPEM: row.PublicKey, PrivateKeyPEM: row.KeyPEM},
				name, nodePrefixes(net, newIPv4, newIPv6), parsePrefixes(subnets), splitList(groups),
				3*365*24*time.Hour)
			if cerr != nil {
				rollbackIPs(sc, net.ID, allocIPv4, allocIPv6)
				httpx.FailErr(w, http.StatusInternalServerError, cerr)
				return
			}
		}

		settingsJSON := ""
		if settings != (model.ClientOverride{}) {
			b, merr := json.Marshal(settings)
			if merr != nil {
				rollbackIPs(sc, net.ID, allocIPv4, allocIPv6)
				httpx.FailErr(w, http.StatusInternalServerError, merr)
				return
			}
			settingsJSON = string(b)
		}

		now := nowStr()
		q := sc.Store.Rebind(`UPDATE client_nodes
			SET name=$1, ipv4=$2, ipv6=$3, groups=$4, subnets=$5, cert_pem=$6,
			    client_settings=$7, config_rev=config_rev+1, updated_at=$8
			WHERE id=$9`)
		if _, err := sc.Store.DB.Exec(q, name, newIPv4, newIPv6, groups, subnets, certPEM, settingsJSON, now, id); err != nil {
			rollbackIPs(sc, net.ID, allocIPv4, allocIPv6)
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		// 释放旧地址并绑定新地址。
		if newIPv4 != row.IPv4 {
			if row.IPv4 != "" {
				_ = sc.IPAM.ReleaseIP(net.ID, row.IPv4)
			}
			if newIPv4 != "" {
				_ = sc.IPAM.BindNode(net.ID, newIPv4, id)
			}
		}
		if newIPv6 != row.IPv6 {
			if row.IPv6 != "" {
				_ = sc.IPAM.ReleaseIP(net.ID, row.IPv6)
			}
			if newIPv6 != "" {
				_ = sc.IPAM.BindNode(net.ID, newIPv6, id)
			}
		}

		updated, err := getClientRow(sc, id)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, updated.ClientNode)
	}
}

// validateClientOverride 校验并归一化每主机客户端参数。
func validateClientOverride(ov *model.ClientOverride) error {
	if ov == nil {
		return nil
	}
	ov.DNS = strings.TrimSpace(ov.DNS)
	for _, d := range strings.Split(ov.DNS, ",") {
		if d = strings.TrimSpace(d); d != "" && stdnet.ParseIP(d) == nil {
			return &clientSettingsError{"DNS 含非法 IP: " + d}
		}
	}
	if ov.MTU != 0 && (ov.MTU < 576 || ov.MTU > 9000) {
		return &clientSettingsError{"MTU 应在 576-9000 之间（0 表示跟随默认）"}
	}
	if ov.Interval != "" {
		d, err := time.ParseDuration(strings.TrimSpace(ov.Interval))
		if err != nil || d <= 0 {
			return &clientSettingsError{"心跳间隔格式非法（示例 30s、1m）"}
		}
		ov.Interval = strings.TrimSpace(ov.Interval)
	}
	switch strings.ToLower(strings.TrimSpace(ov.DataPlaneMode)) {
	case "":
	case "auto", "tcp", "udp":
		ov.DataPlaneMode = strings.ToLower(strings.TrimSpace(ov.DataPlaneMode))
	default:
		return &clientSettingsError{"数据面模式只能是 auto / tcp / udp（留空跟随默认）"}
	}
	return nil
}

type clientSettingsError struct{ msg string }

func (e *clientSettingsError) Error() string { return e.msg }

// ListClients 列出客户端。
func ListClients(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := sc.Store.Rebind(`SELECT id,network_id,name,ipv4,ipv6,groups,subnets,os,arch,kernel,version,status,last_seen,config_rev,created_at,updated_at,client_settings FROM client_nodes ORDER BY id`)
		rows, err := sc.Store.DB.Query(q)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		list := []model.ClientNode{}
		for rows.Next() {
			var n model.ClientNode
			if err := rows.Scan(&n.ID, &n.NetworkID, &n.Name, &n.IPv4, &n.IPv6, &n.Groups, &n.Subnets, &n.OS, &n.Arch, &n.Kernel, &n.Version, &n.Status, &n.LastSeen, &n.ConfigRev, &n.CreatedAt, &n.UpdatedAt, &n.ClientSettings); err != nil {
				httpx.FailErr(w, http.StatusInternalServerError, err)
				return
			}
			list = append(list, n)
		}
		httpx.OK(w, list)
	}
}

// CreateClient 创建客户端：分配 IP、签发证书、生成 enroll token。
func CreateClient(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createClientReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		if req.Name == "" || req.NetworkID == 0 {
			httpx.Fail(w, http.StatusBadRequest, "name 与 network_id 必填")
			return
		}

		net, err := getNetwork(sc, req.NetworkID)
		if err != nil {
			httpx.Fail(w, http.StatusNotFound, "网络不存在")
			return
		}
		caCert, caKey, err := getCA(sc, net.ID)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		// 分配 IP（失败回滚）
		var ipv4, ipv6 string
		if net.Cidr4 != "" {
			ipv4, err = sc.IPAM.Allocate(net.ID, net.Cidr4, req.IPv4, []string{net.Gateway4})
			if err != nil {
				httpx.Fail(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if net.Cidr6 != "" {
			ipv6, err = sc.IPAM.Allocate(net.ID, net.Cidr6, req.IPv6, []string{net.Gateway6})
			if err != nil {
				if ipv4 != "" {
					_ = sc.IPAM.ReleaseIP(net.ID, ipv4)
				}
				httpx.Fail(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		nodeKey, err := pki.GenerateNodeKey()
		if err != nil {
			rollbackIPs(sc, net.ID, ipv4, ipv6)
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		networks := nodePrefixes(net, ipv4, ipv6)
		unsafe := parsePrefixes(req.Subnets)
		groups := splitList(req.Groups)
		if err := validateClientOverride(req.ClientSettings); err != nil {
			rollbackIPs(sc, net.ID, ipv4, ipv6)
			httpx.Fail(w, http.StatusBadRequest, err.Error())
			return
		}
		settings := model.ClientOverride{}
		settingsJSON := ""
		if req.ClientSettings != nil {
			settings = *req.ClientSettings
			if settings != (model.ClientOverride{}) {
				b, merr := json.Marshal(settings)
				if merr != nil {
					rollbackIPs(sc, net.ID, ipv4, ipv6)
					httpx.FailErr(w, http.StatusInternalServerError, merr)
					return
				}
				settingsJSON = string(b)
			}
		}
		certPEM, err := pki.IssueNodeCert(&pki.CA{CertPEM: caCert, KeyPEM: caKey}, nodeKey, req.Name, networks, unsafe, groups, 3*365*24*time.Hour)
		if err != nil {
			rollbackIPs(sc, net.ID, ipv4, ipv6)
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		token, err := randomToken()
		if err != nil {
			rollbackIPs(sc, net.ID, ipv4, ipv6)
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		now := nowStr()
		var nid int64
		err = sc.Store.DB.QueryRow(sc.Store.Rebind(`INSERT INTO client_nodes
			(network_id,name,ipv4,ipv6,public_key,cert_pem,key_pem,groups,subnets,enroll_token_hash,created_at,updated_at,client_settings)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`),
			net.ID, req.Name, ipv4, ipv6, nodeKey.PublicKeyPEM, certPEM, nodeKey.PrivateKeyPEM, req.Groups, req.Subnets, sha256hex(token), now, now, settingsJSON).Scan(&nid)
		if err != nil {
			rollbackIPs(sc, net.ID, ipv4, ipv6)
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		if ipv4 != "" {
			_ = sc.IPAM.BindNode(net.ID, ipv4, nid)
		}
		if ipv6 != "" {
			_ = sc.IPAM.BindNode(net.ID, ipv6, nid)
		}

		httpx.OK(w, map[string]any{
			"client":          model.ClientNode{ID: nid, NetworkID: net.ID, Name: req.Name, IPv4: ipv4, IPv6: ipv6, Groups: req.Groups, Subnets: req.Subnets, Status: "offline", CreatedAt: now, UpdatedAt: now, ClientSettings: settings},
			"enroll_token":    token,
			"install_command": enrollCommand(baseURL(sc, r), token, req.Name),
		})
	}
}

// GetClient 单个客户端。
func GetClient(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		row, err := getClientRow(sc, id)
		if err != nil {
			httpx.Fail(w, http.StatusNotFound, "客户端不存在")
			return
		}
		httpx.OK(w, row.ClientNode)
	}
}

// DeleteClient 删除客户端并释放 IP。
func DeleteClient(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		_ = sc.IPAM.ReleaseNode(id)
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`DELETE FROM client_nodes WHERE id = $1`), id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, nil)
	}
}

// ClientEnrollToken 重新生成 enroll token。
func ClientEnrollToken(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		row, err := getClientRow(sc, id)
		if err != nil {
			httpx.Fail(w, http.StatusNotFound, "客户端不存在")
			return
		}
		token, err := randomToken()
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE client_nodes SET enroll_token_hash = $1, updated_at = $2 WHERE id = $3`),
			sha256hex(token), nowStr(), id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, map[string]any{"enroll_token": token, "install_command": enrollCommand(baseURL(sc, r), token, row.Name)})
	}
}

// ClientBundle 返回客户端配置包（证书 + 配置）。
func ClientBundle(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		row, err := getClientRow(sc, id)
		if err != nil {
			httpx.Fail(w, http.StatusNotFound, "客户端不存在")
			return
		}
		b, err := buildBundle(sc, row)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, b)
	}
}

func getClientRow(sc *svc.ServiceContext, id int64) (*clientRow, error) {
	var c clientRow
	q := sc.Store.Rebind(`SELECT id,network_id,name,ipv4,ipv6,public_key,cert_pem,key_pem,groups,subnets,os,arch,kernel,version,status,last_seen,config_rev,secret_hash,created_at,updated_at,client_settings FROM client_nodes WHERE id = $1`)
	err := sc.Store.DB.QueryRow(q, id).Scan(
		&c.ID, &c.NetworkID, &c.Name, &c.IPv4, &c.IPv6, &c.PublicKey, &c.CertPEM, &c.KeyPEM,
		&c.Groups, &c.Subnets, &c.OS, &c.Arch, &c.Kernel, &c.Version, &c.Status, &c.LastSeen,
		&c.ConfigRev, &c.SecretHash, &c.CreatedAt, &c.UpdatedAt, &c.ClientSettings)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// buildBundle 组装配置包（Nebula 证书 + 完整 Nebula 配置）。
func buildBundle(sc *svc.ServiceContext, c *clientRow) (*model.Bundle, error) {
	net, err := getNetwork(sc, c.NetworkID)
	if err != nil {
		return nil, err
	}
	caCert, _, err := getCA(sc, net.ID)
	if err != nil {
		return nil, err
	}
	input, err := buildNebulaInput(sc, c.NetworkID, false, 0, false)
	if err != nil {
		return nil, err
	}
	cfgBytes, err := configgen.Build(*input)
	if err != nil {
		return nil, err
	}
	_, routeSpecs, err := loadRouteConfig(sc, c.NetworkID)
	if err != nil {
		return nil, err
	}
	// 参数优先级：主机级覆盖 > 网络级 > 全局「设置 → 客户端下发」默认。
	cfg := sc.Config
	if sc.Settings != nil {
		snap := sc.Settings.Snapshot()
		cfg = &snap
	}
	dns := ""
	mtu := 0
	interval := "30s"
	mode := "auto"
	if cfg != nil {
		dns = strings.TrimSpace(cfg.Client.DNS)
		mtu = cfg.Client.MTU
		if v := strings.TrimSpace(cfg.Client.Interval); v != "" {
			interval = v
		}
		if v := strings.TrimSpace(cfg.TCPRelay.Mode); v != "" {
			mode = v
		}
	}
	if v := strings.TrimSpace(net.DNS); v != "" {
		dns = v
	}
	if net.MTU > 0 {
		mtu = net.MTU
	}
	ov := c.ClientSettings
	if v := strings.TrimSpace(ov.DNS); v != "" {
		dns = v
	}
	if ov.MTU > 0 {
		mtu = ov.MTU
	}
	if v := strings.TrimSpace(ov.Interval); v != "" {
		interval = v
	}
	if v := strings.TrimSpace(ov.DataPlaneMode); v != "" {
		mode = v
	}
	// 每台主机可单独关闭服务端 DNS；关闭后 Bundle 不下发 DNS。
	if ov.UseServerDNS != nil && !*ov.UseServerDNS {
		dns = ""
	}

	clientSet := model.ClientSettings{DataPlaneMode: mode, Interval: interval}
	if sc.Relay != nil {
		info := sc.Relay.Info()
		if info.Port > 0 {
			clientSet.TCPRelayToken = info.Token
			adv := strings.TrimSpace(info.Advertise)
			if adv == "" && cfg != nil {
				if u, err := url.Parse(cfg.Server.ExternalURL); err == nil && u.Hostname() != "" {
					adv = stdnet.JoinHostPort(u.Hostname(), strconv.Itoa(info.Port))
				}
			}
			if adv != "" {
				clientSet.TCPRelay = adv
			}
		}
	}
	return &model.Bundle{
		Name: c.Name, Network: net.Name, Cidr4: net.Cidr4, Cidr6: net.Cidr6,
		IPv4: c.IPv4, IPv6: c.IPv6, MTU: mtu, DNS: dns,
		CA: caCert, Cert: c.CertPEM, Key: c.KeyPEM, Config: string(cfgBytes), ConfigRev: c.ConfigRev,
		Routes: routeSpecs, Client: clientSet,
	}, nil
}

func enrollCommand(baseURL, token, name string) string {
	return "iflygo-client enroll --server " + baseURL + " --token " + token + " --name " + name
}

// nodePrefixes 用网络前缀长度构造节点的证书 networks。
func nodePrefixes(net *model.Network, ipv4, ipv6 string) []netip.Prefix {
	var out []netip.Prefix
	if ipv4 != "" && net.Cidr4 != "" {
		if p, err := netip.ParsePrefix(net.Cidr4); err == nil {
			if a, err := netip.ParseAddr(ipv4); err == nil {
				out = append(out, netip.PrefixFrom(a, p.Bits()))
			}
		}
	}
	if ipv6 != "" && net.Cidr6 != "" {
		if p, err := netip.ParsePrefix(net.Cidr6); err == nil {
			if a, err := netip.ParseAddr(ipv6); err == nil {
				out = append(out, netip.PrefixFrom(a, p.Bits()))
			}
		}
	}
	return out
}

func parsePrefixes(csv string) []netip.Prefix {
	var out []netip.Prefix
	for _, s := range splitList(csv) {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func splitList(csv string) []string {
	var out []string
	for _, s := range strings.Split(csv, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func rollbackIPs(sc *svc.ServiceContext, networkID int64, ips ...string) {
	for _, ip := range ips {
		if ip != "" {
			_ = sc.IPAM.ReleaseIP(networkID, ip)
		}
	}
}
