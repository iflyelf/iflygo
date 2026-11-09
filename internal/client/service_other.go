//go:build !windows

package client

import "fmt"

// installWindowsService 非 Windows 平台占位：正常路径不会调用。
func installWindowsService(exe, stateDir, relay, token, configPath string) error {
	return fmt.Errorf("当前平台不支持 Windows 服务安装")
}

// uninstallWindowsService 非 Windows 平台占位：正常路径不会调用。
func uninstallWindowsService() error {
	return fmt.Errorf("当前平台不支持 Windows 服务卸载")
}
