//go:build darwin

package agent

import (
	"fmt"
	"image"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// macOS 通过 CoreGraphics 直接采集与注入，使用 purego 动态调用，
// 保持 CGO_ENABLED=0 的静态交叉编译能力。

type cgPoint struct{ X, Y float64 }
type cgSize struct{ Width, Height float64 }
type cgRect struct {
	Origin cgPoint
	Size   cgSize
}

var (
	cgOnce sync.Once
	cgErr  error

	cgMainDisplayID               func() uint32
	cgGetActiveDisplayList        func(max uint32, displays *uint32, count *uint32) int32
	cgDisplayCreateImage          func(display uint32) uintptr
	cgDisplayBounds               func(display uint32) cgRect
	cgDisplayPixelsWide           func(display uint32) uint64
	cgDisplayPixelsHigh           func(display uint32) uint64
	cgImageGetWidth               func(img uintptr) uint64
	cgImageGetHeight              func(img uintptr) uint64
	cgImageGetBytesPerRow         func(img uintptr) uint64
	cgImageGetDataProvider        func(img uintptr) uintptr
	cgImageRelease                func(img uintptr)
	cgDataProviderCopyData        func(provider uintptr) uintptr
	cfDataGetBytePtr              func(data uintptr) *byte
	cfDataGetLength               func(data uintptr) int64
	cfRelease                     func(ref uintptr)
	cgEventCreateMouseEvent       func(source uintptr, typ uint32, point cgPoint, button uint32) uintptr
	cgEventCreateKeyboardEvent    func(source uintptr, keycode uint16, keyDown uint8) uintptr
	cgEventCreateScrollWheelEvent func(source uintptr, units uint32, count uint32, wheel1, wheel2 int32) uintptr
	cgEventPost                   func(tap uint32, event uintptr)
)

func loadCoreGraphics() error {
	cgOnce.Do(func() {
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
			purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			cgErr = fmt.Errorf("加载 CoreGraphics 失败: %w", err)
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			cgErr = fmt.Errorf("加载 CoreFoundation 失败: %w", err)
			return
		}
		for _, item := range []struct {
			fn   any
			lib  uintptr
			name string
		}{
			{&cgMainDisplayID, cg, "CGMainDisplayID"},
			{&cgGetActiveDisplayList, cg, "CGGetActiveDisplayList"},
			{&cgGetActiveDisplayList, cg, "CGGetActiveDisplayList"},
			{&cgDisplayCreateImage, cg, "CGDisplayCreateImage"},
			{&cgDisplayBounds, cg, "CGDisplayBounds"},
			{&cgDisplayPixelsWide, cg, "CGDisplayPixelsWide"},
			{&cgDisplayPixelsHigh, cg, "CGDisplayPixelsHigh"},
			{&cgDisplayPixelsWide, cg, "CGDisplayPixelsWide"},
			{&cgDisplayPixelsHigh, cg, "CGDisplayPixelsHigh"},
			{&cgImageGetWidth, cg, "CGImageGetWidth"},
			{&cgImageGetHeight, cg, "CGImageGetHeight"},
			{&cgImageGetBytesPerRow, cg, "CGImageGetBytesPerRow"},
			{&cgImageGetDataProvider, cg, "CGImageGetDataProvider"},
			{&cgImageRelease, cg, "CGImageRelease"},
			{&cgDataProviderCopyData, cg, "CGDataProviderCopyData"},
			{&cfDataGetBytePtr, cf, "CFDataGetBytePtr"},
			{&cfDataGetLength, cf, "CFDataGetLength"},
			{&cfRelease, cf, "CFRelease"},
			{&cgEventCreateMouseEvent, cg, "CGEventCreateMouseEvent"},
			{&cgEventCreateKeyboardEvent, cg, "CGEventCreateKeyboardEvent"},
			{&cgEventCreateScrollWheelEvent, cg, "CGEventCreateScrollWheelEvent"},
			{&cgEventPost, cg, "CGEventPost"},
		} {
			purego.RegisterLibFunc(item.fn, item.lib, item.name)
		}
	})
	return cgErr
}

const (
	cgEventLeftMouseDown  = 1
	cgEventLeftMouseUp    = 2
	cgEventRightMouseDown = 3
	cgEventRightMouseUp   = 4
	cgEventMouseMoved     = 5
	cgEventOtherMouseDown = 25
	cgEventOtherMouseUp   = 26
	cgHIDEventTap         = 0
	cgScrollUnitLine      = 1
)

type darwinScreen struct {
	mu  sync.Mutex
	img *image.RGBA
}

func newScreen() (Screen, error) {
	if err := loadCoreGraphics(); err != nil {
		return nil, err
	}
	return &darwinScreen{}, nil
}

func (s *darwinScreen) Capture() (image.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.captureDisplayLocked(cgMainDisplayID())
}

