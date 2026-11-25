//go:build darwin

package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

// darwinClipboard 使用系统自带的 pbpaste / pbcopy，保持 CGO_ENABLED=0。
type darwinClipboard struct{}

func newClipboard() (Clipboard, error) {
	if _, err := exec.LookPath("pbpaste"); err != nil {
		return nil, fmt.Errorf("找不到 pbpaste: %w", err)
	}
	return &darwinClipboard{}, nil
}

func (c *darwinClipboard) ReadText() (string, error) {
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (c *darwinClipboard) WriteText(text string) error {
	if len(text) > clipTextLimit {
		return fmt.Errorf("剪贴板文本超过 %d 字节", clipTextLimit)
	}
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func (c *darwinClipboard) Close() error { return nil }
