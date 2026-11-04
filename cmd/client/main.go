// iflygo-client：客户端 Agent。
//
// 子命令：
//
//	enroll  --server URL --token TOKEN [--name NAME]    一次性 token 注册
//	run     [--state-dir DIR] [--interval 30s]          运行（心跳 + 配置热更新）
//	config  [--state-dir DIR]                           拉取并应用配置包
//	status  [--state-dir DIR]                           查看本地状态
//	service install|uninstall [--state-dir DIR]         安装/卸载系统服务
//	version                                             显示版本
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/client"
	"github.com/iflyelf/iflygo/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "enroll":
		cmdEnroll(os.Args[2:])
	case "run":
		cmdRun(os.Args[2:])
	case "config":
		cmdConfig(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "service":
		cmdService(os.Args[2:])
	case "version", "-version", "--version":
		fmt.Printf("iflygo-client %s (build %s, commit %s)\n", version.Version, version.Build, version.Commit)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("用法: iflygo-client <enroll|run|config|status|service|version> [选项]")
}

func stateDir(fs *flag.FlagSet) string {
	return fs.Lookup("state-dir").Value.String()
}

func cmdEnroll(args []string) {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	server := fs.String("server", "", "控制面地址，如 https://console.example.com")
	token := fs.String("token", "", "一次性 enroll token")
	name := fs.String("name", "", "节点名称")
	dir := fs.String("state-dir", client.DefaultStateDir(), "状态目录")
	fs.Parse(args)

	if *server == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "enroll 需要 --server 与 --token")
		os.Exit(2)
	}

	info := client.RegInfo{
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Kernel:  detectKernel(),
		Version: version.Version,
	}
	id, secret, bundle, err := client.Register(context.Background(), strings.TrimRight(*server, "/"), *token, info)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 注册失败: %v\n", err)
		os.Exit(1)
	}

	if *name == "" {
		*name = fmt.Sprintf("client-%d", id)
	}
	st := &client.State{
		Server:    strings.TrimRight(*server, "/"),
		ClientID:  id,
		Secret:    secret,
		Name:      *name,
		Network:   bundle.Network,
		IPv4:      bundle.IPv4,
		IPv6:      bundle.IPv6,
		ConfigRev: bundle.ConfigRev,
	}
	if err := st.Save(*dir); err != nil {
		fmt.Fprintf(os.Stderr, "❌ 保存状态失败: %v\n", err)
		os.Exit(1)
	}
	if err := client.ApplyBundle(*dir, bundle); err != nil {
		fmt.Fprintf(os.Stderr, "❌ 应用配置失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ 注册成功: client_id=%d name=%s ipv4=%s ipv6=%s\n", id, *name, bundle.IPv4, bundle.IPv6)
	fmt.Printf("状态目录: %s\n", *dir)
	fmt.Println("下一步: iflygo-client run --state-dir " + *dir)
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	dir := fs.String("state-dir", client.DefaultStateDir(), "状态目录")
	interval := fs.Duration("interval", 30*time.Second, "心跳间隔")
	fs.Parse(args)

	if err := client.Run(*dir, *interval); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}

func cmdConfig(args []string) {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	dir := fs.String("state-dir", client.DefaultStateDir(), "状态目录")
	fs.Parse(args)

	if err := client.FetchAndApply(*dir); err != nil {
		fmt.Fprintf(os.Stderr, "❌ 拉取配置失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 配置已更新到 %s\n", *dir)
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	dir := fs.String("state-dir", client.DefaultStateDir(), "状态目录")
	fs.Parse(args)

	st, err := client.LoadState(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	fmt.Println(string(b))
}

func cmdService(args []string) {
	if len(args) == 0 {
		fmt.Println("用法: iflygo-client service install|uninstall [--state-dir DIR]")
		os.Exit(2)
	}
	action := args[0]
	fs := flag.NewFlagSet("service", flag.ExitOnError)
	dir := fs.String("state-dir", client.DefaultStateDir(), "状态目录")
	fs.Parse(args[1:])

	switch action {
	case "install":
		exe, _ := os.Executable()
		exe, _ = filepath.Abs(exe)
		if err := client.InstallService(exe, *dir); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ 服务已安装并启动")
	case "uninstall":
		if err := client.UninstallService(); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ 服务已卸载")
	default:
		fmt.Fprintf(os.Stderr, "未知操作: %s\n", action)
		os.Exit(2)
	}
}

func detectKernel() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