func (s *darwinScreen) Displays() ([]DisplayInfo, error) {
	var ids [16]uint32
	var count uint32
	if ret := cgGetActiveDisplayList(uint32(len(ids)), &ids[0], &count); ret != 0 {
		return nil, fmt.Errorf("CGGetActiveDisplayList 失败: %d", ret)
	}
	main := cgMainDisplayID()
	out := make([]DisplayInfo, 0, count)
	for i := 0; i < int(count); i++ {
		id := ids[i]
		bounds := cgDisplayBounds(id)
		out = append(out, DisplayInfo{
			ID: len(out), Name: fmt.Sprintf("显示器 %d", len(out)+1),
			X: int(bounds.Origin.X), Y: int(bounds.Origin.Y),
			Width: int(bounds.Size.Width), Height: int(bounds.Size.Height), Primary: id == main,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("未检测到显示器")
	}
	return out, nil
}

func (s *darwinScreen) CaptureDisplay(id int) (image.Image, error) {
	var ids [16]uint32
	var count uint32
	if ret := cgGetActiveDisplayList(uint32(len(ids)), &ids[0], &count); ret != 0 {
		return nil, fmt.Errorf("CGGetActiveDisplayList 失败: %d", ret)
	}
	if id < 0 || id >= int(count) {
		return nil, fmt.Errorf("未知显示器: %d", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.captureDisplayLocked(ids[id])
}

func (s *darwinScreen) captureDisplayLocked(display uint32) (image.Image, error) {
	ref := cgDisplayCreateImage(display)
	if ref == 0 {
		return nil, fmt.Errorf("CGDisplayCreateImage 失败；请在“系统设置 → 隐私与安全性 → 屏幕录制”中授权")
	}
	defer cgImageRelease(ref)

	w := int(cgImageGetWidth(ref))
	h := int(cgImageGetHeight(ref))
	row := int(cgImageGetBytesPerRow(ref))
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("macOS 屏幕尺寸无效")
	}
	provider := cgImageGetDataProvider(ref)
	data := cgDataProviderCopyData(provider)
	if data == 0 {
		return nil, fmt.Errorf("CGDataProviderCopyData 失败")
	}
	defer cfRelease(data)
	length := int(cfDataGetLength(data))
	ptr := cfDataGetBytePtr(data)
	if ptr == nil || length < row*h || row < w*4 {
		return nil, fmt.Errorf("macOS 屏幕像素格式不支持（row=%d len=%d）", row, length)
	}
	src := unsafe.Slice(ptr, length)
	if s.img == nil || s.img.Rect.Dx() != w || s.img.Rect.Dy() != h {
		s.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	dst := s.img.Pix
	for y := 0; y < h; y++ {
		in := src[y*row:]
		out := dst[y*s.img.Stride:]
		for x := 0; x < w; x++ {
			out[x*4+0] = in[x*4+2]
			out[x*4+1] = in[x*4+1]
			out[x*4+2] = in[x*4+0]
			out[x*4+3] = 0xff
		}
	}
	return s.img, nil
}

func (s *darwinScreen) Close() error { return nil }

type darwinInput struct {
	x, y, w, h float64
}

func newInput() (Input, error) {
	if err := loadCoreGraphics(); err != nil {
		return nil, err
	}
	return &darwinInput{}, nil
}

// SetDisplay 输入坐标映射到当前显示器区域（CoreGraphics 点坐标）。
func (i *darwinInput) SetDisplay(x, y, w, h int) {
	i.x, i.y, i.w, i.h = float64(x), float64(y), float64(w), float64(h)
}

func (i *darwinInput) Mouse(x, y float64, flag, scroll int) error {
	regionX, regionY, regionW, regionH := i.x, i.y, i.w, i.h
	if regionW <= 0 || regionH <= 0 {
		bounds := cgDisplayBounds(cgMainDisplayID())
		regionX, regionY, regionW, regionH = bounds.Origin.X, bounds.Origin.Y, bounds.Size.Width, bounds.Size.Height
	}
	px := regionX + clamp01(x)*regionW
	py := regionY + clamp01(y)*regionH
	pt := cgPoint{X: px, Y: py}

	switch flag {
	case MouseMove:
		i.post(cgEventCreateMouseEvent(0, cgEventMouseMoved, pt, 0))
	case MouseLeftDown:
		i.post(cgEventCreateMouseEvent(0, cgEventLeftMouseDown, pt, 0))
	case MouseLeftUp:
		i.post(cgEventCreateMouseEvent(0, cgEventLeftMouseUp, pt, 0))
	case MouseRightDown:
		i.post(cgEventCreateMouseEvent(0, cgEventRightMouseDown, pt, 1))
	case MouseRightUp:
		i.post(cgEventCreateMouseEvent(0, cgEventRightMouseUp, pt, 1))
	case MouseMiddleDn:
		i.post(cgEventCreateMouseEvent(0, cgEventOtherMouseDown, pt, 2))
	case MouseMiddleUp:
		i.post(cgEventCreateMouseEvent(0, cgEventOtherMouseUp, pt, 2))
	case MouseWheelV:
		if units := wheelSteps(scroll); units != 0 {
			i.post(cgEventCreateScrollWheelEvent(0, cgScrollUnitLine, 1, int32(units), 0))
		}
	case MouseWheelH:
		if units := wheelSteps(scroll); units != 0 {
			i.post(cgEventCreateScrollWheelEvent(0, cgScrollUnitLine, 2, 0, int32(units)))
		}
	}
	return nil
}

func (i *darwinInput) Key(vk int, down bool) error {
	code := vkToMacKeycode(vk)
	if code < 0 {
		return fmt.Errorf("不支持的按键: %d", vk)
	}
	value := uint8(0)
	if down {
		value = 1
	}
	i.post(cgEventCreateKeyboardEvent(0, uint16(code), value))
	return nil
}

func (i *darwinInput) post(event uintptr) {
	if event == 0 {
		return
	}
	cgEventPost(cgHIDEventTap, event)
	cfRelease(event)
}

func (i *darwinInput) Close() error { return nil }
