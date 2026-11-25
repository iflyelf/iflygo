package agent

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"

	"golang.org/x/image/draw"
)

// Screen 是平台屏幕采集实现。Capture 返回当前整个虚拟桌面。
type Screen interface {
	Capture() (image.Image, error)
	Close() error
}

// DisplayInfo 描述一个可单独采集的显示器。
type DisplayInfo struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Primary bool   `json:"primary"`
}

// DisplayProvider 由支持多显示器的平台实现。
type DisplayProvider interface {
	Displays() ([]DisplayInfo, error)
	CaptureDisplay(id int) (image.Image, error)
}

// DisplayInput 由输入实现可选支持：把归一化坐标映射到指定显示器区域。
type DisplayInput interface {
	SetDisplay(x, y, w, h int)
}

// Input 是平台输入注入实现。x/y 为 0..1 的归一化坐标。
type Input interface {
	// Mouse flag 与 Web 客户端 MouseFlag 一致：
	// 0 move、1 左键按下、2 左键抬起、3 右键按下、4 右键抬起、
	// 5 中键按下、6 中键抬起、7 垂直滚轮、8 水平滚轮。
	Mouse(x, y float64, flag, scroll int) error
	// Key 接收浏览器 KeyboardEvent.keyCode（Windows 虚拟键码）。
	Key(vk int, down bool) error
	Close() error
}

// 鼠标动作常量，与 web/crossdesk/control.js 保持一致。
const (
	MouseMove      = 0
	MouseLeftDown  = 1
	MouseLeftUp    = 2
	MouseRightDown = 3
	MouseRightUp   = 4
	MouseMiddleDn  = 5
	MouseMiddleUp  = 6
	MouseWheelV    = 7
	MouseWheelH    = 8
)

// encodeJPEG 按需缩小画面并编码为 JPEG。
func encodeJPEG(img image.Image, maxWidth, quality int) ([]byte, int, int, error) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, 0, 0, fmt.Errorf("屏幕尺寸无效: %dx%d", w, h)
	}
	if maxWidth > 0 && w > maxWidth {
		nw := maxWidth
		nh := h * maxWidth / w
		if nh < 1 {
			nh = 1
		}
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Src, nil)
		img, w, h = dst, nw, nh
	}
	if quality < 1 {
		quality = 1
	}
	if quality > 100 {
		quality = 100
	}
	var buf bytes.Buffer
	buf.Grow(w * h / 8)
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), w, h, nil
}

// wheelSteps 将浏览器滚轮增量换算为离散滚轮步数。
// 触控板/鼠标通常给出约 100 像素/格，虚拟滚轮按钮直接给出 ±1。
func wheelSteps(scroll int) int {
	if scroll == 0 {
		return 0
	}
	step := scroll
	if scroll >= 20 || scroll <= -20 {
		step = scroll / 100
		if step == 0 {
			if scroll > 0 {
				step = 1
			} else {
				step = -1
			}
		}
	}
	if step > 10 {
		step = 10
	}
	if step < -10 {
		step = -10
	}
	return step
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
