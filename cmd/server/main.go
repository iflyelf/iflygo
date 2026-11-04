// iflygo-server：控制面 + 数据面角色入口。
//
// 角色（后续逐步实现）：console / lighthouse / relay / edge / gateway。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iflyelf/iflygo/internal/config"
	"github.com/iflyelf/iflygo/internal/middleware"
	"github.com/iflyelf/iflygo/internal/svc"
	"github.com/iflyelf/iflygo/internal/version"
)

func main() {
	configFile := flag.String("c", "", "配置文件路径")
	role := flag.String("role", "console", "服务端角色(逗号分隔): console,lighthouse,relay,edge,gateway")
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
