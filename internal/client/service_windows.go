//go:build windows

package client

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// installWindowsService 注册并启动 Windows 服务（SCM）。
func installWindowsService(exe, stateDir, relay, token, configPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("连接 Windows 服务管理器失败: %w", err)
	}
	defer m.Disconnect()

	args := []string{"run", "--state-dir", stateDir}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	if relay != "" {
		args = append(args, "--tcp-relay", relay)
	}
	if token != "" {
		args = append(args, "--tcp-relay-token", token)
	}

	// 已存在：先停止并删除，确保启动参数（配置/中继）是最新的。
	if s, err := m.OpenService(serviceName); err == nil {
		_, _ = s.Control(svc.Stop)
		_ = s.Delete()
		_ = s.Close()
		if err := waitServiceDeleted(m, serviceName, 5*time.Second); err != nil {
			return fmt.Errorf("旧服务仍在删除中，请稍后重试: %w", err)
		}
	}

	s, err := m.CreateService(serviceName, exe, mgr.Config{
		DisplayName: "iFlyGo Client Agent",
		Description: "iFlyGo P2P tunnel client agent",
		StartType:   mgr.StartAutomatic,
	}, args...)
	if err != nil {
		return fmt.Errorf("创建 Windows 服务失败: %w", err)
	}
	defer s.Close()
	if err := s.Start(); err != nil {
		return fmt.Errorf("启动 Windows 服务失败: %w", err)
	}
	return nil
}

// uninstallWindowsService 停止并删除 Windows 服务。
func uninstallWindowsService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("连接 Windows 服务管理器失败: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return nil // 未安装
	}
	defer s.Close()
	_, _ = s.Control(svc.Stop)
	if err := s.Delete(); err != nil {
		return fmt.Errorf("删除 Windows 服务失败: %w", err)
	}
	return nil
}

func waitServiceDeleted(m *mgr.Mgr, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s, err := m.OpenService(name)
		if err != nil {
			return nil
		}
		_ = s.Close()
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("等待服务删除超时")
}
