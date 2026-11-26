package agent

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// commandScreen 通过外部命令抓屏：命令需向 stdout 输出 PNG/JPEG。
// 主要用于 Wayland（如 grim）或平台原生采集不可用的环境。
type commandScreen struct {
	command string
	mu      sync.Mutex
}

func newCommandScreen(command string) (Screen, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("--capture-cmd 为空")
	}
	return &commandScreen{command: command}, nil
}

func (s *commandScreen) Capture() (image.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shell, args := audioShell(s.command)
	out, err := exec.CommandContext(ctx, shell, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("抓屏命令失败: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("抓屏命令没有输出")
	}
	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("解码抓屏输出失败（需要 PNG/JPEG）: %w", err)
	}
	return img, nil
}

func (s *commandScreen) Close() error { return nil }
