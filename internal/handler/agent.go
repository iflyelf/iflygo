package handler

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/svc"
)

type registerReq struct {
	EnrollToken string `json:"enroll_token"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Kernel      string `json:"kernel"`
	Version     string `json:"version"`
}

// AgentRegister 用一次性 enroll token 注册，返回 client_id + secret + bundle。
func AgentRegister(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		if req.EnrollToken == "" {
			httpx.Fail(w, http.StatusUnauthorized, "缺少 enroll_token")
			return
		}

		tokenHash := sha256hex(req.EnrollToken)
		q := sc.Store.Rebind(`SELECT id FROM client_nodes WHERE enroll_token_hash = $1`)
		var id int64
		if err := sc.Store.DB.QueryRow(q, tokenHash).Scan(&id); err != nil {
			httpx.Fail(w, http.StatusUnauthorized, "enroll_token 无效或已使用")
			return
		}

		secret, err := randomToken()
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		now := nowStr()
		_, err = sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE client_nodes SET enroll_token_hash = '', secret_hash = $1, os = $2, arch = $3, kernel = $4, version = $5, status = 'online', last_seen = $6, updated_at = $7 WHERE id = $8`),
			sha256hex(secret), req.OS, req.Arch, req.Kernel, req.Version, now, now, id)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		row, err := getClientRow(sc, id)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		bundle, err := buildBundle(sc, row)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		httpx.OK(w, map[string]any{"client_id": id, "secret": secret, "bundle": bundle})
	}
}

// AgentHeartbeat 心跳，返回配置版本。
func AgentHeartbeat(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		row, err := agentClient(sc, r)
		if err != nil {
			httpx.Fail(w, http.StatusUnauthorized, "未授权")
			return
		}
		now := nowStr()
		_, err = sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE client_nodes SET status = 'online', last_seen = $1, updated_at = $2 WHERE id = $3`), now, now, row.ID)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, map[string]any{"config_rev": row.ConfigRev, "server_url": ""})
	}
}

// AgentConfig 拉取配置包。
func AgentConfig(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		row, err := agentClient(sc, r)
		if err != nil {
			httpx.Fail(w, http.StatusUnauthorized, "未授权")
			return
		}
		bundle, err := buildBundle(sc, row)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, bundle)
	}
}

// agentClient 从 Bearer <id>:<secret> 解析客户端。
func agentClient(sc *svc.ServiceContext, r *http.Request) (*clientRow, error) {
	h := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	idStr, secret, ok := strings.Cut(h, ":")
	if !ok {
		return nil, errUnauthorized
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, errUnauthorized
	}
	row, err := getClientRow(sc, id)
	if err != nil {
		return nil, errUnauthorized
	}
	if subtle.ConstantTimeCompare([]byte(row.SecretHash), []byte(sha256hex(secret))) != 1 {
		return nil, errUnauthorized
	}
	return row, nil
}

var errUnauthorized = &authError{}

type authError struct{}

func (authError) Error() string { return "未授权" }
