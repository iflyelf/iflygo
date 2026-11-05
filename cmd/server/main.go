// iflygo-server：控制面 + 数据面角色入口。
//
// 角色（后续逐步实现）：console / lighthouse / relay / edge / gateway。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/iflyelf/iflygo/internal/config"
	"github.com/iflyelf/iflygo/internal/middleware"
	"github.com/iflyelf/iflygo/internal/overlay"
	"github.com/iflyelf/iflygo/internal/svc"
	"github.com/iflyelf/iflygo/internal/version"
)

func main() {
	configFile := flag.String("c", "", "配置文件路径")
	role := flag.String("role", "console", "服务端角色(逗号分隔): console,lighthouse,relay,edge,gateway")
	overlayConfig := flag.String("overlay-config", "", "Nebula 配置文件（lighthouse/relay/edge 角色用）")
	edgeAddr := flag.String("edge-addr", "", "edge 代理监听地址（如 :443）")
	edgeRoutes := flag.String("edge-routes", "", "edge 路由表 JSON 文件路径")
	edgeTLSCert := flag.String("edge-tls-cert", "", "edge TLS 证书（可选）")
	edgeTLSKey := flag.String("edge-tls-key", "", "edge TLS 私钥（可选）")
	edgeAcmeEmail := flag.String("edge-acme-email", "", "启用 Let's Encrypt 自动证书（填邮箱）")
	edgeAcmeCache := flag.String("edge-acme-cache", "", "ACME 证书缓存目录")
	edgeHTTPAddr := flag.String("edge-http-addr", "", "edge ACME HTTP 监听地址（默认 :80）")
	showVersion := flag.Bool("version", false, "显示版本并退出")
	flag.Parse()

	if *showVersion {
		fmt.Printf("iflygo-server %s (build %s, commit %s)\n", version.Version, version.Build, version.Commit)
		return
	}

	cfg, err := config.Load(*configFile)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if err := cfg.ValidateRequired(); err != nil {
		log.Fatalf("配置校验失败: %v", err)
	}

	sc, err := svc.New(cfg)
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}
	defer sc.Store.Close()

	mux := http.NewServeMux()
	registerRoutes(mux, sc)

	var root http.Handler = mux
	root = middleware.Recover(root)
	root = middleware.SecurityHeaders(root)
	root = middleware.AccessLog(root)

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           root,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("🚀 iflygo-server %s 监听 %s (role=%s)", version.Version, cfg.Addr(), *role)

	// lighthouse/relay/edge 数据面角色：读取预生成 Nebula 配置并启动 overlay
	var ov *overlay.Overlay
	if *overlayConfig != "" {
		b, err := os.ReadFile(*overlayConfig)
		if err != nil {
			log.Fatalf("读取 overlay 配置失败: %v", err)
		}
		ov = overlay.New(slog.Default())
		if err := ov.Start(b); err != nil {
			log.Fatalf("启动 overlay 失败: %v", err)
		}
		defer ov.Stop()
		log.Printf("🔗 overlay 数据面已启动 (roles=%s)", *role)
	}

	// edge 角色：按 Host 反向代理到 overlay 目标
	if *edgeAddr != "" && strings.Contains(*role, "edge") {
		if err := startEdge(edgeConfig{
			addr: *edgeAddr, httpAddr: *edgeHTTPAddr, routes: *edgeRoutes,
			cert: *edgeTLSCert, key: *edgeTLSKey,
			acmeEmail: *edgeAcmeEmail, acmeCache: *edgeAcmeCache,
		}); err != nil {
			log.Fatalf("启动 edge 失败: %v", err)
		}
	}

	go func() {
		if cfg.Server.TLSCert != "" && cfg.Server.TLSKey != "" {
			if err := srv.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey); err != nil && err != http.ErrServerClosed {
				log.Fatalf("服务启动失败: %v", err)
			}
			return
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("正在关闭...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
