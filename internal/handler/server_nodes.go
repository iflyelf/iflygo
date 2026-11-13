package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/configgen"
	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/pki"
	"github.com/iflyelf/iflygo/internal/svc"
)

type serverNodeReq struct {
	NetworkID  int64  `json:"network_id"`
	Name       string `json:"name"`
	Roles      string `json:"roles"` // lighthouse,relay,edge,gateway
	Region     string `json:"region"`
	OverlayIP4 string `json:"overlay_ip4"`
	OverlayIP6 string `json:"overlay_ip6"`
	PublicHost string `json:"public_host"`
	PublicPort int    `json:"public_port"`
}

// ListServerNodes 列出服务端节点。
func ListServerNodes(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := sc.Store.Rebind(`SELECT id,network_id,name,roles,region,overlay_ip4,overlay_ip6,public_host,public_port,created_at FROM server_nodes ORDER BY id`)
		rows, err := sc.Store.DB.Query(q)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		list := []model.ServerNode{}
		for rows.Next() {
			var n model.ServerNode
			if err := rows.Scan(&n.ID, &n.NetworkID, &n.Name, &n.Roles, &n.Region, &n.OverlayIP4, &n.OverlayIP6, &n.PublicHost, &n.PublicPort, &n.CreatedAt); err != nil {
				httpx.FailErr(w, http.StatusInternalServerError, err)
				return
			}
			n.Health = publicHostHealth(sc, n.PublicHost)
			list = append(list, n)
		}
		httpx.OK(w, list)
	}
}

