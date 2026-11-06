package liaison

import (
	"database/sql"
	"net/http"

	"github.com/iflyelf/iflygo/internal/auth"
	"github.com/iflyelf/iflygo/internal/config"
)

// Service Liaison 远程管理套件服务。
type Service struct {
	db       *sql.DB
	rbac     *RBAC
	dbReg    *dbRegistry
	settings func() config.Config
}

// New 构造 Liaison 服务。settings 可为 nil（未注入时 LLM 网关不可用）。
func New(db *sql.DB, rbac *RBAC, settings func() config.Config) *Service {
	return &Service{db: db, rbac: rbac, dbReg: newDBRegistry(db), settings: settings}
}

// RBAC 返回 Casbin 鉴权器（供 svc 或中间件复用）。
func (s *Service) RBAC() *RBAC { return s.rbac }

// Authorize 判断 subject 对 obj 执行 act。
func (s *Service) Authorize(sub, obj, act string) bool {
	if s.rbac == nil {
		return false
	}
	return s.rbac.Allow(sub, obj, act)
}

// subjectOf 从请求上下文解析当前用户。
func subjectOf(r *http.Request) (string, *auth.User) {
	u, ok := auth.FromContext(r.Context())
	if !ok || u == nil {
		return "", nil
	}
	return u.Username, u
}

// authorizeUser 按用户角色做 Casbin 授权（任一角色命中即放行）。
func (s *Service) authorizeUser(u *auth.User, obj, act string) bool {
	if u == nil || s.rbac == nil {
		return false
	}
	return s.rbac.AllowRoles(u.Roles, obj, act)
}

// RegisterRoutes 注册 Liaison 路由。
// reqAuth / reqSuperAdmin 为鉴权包装（由 main 传入，避免 import 中间件造成 import cycle）。
func RegisterRoutes(mux *http.ServeMux, s *Service, reqAuth, reqSuperAdmin func(http.HandlerFunc) http.Handler) {
	// WebSocket 通道（SSH/RDP/VNC 终端）
	mux.Handle("GET /api/liaison/ws", reqAuth(s.handleWS))

	// 文件管理（REST）
	mux.Handle("GET /api/liaison/files", reqAuth(s.handleFilesList))
	mux.Handle("GET /api/liaison/files/content", reqAuth(s.handleFileContent))
	mux.Handle("PUT /api/liaison/files/content", reqAuth(s.handleFileWrite))
	mux.Handle("POST /api/liaison/files/upload", reqAuth(s.handleFileUpload))
	mux.Handle("DELETE /api/liaison/files", reqAuth(s.handleFileDelete))

	// Web DB（REST）
	mux.Handle("GET /api/liaison/db/connections", reqAuth(s.handleDBList))
	mux.Handle("POST /api/liaison/db/connections", reqAuth(s.handleDBCreate))
	mux.Handle("DELETE /api/liaison/db/connections/{id}", reqAuth(s.handleDBDelete))
	mux.Handle("POST /api/liaison/db/query", reqAuth(s.handleDBQuery))

	// LLM 网关（OpenAI 兼容代理）
	mux.Handle("GET /api/liaison/llm/models", reqAuth(s.handleLLMModels))
	mux.Handle("POST /api/liaison/llm/chat", reqAuth(s.handleLLMChat))

	// Casbin 策略管理（仅 superadmin）
	mux.Handle("GET /api/liaison/policies", reqSuperAdmin(s.handlePoliciesList))
	mux.Handle("POST /api/liaison/policies", reqSuperAdmin(s.handlePolicyAdd))
	mux.Handle("DELETE /api/liaison/policies", reqSuperAdmin(s.handlePolicyRemove))
}
