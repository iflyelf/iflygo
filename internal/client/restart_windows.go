//go:build windows

package client

// RestartForUpdate Windows 上由服务恢复/计划任务拉起新进程。
func RestartForUpdate(string) error { return ErrRestartRequired }
