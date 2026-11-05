package main

import (
	"net/http"

	"github.com/iflyelf/iflygo/internal/handler"
	"github.com/iflyelf/iflygo/internal/middleware"
	"github.com/iflyelf/iflygo/internal/svc"
)

// registerRoutes 注册 HTTP 路由。
func registerRoutes(mux *http.ServeMux, sc *svc.ServiceContext) {
	a := sc.Auth
	auth := func(h http.HandlerFunc) http.Handler {
		return middleware.RequireAuth(a, h)
	}

	// 健康检查
	mux.HandleFunc("GET /health", handler.Health())
	mux.HandleFunc("GET /ready", handler.Ready(sc))

	// 认证
	mux.HandleFunc("GET /api/auth/config", handler.AuthConfig(sc))
	mux.HandleFunc("POST /api/auth/login", handler.Login(sc))
	mux.Handle("POST /api/auth/logout", auth(handler.Logout(sc)))
	mux.Handle("GET /api/auth/userinfo", auth(handler.UserInfo(sc)))

	// 网络
	mux.Handle("GET /api/networks", auth(handler.ListNetworks(sc)))
	mux.Handle("POST /api/networks", auth(handler.CreateNetwork(sc)))
	mux.Handle("DELETE /api/networks/{id}", auth(handler.DeleteNetwork(sc)))

	// 服务端节点（lighthouse/relay/edge/gateway）
	mux.Handle("GET /api/server-nodes", auth(handler.ListServerNodes(sc)))
	mux.Handle("POST /api/server-nodes", auth(handler.CreateServerNode(sc)))
	mux.Handle("DELETE /api/server-nodes/{id}", auth(handler.DeleteServerNode(sc)))
	mux.Handle("GET /api/server-nodes/{id}/bundle", auth(handler.ServerNodeBundle(sc)))

	// 不安全路由（子网路由 + 网关）
	mux.Handle("GET /api/networks/{id}/routes", auth(handler.ListRoutes(sc)))
	mux.Handle("POST /api/networks/{id}/routes", auth(handler.CreateRoute(sc)))
	mux.Handle("DELETE /api/routes/{id}", auth(handler.DeleteRoute(sc)))

	// 客户端
	mux.Handle("GET /api/clients", auth(handler.ListClients(sc)))
	mux.Handle("POST /api/clients", auth(handler.CreateClient(sc)))
	mux.Handle("GET /api/clients/{id}", auth(handler.GetClient(sc)))
	mux.Handle("DELETE /api/clients/{id}", auth(handler.DeleteClient(sc)))
	mux.Handle("POST /api/clients/{id}/enroll-token", auth(handler.ClientEnrollToken(sc)))
	mux.Handle("GET /api/clients/{id}/bundle", auth(handler.ClientBundle(sc)))

	// Agent（注册匿名，心跳/配置需 Bearer）
	mux.HandleFunc("POST /api/agent/register", handler.AgentRegister(sc))
	mux.HandleFunc("POST /api/agent/heartbeat", handler.AgentHeartbeat(sc))
	mux.HandleFunc("GET /api/agent/config", handler.AgentConfig(sc))

	// 前端 SPA
	mux.Handle("/", spaHandler())
}
