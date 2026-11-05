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

		httpx.OK(w, model.ServerNode{ID: nid, NetworkID: net.ID, Name: req.Name, Roles: req.Roles, Region: req.Region,
			OverlayIP4: ip4, OverlayIP6: ip6, PublicHost: req.PublicHost, PublicPort: req.PublicPort, CreatedAt: now})
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

		httpx.OK(w, model.Bundle{CA: caCert, Cert: certPEM, Key: keyPEM, Config: string(cfg)})
	}
}
