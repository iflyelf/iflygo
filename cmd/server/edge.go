package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/iflyelf/iflygo/internal/edge"
	"golang.org/x/crypto/acme/autocert"
)

// edgeConfig edge 角色配置。
type edgeConfig struct {
	addr      string // HTTPS 监听地址
	httpAddr  string // HTTP（ACME 挑战 + 跳转）监听地址
	routes    string // 路由表 JSON 文件
	cert      string // 静态 TLS 证书
	key       string // 静态 TLS 私钥
	acmeEmail string // 启用 ACME 自动证书
	acmeCache string // 证书缓存目录
}

// startEdge 启动 edge 反向代理，并周期重载路由表文件。
func startEdge(cfg edgeConfig) error {
	routes, err := edge.LoadRoutesFile(cfg.routes)
	if err != nil {
		return err
	}
	table := edge.NewTable(routes)
	proxy := edge.NewProxy(table)

	// 周期重载路由表（域名/目标变更后无需重启）
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			r, err := edge.LoadRoutesFile(cfg.routes)
			if err != nil {
				continue
			}
			table.Set(r)
			log.Printf("🌐 edge 路由表已重载: %d 条", len(r))
		}
	}()

	if cfg.acmeEmail != "" {
		return startEdgeACME(cfg, table, proxy)
	}

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           proxy,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		var err error
		if cfg.cert != "" && cfg.key != "" {
			log.Printf("🌐 edge 代理监听 %s (TLS)", cfg.addr)
			err = srv.ListenAndServeTLS(cfg.cert, cfg.key)
		} else {
			log.Printf("🌐 edge 代理监听 %s (HTTP)", cfg.addr)
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Printf("❌ edge 代理退出: %v", err)
		}
	}()
	return nil
}

// startEdgeACME 使用 Let's Encrypt（autocert）自动签发证书并服务 HTTPS。
func startEdgeACME(cfg edgeConfig, table *edge.Table, proxy http.Handler) error {
	cacheDir := cfg.acmeCache
	if cacheDir == "" {
		cacheDir = "data/edge-certs"
	}
	mgr := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Email:      cfg.acmeEmail,
		Cache:      autocert.DirCache(cacheDir),
		HostPolicy: hostPolicy(table),
	}

	// HTTP 服务：ACME HTTP-01 挑战 + 其余跳转 HTTPS
	httpAddr := cfg.httpAddr
	if httpAddr == "" {
		httpAddr = ":80"
	}
	challenge := mgr.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := "https://" + r.Host + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	}))
	go func() {
		log.Printf("🌐 edge ACME HTTP 监听 %s", httpAddr)
		if err := http.ListenAndServe(httpAddr, challenge); err != nil {
			log.Printf("❌ edge ACME HTTP 退出: %v", err)
		}
	}()

	tlsCfg := &tls.Config{
		GetCertificate: mgr.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	}
	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           proxy,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		log.Printf("🌐 edge 代理监听 %s (ACME 自动 HTTPS)", cfg.addr)
		if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Printf("❌ edge 代理退出: %v", err)
		}
	}()
	return nil
}

// hostPolicy 仅允许路由表内的域名申请证书。
func hostPolicy(table *edge.Table) autocert.HostPolicy {
	return func(_ context.Context, host string) error {
		if _, ok := table.Lookup(host); ok {
			return nil
		}
		return fmt.Errorf("域名未配置路由: %s", host)
	}
}
