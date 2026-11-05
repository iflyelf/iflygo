package main

import (
	"log"
	"net/http"
	"time"

	"github.com/iflyelf/iflygo/internal/edge"
)

// startEdge 启动 edge 反向代理，并周期重载路由表文件。
func startEdge(addr, routesPath, cert, key string) error {
	routes, err := edge.LoadRoutesFile(routesPath)
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
			r, err := edge.LoadRoutesFile(routesPath)
			if err != nil {
				continue
			}
			table.Set(r)
			log.Printf("🌐 edge 路由表已重载: %d 条", len(r))
		}
	}()

	srv := &http.Server{
		Addr:              addr,
		Handler:           proxy,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		var err error
		if cert != "" && key != "" {
			log.Printf("🌐 edge 代理监听 %s (TLS)", addr)
			err = srv.ListenAndServeTLS(cert, key)
		} else {
			log.Printf("🌐 edge 代理监听 %s (HTTP)", addr)
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Printf("❌ edge 代理退出: %v", err)
		}
	}()
	return nil
}
