package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const serviceName = "iflygo-client"

// SystemdUnit 生成 systemd 单元内容。
func SystemdUnit(exe, stateDir string) string {
	return SystemdUnitWithRelay(exe, stateDir, "", "")
}

// SystemdUnitWithRelay 生成带 TCP 中继启动参数的 systemd 单元内容。
// relay/token 为空时不附加对应参数。
func SystemdUnitWithRelay(exe, stateDir, relay, token string) string {
	return SystemdUnitWithOptions(exe, stateDir, relay, token, "")
}

// SystemdUnitWithOptions 生成完整启动参数的 systemd 单元内容。
func SystemdUnitWithOptions(exe, stateDir, relay, token, configPath string) string {
	extra := ""
	if configPath != "" {
		extra += " --config " + configPath
	}
	if relay != "" {
		extra += " --tcp-relay " + relay
	}
	if token != "" {
		extra += " --tcp-relay-token " + token
	}
	return fmt.Sprintf(`[Unit]
Description=iFlyGo Client Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run --state-dir %s%s
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, exe, stateDir, extra)
}

// InstallService 安装为系统服务；relay/token/config 非空时写入 unit 的启动参数。
func InstallService(exe, stateDir, relay, token, configPath string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("暂不支持 %s 服务安装（Linux systemd 已支持，Windows 后续版本）", runtime.GOOS)
	}
	if strings.ContainsAny(relay+token+configPath, " \t\r\n\"'\\") {
		return fmt.Errorf("服务启动参数不能包含空白或引号")
	}
	unit := SystemdUnitWithOptions(exe, stateDir, relay, token, configPath)
	path := filepath.Join("/etc/systemd/system", serviceName+".service")
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("写入 unit 失败（需要 root）: %w", err)
	}
	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", serviceName},
		{"start", serviceName},
	} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s 失败: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// UninstallService 卸载系统服务。
func UninstallService() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("暂不支持 %s 服务卸载", runtime.GOOS)
	}
	_ = exec.Command("systemctl", "stop", serviceName).Run()
	_ = exec.Command("systemctl", "disable", serviceName).Run()
	path := filepath.Join("/etc/systemd/system", serviceName+".service")
	_ = os.Remove(path)
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return nil
}
