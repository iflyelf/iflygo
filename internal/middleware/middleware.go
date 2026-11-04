// Package middleware 提供 HTTP 中间件。
package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/auth"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
)

type userKey struct{}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// AccessLog 记录访问日志。
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if r.URL.Path != "/health" {
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

// SecurityHeaders 设置安全响应头。
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// Recover 捕获 panic，返回 500。
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("❌ panic: %v", rec)
				httpx.Fail(w, http.StatusInternalServerError, "内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// RequireAuth 要求已登录。
func RequireAuth(a *auth.Auth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.User(r)
		if err != nil || u == nil {
			httpx.Fail(w, http.StatusUnauthorized, "未登录")
			return
		}
		ctx := context.WithValue(r.Context(), userKey{}, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CurrentUser 从上下文取当前用户。
func CurrentUser(r *http.Request) *auth.User {
	if u, ok := r.Context().Value(userKey{}).(*auth.User); ok {
		return u
	}
	return nil
}
