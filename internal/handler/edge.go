package handler

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/iflyelf/iflygo/internal/edge"
	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/svc"
)

type edgeServiceReq struct {
	NetworkID int64  `json:"network_id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Target    string `json:"target"`
	Protocol  string `json:"protocol"`
	TLSMode   string `json:"tls_mode"`
}

// ListEdgeServices 列出边缘服务。
func ListEdgeServices(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var rows *sql.Rows
		var err error
		if nidStr := r.URL.Query().Get("network_id"); nidStr != "" {
			nid, _ := strconv.ParseInt(nidStr, 10, 64)
			rows, err = sc.Store.DB.Query(sc.Store.Rebind(`SELECT id,network_id,name,host,target,protocol,tls_mode,enabled,created_at FROM edge_services WHERE network_id = $1 ORDER BY id`), nid)
		} else {
			rows, err = sc.Store.DB.Query(sc.Store.Rebind(`SELECT id,network_id,name,host,target,protocol,tls_mode,enabled,created_at FROM edge_services ORDER BY id`))
		}
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		list := []model.EdgeService{}
		for rows.Next() {
			var s model.EdgeService
			var enabled int64
			if err := rows.Scan(&s.ID, &s.NetworkID, &s.Name, &s.Host, &s.Target, &s.Protocol, &s.TLSMode, &enabled, &s.CreatedAt); err != nil {
				httpx.FailErr(w, http.StatusInternalServerError, err)
				return
			}
			s.Enabled = enabled != 0
			list = append(list, s)
		}
		httpx.OK(w, list)
	}
}

// CreateEdgeService 创建边缘服务。
func CreateEdgeService(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req edgeServiceReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		if req.Host == "" || req.Target == "" || req.NetworkID == 0 {
			httpx.Fail(w, http.StatusBadRequest, "host、target、network_id 必填")
			return
		}
		if req.Protocol == "" {
			req.Protocol = "http"
		}
		if req.TLSMode == "" {
			req.TLSMode = "terminate"
		}
		now := nowStr()
		res, err := sc.Store.DB.Exec(sc.Store.Rebind(`INSERT INTO edge_services (network_id,name,host,target,protocol,tls_mode,enabled,created_at) VALUES ($1,$2,$3,$4,$5,$6,1,$7)`),
			req.NetworkID, req.Name, req.Host, req.Target, req.Protocol, req.TLSMode, now)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		id, _ := res.LastInsertId()
		httpx.OK(w, model.EdgeService{ID: id, NetworkID: req.NetworkID, Name: req.Name, Host: req.Host, Target: req.Target, Protocol: req.Protocol, TLSMode: req.TLSMode, Enabled: true, CreatedAt: now})
	}
}

// DeleteEdgeService 删除边缘服务。
func DeleteEdgeService(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`DELETE FROM edge_services WHERE id = $1`), id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, nil)
	}
}

// EdgeRoutes 返回边缘节点所需的路由表（域名 → overlay 目标）。
func EdgeRoutes(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		var networkID int64
		if err := sc.Store.DB.QueryRow(sc.Store.Rebind(`SELECT network_id FROM server_nodes WHERE id = $1`), id).Scan(&networkID); err != nil {
			httpx.Fail(w, http.StatusNotFound, "边缘节点不存在")
			return
		}
		rows, err := sc.Store.DB.Query(sc.Store.Rebind(`SELECT host,target FROM edge_services WHERE network_id = $1 AND enabled = 1 AND protocol IN ('http','https') ORDER BY id`), networkID)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		routes := []edge.Route{}
		for rows.Next() {
			var rt edge.Route
			if err := rows.Scan(&rt.Host, &rt.Target); err != nil {
				httpx.FailErr(w, http.StatusInternalServerError, err)
				return
			}
			routes = append(routes, rt)
		}
		httpx.OK(w, routes)
	}
}
