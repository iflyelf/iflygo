package handler

import (
	"net/http"

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/svc"
)

// Health 存活探针。
func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.OK(w, map[string]any{"status": "ok"})
	}
}

// Ready 就绪探针（校验数据库）。
func Ready(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := sc.Store.DB.PingContext(r.Context()); err != nil {
			httpx.Fail(w, http.StatusServiceUnavailable, "数据库不可用")
			return
		}
		httpx.OK(w, map[string]any{"status": "ready"})
	}
}
