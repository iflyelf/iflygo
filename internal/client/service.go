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
	return fmt.Sprintf(`[Unit]
Description=iFlyGo Client Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run --state-dir %s
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, exe, stateDir)
}

// InstallService 安装为系统服务。
func InstallService(exe, stateDir string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("暂不支持 %s 服务安装（Linux systemd 已支持，Windows 后续版本）", runtime.GOOS)
	}
	unit := SystemdUnit(exe, stateDir)
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