// CreateServerNode 创建服务端节点（分配 IP、签发证书）。
func CreateServerNode(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req serverNodeReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		name, nerr := normalizeTunnelName(req.Name)
		if nerr != nil {
			httpx.Fail(w, http.StatusBadRequest, nerr.Error())
			return
		}
		if req.NetworkID == 0 {
			httpx.Fail(w, http.StatusBadRequest, "network_id 必填")
			return
		}
		req.Name = name
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

		var ip4, ip6 string
		if net.Cidr4 != "" {
			ip4, err = sc.IPAM.Allocate(net.ID, net.Cidr4, req.OverlayIP4, []string{net.Gateway4})
			if err != nil {
				httpx.Fail(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if net.Cidr6 != "" {
			ip6, err = sc.IPAM.Allocate(net.ID, net.Cidr6, req.OverlayIP6, []string{net.Gateway6})
			if err != nil {
				if ip4 != "" {
					_ = sc.IPAM.ReleaseIP(net.ID, ip4)
				}
				httpx.Fail(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		nodeKey, err := pki.GenerateNodeKey()
		if err != nil {
			rollbackIPs(sc, net.ID, ip4, ip6)
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		certPEM, err := pki.IssueNodeCert(&pki.CA{CertPEM: caCert, KeyPEM: caKey}, nodeKey, req.Name,
			nodePrefixes(net, ip4, ip6), nil, nil, 3*365*24*time.Hour)
		if err != nil {
			rollbackIPs(sc, net.ID, ip4, ip6)
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		now := nowStr()
		var nid int64
		err = sc.Store.DB.QueryRow(sc.Store.Rebind(`INSERT INTO server_nodes
			(network_id,name,roles,region,overlay_ip4,overlay_ip6,public_host,public_port,public_key,cert_pem,key_pem,created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`),
			net.ID, req.Name, req.Roles, req.Region, ip4, ip6, req.PublicHost, req.PublicPort,
			nodeKey.PublicKeyPEM, certPEM, nodeKey.PrivateKeyPEM, now).Scan(&nid)
		if err != nil {
			rollbackIPs(sc, net.ID, ip4, ip6)
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		if ip4 != "" {
			_ = sc.IPAM.BindNode(net.ID, ip4, nid)
		}
		if ip6 != "" {
			_ = sc.IPAM.BindNode(net.ID, ip6, nid)
		}

		triggerGatewayReconcile(sc)
		httpx.OK(w, model.ServerNode{ID: nid, NetworkID: net.ID, Name: req.Name, Roles: req.Roles, Region: req.Region,
			OverlayIP4: ip4, OverlayIP6: ip6, PublicHost: req.PublicHost, PublicPort: req.PublicPort, CreatedAt: now})
	}
}

// UpdateServerNode 更新服务端节点（名称/角色/公网入口/overlay IP）。
// overlay IP 变更会重新签发证书并更新 IPAM 绑定。
func UpdateServerNode(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		var row struct {
			NetworkID  int64
			Name       string
			Roles      string
			Region     string
			OverlayIP4 string
			OverlayIP6 string
			PublicHost string
			PublicPort int
			PublicKey  string
			KeyPEM     string
		}
		q := sc.Store.Rebind(`SELECT network_id,name,roles,region,overlay_ip4,overlay_ip6,public_host,public_port,public_key,key_pem FROM server_nodes WHERE id = $1`)
		if err := sc.Store.DB.QueryRow(q, id).Scan(&row.NetworkID, &row.Name, &row.Roles, &row.Region,
			&row.OverlayIP4, &row.OverlayIP6, &row.PublicHost, &row.PublicPort, &row.PublicKey, &row.KeyPEM); err != nil {
			httpx.Fail(w, http.StatusNotFound, "服务端节点不存在")
			return
		}
		net, err := getNetwork(sc, row.NetworkID)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		var req serverNodeReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = row.Name
		} else {
			n, nerr := normalizeTunnelName(name)
			if nerr != nil {
				httpx.Fail(w, http.StatusBadRequest, nerr.Error())
				return
			}
			name = n
		}
		roles := strings.TrimSpace(req.Roles)
		if roles == "" {
			roles = row.Roles
		}
		region := strings.TrimSpace(req.Region)
		if region == "" {
			region = row.Region
		}
		host := strings.TrimSpace(req.PublicHost)
		port := req.PublicPort
		if port == 0 {
			port = row.PublicPort
		}

		ip4, ip6 := row.OverlayIP4, row.OverlayIP6
		var alloc4, alloc6 string
		want4 := strings.TrimSpace(req.OverlayIP4)
		if want4 != row.OverlayIP4 {
			if net.Cidr4 == "" {
				httpx.Fail(w, http.StatusBadRequest, "该网络未配置 IPv4 网段")
				return
			}
			ip, aerr := sc.IPAM.Allocate(net.ID, net.Cidr4, want4, []string{net.Gateway4})
			if aerr != nil {
				httpx.Fail(w, http.StatusBadRequest, aerr.Error())
				return
			}
			alloc4, ip4 = ip, ip
		}
		want6 := strings.TrimSpace(req.OverlayIP6)
		if want6 != row.OverlayIP6 {
			if net.Cidr6 == "" {
				rollbackIPs(sc, net.ID, alloc4)
				httpx.Fail(w, http.StatusBadRequest, "该网络未配置 IPv6 网段")
				return
			}
			ip, aerr := sc.IPAM.Allocate(net.ID, net.Cidr6, want6, []string{net.Gateway6})
			if aerr != nil {
				rollbackIPs(sc, net.ID, alloc4)
				httpx.Fail(w, http.StatusBadRequest, aerr.Error())
				return
			}
			alloc6, ip6 = ip, ip
		}

		certChanged := name != row.Name || ip4 != row.OverlayIP4 || ip6 != row.OverlayIP6 || roles != row.Roles
		certPEM := ""
		if certChanged {
			caCert, caKey, cerr := getCA(sc, net.ID)
			if cerr != nil {
				rollbackIPs(sc, net.ID, alloc4, alloc6)
				httpx.FailErr(w, http.StatusInternalServerError, cerr)
				return
			}
			certPEM, cerr = pki.IssueNodeCert(&pki.CA{CertPEM: caCert, KeyPEM: caKey},
				&pki.NodeKey{PublicKeyPEM: row.PublicKey, PrivateKeyPEM: row.KeyPEM},
				name, nodePrefixes(net, ip4, ip6), nil, splitList(roles), 3*365*24*time.Hour)
			if cerr != nil {
				rollbackIPs(sc, net.ID, alloc4, alloc6)
				httpx.FailErr(w, http.StatusInternalServerError, cerr)
				return
			}
		}

		if certChanged {
			_, err = sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE server_nodes SET name=$1,roles=$2,region=$3,overlay_ip4=$4,overlay_ip6=$5,public_host=$6,public_port=$7,cert_pem=$8 WHERE id=$9`),
				name, roles, region, ip4, ip6, host, port, certPEM, id)
		} else {
			_, err = sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE server_nodes SET name=$1,roles=$2,region=$3,overlay_ip4=$4,overlay_ip6=$5,public_host=$6,public_port=$7 WHERE id=$8`),
				name, roles, region, ip4, ip6, host, port, id)
		}
		if err != nil {
			rollbackIPs(sc, net.ID, alloc4, alloc6)
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		if ip4 != row.OverlayIP4 {
			if row.OverlayIP4 != "" {
				_ = sc.IPAM.ReleaseIP(net.ID, row.OverlayIP4)
			}
			if ip4 != "" {
				_ = sc.IPAM.BindNode(net.ID, ip4, id)
			}
		}
		if ip6 != row.OverlayIP6 {
			if row.OverlayIP6 != "" {
				_ = sc.IPAM.ReleaseIP(net.ID, row.OverlayIP6)
			}
			if ip6 != "" {
				_ = sc.IPAM.BindNode(net.ID, ip6, id)
			}
		}
		bumpConfigRev(sc, net.ID)
		triggerGatewayReconcile(sc)
		httpx.OK(w, model.ServerNode{ID: id, NetworkID: net.ID, Name: name, Roles: roles, Region: region,
			OverlayIP4: ip4, OverlayIP6: ip6, PublicHost: host, PublicPort: port})
	}
}

// DeleteServerNode 删除服务端节点。
func DeleteServerNode(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		_ = sc.IPAM.ReleaseNode(id)
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`DELETE FROM server_nodes WHERE id = $1`), id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, nil)
	}
}

// ServerNodeBundle 返回服务端节点的配置包（ca/cert/key/nebula 配置）。
func ServerNodeBundle(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		var n model.ServerNode
		var certPEM, keyPEM string
		q := sc.Store.Rebind(`SELECT id,network_id,name,roles,overlay_ip4,overlay_ip6,public_host,public_port,cert_pem,key_pem FROM server_nodes WHERE id = $1`)
		if err := sc.Store.DB.QueryRow(q, id).Scan(&n.ID, &n.NetworkID, &n.Name, &n.Roles, &n.OverlayIP4, &n.OverlayIP6, &n.PublicHost, &n.PublicPort, &certPEM, &keyPEM); err != nil {
			httpx.Fail(w, http.StatusNotFound, "服务端节点不存在")
			return
		}
		caCert, _, err := getCA(sc, n.NetworkID)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		net, err := getNetwork(sc, n.NetworkID)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		isLighthouse := strings.Contains(n.Roles, "lighthouse")
		isRelay := strings.Contains(n.Roles, "relay")
		input, err := buildNebulaInput(sc, n.NetworkID, isLighthouse, n.PublicPort, isRelay)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		cfg, err := configgen.Build(*input)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		bundle := model.Bundle{CA: caCert, Cert: certPEM, Key: keyPEM, Config: string(cfg)}
		// 网关节点的部署包直接携带 NAT 脚本，与节点/Agent 部署流程合并。
		if strings.Contains(n.Roles, "gateway") {
			switch effectiveEgressMode(sc, net) {
			case "full":
				gw4, gw6 := "", ""
				if net.Cidr4 != "" {
					gw4 = resolveDNSGateway(sc, net, true)
				}
				if net.Cidr6 != "" {
					gw6 = resolveDNSGateway(sc, net, false)
				}
				bundle.BootstrapScript = buildFullNATScript(net, gw4, gw6, egressExcludePrefixes(sc, net))
			case "dns":
				v4, v6, gw4, gw6, nerr := computeDNSNAT(sc, net, "", "")
				if nerr == nil {
					bundle.BootstrapScript = buildDNSNATScript(net, v4, v6, gw4, gw6)
				}
			}
		}
		httpx.OK(w, bundle)
	}
}
