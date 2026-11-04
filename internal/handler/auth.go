package handler

import (
	"net/http"

	"github.com/iflyelf/iflygo/internal/middleware"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/svc"
)

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthConfig 返回认证配置（前端据此决定是否显示登录页）。
func AuthConfig(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.OK(w, map[string]any{"provider": "builtin", "requires_login": true})
	}
}

// Login 登录。
func Login(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		u, err := sc.Auth.Verify(req.Username, req.Password)
		if err != nil {
			httpx.Fail(w, http.StatusUnauthorized, err.Error())
			return
		}
		sc.Auth.Issue(w, r, u)
		httpx.OK(w, u)
	}
}

// Logout 登出。
func Logout(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc.Auth.Clear(w)
		httpx.OK(w, nil)
	}
}

// UserInfo 当前用户信息。
func UserInfo(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.OK(w, middleware.CurrentUser(r))
	}
}
