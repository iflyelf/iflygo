package handler

import (
	"net/http"
	"net/netip"
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
}

// ListClients 列出客户端。
func ListClients(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := sc.Store.Rebind(`SELECT id,network_id,name,ipv4,ipv6,groups,subnets,os,arch,kernel,version,status,last_seen,config_rev,created_at,updated_at FROM client_nodes ORDER BY id`)
		rows, err := sc.Store.DB.Query(q)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		list := []model.ClientNode{}
		for rows.Next() {
			var n model.ClientNode
			if err := rows.Scan(&n.ID, &n.NetworkID, &n.Name, &n.IPv4, &n.IPv6, &n.Groups, &n.Subnets, &n.OS, &n.Arch, &n.Kernel, &n.Version, &n.Status, &n.LastSeen, &n.ConfigRev, &n.CreatedAt, &n.UpdatedAt); err != nil {
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
			(network_id,name,ipv4,ipv6,public_key,cert_pem,key_pem,groups,subnets,enroll_token_hash,created_at,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`),
			net.ID, req.Name, ipv4, ipv6, nodeKey.PublicKeyPEM, certPEM, nodeKey.PrivateKeyPEM, req.Groups, req.Subnets, sha256hex(token), now, now).Scan(&nid)
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
			"client":          model.ClientNode{ID: nid, NetworkID: net.ID, Name: req.Name, IPv4: ipv4, IPv6: ipv6, Groups: req.Groups, Subnets: req.Subnets, Status: "offline", CreatedAt: now, UpdatedAt: now},
			"enroll_token":    token,
			"install_command": enrollCommand(baseURL(sc, r), token, req.Name),
		})
	}
}

// GetClient 单个客户端。
func GetClient(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
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
		id, err := pathID(r)
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
		id, err := pathID(r)
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
		id, err := pathID(r)
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
	q := sc.Store.Rebind(`SELECT id,network_id,name,ipv4,ipv6,public_key,cert_pem,key_pem,groups,subnets,os,arch,kernel,version,status,last_seen,config_rev,secret_hash,created_at,updated_at FROM client_nodes WHERE id = $1`)
	err := sc.Store.DB.QueryRow(q, id).Scan(
		&c.ID, &c.NetworkID, &c.Name, &c.IPv4, &c.IPv6, &c.PublicKey, &c.CertPEM, &c.KeyPEM,
		&c.Groups, &c.Subnets, &c.OS, &c.Arch, &c.Kernel, &c.Version, &c.Status, &c.LastSeen,
		&c.ConfigRev, &c.SecretHash, &c.CreatedAt, &c.UpdatedAt)
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
	return &model.Bundle{
		Network: net.Name, IPv4: c.IPv4, IPv6: c.IPv6, MTU: net.MTU, DNS: net.DNS,
		CA: caCert, Cert: c.CertPEM, Key: c.KeyPEM, Config: string(cfgBytes), ConfigRev: c.ConfigRev,
		Routes: routeSpecs,
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
