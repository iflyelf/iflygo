package middleware

import (
	"log"
	"net/http"

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
)

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
