// Package svc 组装服务上下文（配置、数据库、认证、IPAM）。
package svc

import (
	"time"

	"github.com/iflyelf/iflygo/internal/auth"
	"github.com/iflyelf/iflygo/internal/config"
	"github.com/iflyelf/iflygo/internal/ipam"
	"github.com/iflyelf/iflygo/internal/store"
)

// ServiceContext 服务依赖集合。
type ServiceContext struct {
	Cfg   *config.Config
	Store *store.Store
	Auth  *auth.Auth
	IPAM  *ipam.Allocator
}

// New 构建服务上下文。
func New(cfg *config.Config) (*ServiceContext, error) {
	st, err := store.Open(cfg)
	if err != nil {
		return nil, err
	}

	ttl, _ := time.ParseDuration(cfg.Auth.SessionTTL)
	a := auth.New(st, cfg.Auth.SessionSecret, ttl, cfg.Auth.CookieSecure)
	if err := a.EnsureAdmin(cfg.Auth.AdminUsername, cfg.Auth.AdminPassword); err != nil {
		return nil, err
	}

	return &ServiceContext{
		Cfg:   cfg,
		Store: st,
		Auth:  a,
		IPAM:  ipam.New(st),
	}, nil
}
