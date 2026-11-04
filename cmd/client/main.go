// iflygo-client：客户端 Agent 入口。
//
// 后续将包含：
//   - enroll：一次性 token 注册、本地密钥对、拉取配置
//   - run：启动 overlay（嵌入 Nebula）+ L7 隧道 + 服务守护
//   - service：systemd / Windows 服务安装
//   - self-update：二进制自升级（含回滚）
package main

import (
	"flag"
	"fmt"

	"github.com/iflyelf/iflygo/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "显示版本并退出")
	flag.Parse()

	if *showVersion {
		fmt.Printf("iflygo-client %s (build %s, commit %s)\n", version.Version, version.Build, version.Commit)
		return
	}

	fmt.Printf("iflygo-client %s (enroll/run 待实现)\n", version.Version)
}
