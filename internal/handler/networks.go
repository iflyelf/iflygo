package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/pki"
	"github.com/iflyelf/iflygo/internal/svc"
)

type networkReq struct {
	Name     string               `json:"name"`
	Cidr4    string               `json:"cidr4"`
	Cidr6    string               `json:"cidr6"`
	Gateway4 string               `json:"gateway4"`
	Gateway6 string               `json:"gateway6"`
	DNS      string               `json:"dns"`
	MTU      int                  `json:"mtu"`
	Nebula   model.NebulaSettings `json:"nebula"`
}

// ListNetworks 列出网络。
func ListNetworks(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := sc.Store.DB.Query(sc.Store.Rebind(`SELECT id,name,cidr4,cidr6,gateway4,gateway6,dns,mtu,created_at FROM networks ORDER BY id`))
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		list := []model.Network{}
		for rows.Next() {
			var n model.Network
			if err := rows.Scan(&n.ID, &n.Name, &n.Cidr4, &n.Cidr6, &n.Gateway4, &n.Gateway6, &n.DNS, &n.MTU, &n.CreatedAt); err != nil {
				httpx.FailErr(w, http.StatusInternalServerError, err)
				return
			}
			list = append(list, n)
		}
		httpx.OK(w, list)
	}
}

// CreateNetwork 创建网络并生成其 CA。
func CreateNetwork(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req networkReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		if req.Name == "" || req.Cidr4 == "" {
			httpx.Fail(w, http.StatusBadRequest, "name 与 cidr4 必填")
			return
		}
		if req.MTU == 0 {
			req.MTU = 1300
		}

		ca, err := pki.CreateCA("iFlyGo CA ("+req.Name+")", 10*365*24*time.Hour)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		now := nowStr()
		nebulaJSON, err := json.Marshal(req.Nebula)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		var nid int64
		err = sc.Store.DB.QueryRow(sc.Store.Rebind(`INSERT INTO networks (name,cidr4,cidr6,gateway4,gateway6,dns,mtu,nebula,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`),
			req.Name, req.Cidr4, req.Cidr6, req.Gateway4, req.Gateway6, req.DNS, req.MTU, string(nebulaJSON), now).Scan(&nid)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`INSERT INTO cas (network_id,name,cert_pem,key_pem,fingerprint,expires_at,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`),
			nid, ca.Name, ca.CertPEM, ca.KeyPEM, ca.Fingerprint, ca.ExpiresAt.UTC().Format(time.RFC3339), now); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		httpx.OK(w, model.Network{ID: nid, Name: req.Name, Cidr4: req.Cidr4, Cidr6: req.Cidr6, Gateway4: req.Gateway4, Gateway6: req.Gateway6, DNS: req.DNS, MTU: req.MTU, CreatedAt: now})
	}
}

// DeleteNetwork 删除网络（级联删除 CA/节点/地址）。
func DeleteNetwork(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`DELETE FROM networks WHERE id = $1`), id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, nil)
	}
}

// getNetwork 读取网络。
func getNetwork(sc *svc.ServiceContext, id int64) (*model.Network, error) {
	var n model.Network
	q := sc.Store.Rebind(`SELECT id,name,cidr4,cidr6,gateway4,gateway6,dns,mtu,created_at FROM networks WHERE id = $1`)
	if err := sc.Store.DB.QueryRow(q, id).Scan(&n.ID, &n.Name, &n.Cidr4, &n.Cidr6, &n.Gateway4, &n.Gateway6, &n.DNS, &n.MTU, &n.CreatedAt); err != nil {
		return nil, err
	}
	return &n, nil
}

// getCA 读取网络的 CA。
func getCA(sc *svc.ServiceContext, networkID int64) (certPEM, keyPEM string, err error) {
	q := sc.Store.Rebind(`SELECT cert_pem, key_pem FROM cas WHERE network_id = $1 ORDER BY id DESC LIMIT 1`)
	err = sc.Store.DB.QueryRow(q, networkID).Scan(&certPEM, &keyPEM)
	return
}

// getNebulaSettings 读取网络的 Nebula 设置。
func getNebulaSettings(sc *svc.ServiceContext, networkID int64) (*model.NebulaSettings, error) {
	var raw string
	q := sc.Store.Rebind(`SELECT nebula FROM networks WHERE id = $1`)
	if err := sc.Store.DB.QueryRow(q, networkID).Scan(&raw); err != nil {
		return nil, err
	}
	s := &model.NebulaSettings{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func tunnelID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}
