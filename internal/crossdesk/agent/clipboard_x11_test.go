//go:build linux && !android

package agent

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestX11ClipboardRoundTrip(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("未设置 DISPLAY，跳过 X11 剪贴板测试")
	}
	clipboard, err := newClipboard()
	if err != nil {
		t.Skipf("X11 剪贴板不可用: %v", err)
	}
	defer clipboard.Close()

	text := "iFlyGo 剪贴板 ✓ line\ntwo"
	if err := clipboard.WriteText(text); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	got, err := clipboard.ReadText()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got != text {
		t.Fatalf("读取内容不一致: got %q want %q", got, text)
	}
	if err := clipboard.WriteText("second"); err != nil {
		t.Fatalf("二次写入失败: %v", err)
	}
	if got, err = clipboard.ReadText(); err != nil || got != "second" {
		t.Fatalf("二次读取失败: got %q err=%v", got, err)
	}
	if _, err := exec.LookPath("xclip"); err != nil {
		t.Skip("未安装 xclip，跳过跨应用测试")
	}
	// 外部应用写入后，iFlyGo 能读取。
	cmd := exec.Command("xclip", "-selection", "clipboard", "-in")
	cmd.Stdin = strings.NewReader("external-text")
	if err := cmd.Run(); err != nil {
		t.Fatalf("xclip 写入失败: %v", err)
	}
	if got, err = clipboard.ReadText(); err != nil || got != "external-text" {
		t.Fatalf("读取外部剪贴板失败: got %q err=%v", got, err)
	}
	// iFlyGo 写入后，外部应用能读取。
	if err := clipboard.WriteText("from-iflygo"); err != nil {
		t.Fatalf("写入外部剪贴板失败: %v", err)
	}
	out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	if err != nil {
		t.Fatalf("xclip 读取失败: %v", err)
	}
	if string(out) != "from-iflygo" {
		t.Fatalf("xclip 读取内容不一致: %q", string(out))
	}
}
