// Package httpx 统一 JSON 响应与请求解析。
package httpx

import (
	"encoding/json"
	"log"
	"net/http"
)

type envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// WriteJSON 输出 JSON。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// OK 成功响应：HTTP 200 + code 0。
func OK(w http.ResponseWriter, data any) {
	WriteJSON(w, http.StatusOK, envelope{Code: 0, Message: "ok", Data: data})
}

// Fail 失败响应。
func Fail(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, envelope{Code: status, Message: message})
}

// FailErr 失败响应（错误脱敏）。
func FailErr(w http.ResponseWriter, status int, err error) {
	if err == nil {
		Fail(w, status, "内部错误")
		return
	}
	log.Printf("❌ [%d] %v", status, err)
	Fail(w, status, err.Error())
}

// DecodeJSON 解析 JSON 请求体。
func DecodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}
