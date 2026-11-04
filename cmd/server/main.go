// iflygo-server：控制面 + 数据面角色入口。
//
// 单个二进制通过 -role 启用不同角色：
//
//	console      Web/API、数据库、IPAM、PKI、Token、配置生成、域名
//	lighthouse   UDP 发现、NAT 打洞协调
//	relay        UDP 中继
//	edge         443 入口、Host/SNI 路由、ACME
//	gateway      不安全路由网关（转发/NAT/子网探测）
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/iflyelf/iflygo/internal/version"
)

func main() {
	role := flag.String("role", "console", "服务端角色(逗号分隔): console,lighthouse,relay,edge,gateway")
	listen := flag.String("listen", ":8080", "控制面监听地址")
	showVersion := flag.Bool("version", false, "显示版本并退出")
	flag.Parse()

	if *showVersion {
		fmt.Printf("iflygo-server %s (build %s, commit %s)\n", version.Version, version.Build, version.Commit)
		return
	}

	roles := splitRoles(*role)
	log.Printf("🚀 iflygo-server %s 启动, roles=%v", version.Version, roles)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","version":%q,"roles":%q}`+"\n", version.Version, strings.Join(roles, ","))
	})

	log.Printf("🔗 监听 %s (角色骨架，业务逻辑开发中)", *listen)
	if err := http.ListenAndServe(*listen, mux); err != nil {
		log.Fatalf("❌ 服务启动失败: %v", err)
	}
}

func splitRoles(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
