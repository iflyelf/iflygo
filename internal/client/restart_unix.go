//go:build !windows

package client

import (
	"os"
	"syscall"
)

// RestartForUpdate Unix 上用新二进制原地替换当前进程（PID 不变）。
func RestartForUpdate(binPath string) error {
	args := append([]string{binPath}, os.Args[1:]...)
	return syscall.Exec(binPath, args, os.Environ())
}
