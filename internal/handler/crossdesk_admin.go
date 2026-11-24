package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/iflyelf/iflygo/internal/crossdesk"
	"github.com/iflyelf/iflygo/internal/pkg/audit"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/svc"
)

// ListCrossDeskDevices 原生信令设备列表（管理员）。
func ListCrossDeskDevices(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := sc.Store.DB.Query(sc.Store.Rebind(`SELECT device_id,kind,online,last_seen,created_at
			FROM crossdesk_devices ORDER BY created_at DESC LIMIT 1000`))
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		list := []map[string]any{}
		for rows.Next() {
			var id, kind, lastSeen, createdAt string
			var online int64
			if err := rows.Scan(&id, &kind, &online, &lastSeen, &createdAt); err != nil {
				httpx.FailErr(w, http.StatusInternalServerError, err)
				return
			}
			list = append(list, map[string]any{
				"device_id": id, "kind": kind, "online": online != 0,
				"last_seen": lastSeen, "created_at": createdAt,
			})
		}
		httpx.OK(w, list)
	}
}

// DeleteCrossDeskDevice 删除原生信令设备并断开在线连接。
func DeleteCrossDeskDevice(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" || len(id) > 128 {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		if sc.CrossDesk != nil {
			sc.CrossDesk.Kick(id)
		}
		_, _ = sc.Store.DB.Exec(sc.Store.Rebind(`DELETE FROM crossdesk_reconnect_credentials WHERE device_id = $1`), id)
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`DELETE FROM crossdesk_devices WHERE device_id = $1`), id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		auditOp(sc, r, audit.ActionCrossDeskDeviceDelete, "crossdesk_device", id, id, nil)
		httpx.OK(w, nil)
	}
}

// CrossDeskJoinTicket 为指定设备生成一次性 Web 连接票据（管理员）。
//
// Web 客户端携带 join_ticket 时无需再输入设备密码；票据 60 秒有效且只能使用一次。
func CrossDeskJoinTicket(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" || len(id) > 128 {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		var kind string
		if err := sc.Store.DB.QueryRow(sc.Store.Rebind(`SELECT COALESCE(kind,'desktop') FROM crossdesk_devices WHERE device_id = $1`), id).Scan(&kind); err != nil {
			httpx.Fail(w, http.StatusNotFound, "设备不存在")
			return
		}
		if kind != "desktop" {
			httpx.Fail(w, http.StatusBadRequest, "仅桌面设备支持免密连接")
			return
		}
		store := crossdesk.NewSQLStore(sc.Store)
		token, expires, err := store.IssueJoinTicket(id)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, map[string]any{
			"join_ticket": token,
			"expires_at":  expires,
			"join_url":    "/crossdesk/web/?device_id=" + url.QueryEscape(id) + "&join_ticket=" + url.QueryEscape(token),
		})
	}
}

// ListCrossDeskSessions 原生信令活动会话（管理员）。
func ListCrossDeskSessions(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sc.CrossDesk == nil {
			httpx.OK(w, []any{})
			return
		}
		httpx.OK(w, sc.CrossDesk.Sessions())
	}
}

// DisconnectCrossDeskSession 断开原生信令连接（管理员）。
func DisconnectCrossDeskSession(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			TransmissionID string `json:"transmission_id"`
			UserID         string `json:"user_id"`
		}
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		req.TransmissionID = strings.TrimSpace(req.TransmissionID)
		req.UserID = strings.TrimSpace(req.UserID)
		if req.TransmissionID == "" || req.UserID == "" {
			httpx.Fail(w, http.StatusBadRequest, "transmission_id 与 user_id 必填")
			return
		}
		ok := false
		if sc.CrossDesk != nil {
			if req.TransmissionID == req.UserID {
				ok = sc.CrossDesk.Kick(req.UserID)
			} else {
				ok = sc.CrossDesk.DisconnectGuest(req.TransmissionID, req.UserID)
			}
		}
		if !ok {
			httpx.Fail(w, http.StatusNotFound, "连接不存在")
			return
		}
		auditOp(sc, r, audit.ActionCrossDeskDisconnect, "crossdesk_session", req.TransmissionID, req.UserID, nil)
		httpx.OK(w, nil)
	}
}

// CrossDeskNativeStats 原生信令统计（管理员）。
func CrossDeskNativeStats(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sc.CrossDesk == nil {
			httpx.OK(w, map[string]any{"connections": 0, "transmissions": 0, "devices": 0, "online_devices": 0})
			return
		}
		httpx.OK(w, sc.CrossDesk.Stats())
	}
}
