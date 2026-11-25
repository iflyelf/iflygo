//go:build linux && !android

package agent

import (
	"os"
	"testing"
)

func TestX11Displays(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("未设置 DISPLAY，跳过 X11 显示器测试")
	}
	screen, err := newScreen()
	if err != nil {
		t.Skipf("X11 不可用: %v", err)
	}
	defer screen.Close()
	provider, ok := screen.(DisplayProvider)
	if !ok {
		t.Fatal("x11Screen 未实现 DisplayProvider")
	}
	displays, err := provider.Displays()
	if err != nil {
		t.Fatalf("枚举显示器失败: %v", err)
	}
	if len(displays) == 0 {
		t.Fatal("未返回显示器")
	}
	var primary int
	for _, d := range displays {
		if d.Width <= 0 || d.Height <= 0 {
			t.Fatalf("显示器尺寸无效: %+v", d)
		}
		if d.Primary {
			primary = d.ID
		}
	}
	img, err := provider.CaptureDisplay(primary)
	if err != nil {
		t.Fatalf("采集显示器失败: %v", err)
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		t.Fatalf("采集图像尺寸无效: %v", img.Bounds())
	}
	if _, err := provider.CaptureDisplay(9999); err == nil {
		t.Fatal("未知显示器应返回错误")
	}
}
