//go:build windows

package agent

import (
	"fmt"
	"image"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modGdi32    = windows.NewLazySystemDLL("gdi32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSetProcessDPIAware = modUser32.NewProc("SetProcessDPIAware")
	procGetDC              = modUser32.NewProc("GetDC")
	procReleaseDC          = modUser32.NewProc("ReleaseDC")
	procGetSystemMetrics   = modUser32.NewProc("GetSystemMetrics")
	procSetCursorPos       = modUser32.NewProc("SetCursorPos")
	procMouseEvent         = modUser32.NewProc("mouse_event")
	procKeybdEvent         = modUser32.NewProc("keybd_event")

	procCreateCompatibleDC     = modGdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = modGdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = modGdi32.NewProc("SelectObject")
	procBitBlt                 = modGdi32.NewProc("BitBlt")
	procGetDIBits              = modGdi32.NewProc("GetDIBits")
	procDeleteObject           = modGdi32.NewProc("DeleteObject")
	procDeleteDC               = modGdi32.NewProc("DeleteDC")
)

const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
	srcCopy           = 0x00CC0020
	dibRGBColors      = 0

	mouseEventLeftDown   = 0x0002
	mouseEventLeftUp     = 0x0004
	mouseEventRightDown  = 0x0008
	mouseEventRightUp    = 0x0010
	mouseEventMiddleDown = 0x0020
	mouseEventMiddleUp   = 0x0040
	mouseEventWheel      = 0x0800
	mouseEventHWheel     = 0x01000

	keyEventKeyUp = 0x0002
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

var dpiOnce sync.Once

type windowsScreen struct {
	mu  sync.Mutex
	buf []byte
	img *image.RGBA
	w   int
	h   int
	x   int
	y   int
}

func systemMetric(index int) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int32(r)
}

func newScreen() (Screen, error) {
	dpiOnce.Do(func() { procSetProcessDPIAware.Call() })
	s := &windowsScreen{}
	if err := s.refresh(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *windowsScreen) refresh() error {
	s.x = int(systemMetric(smXVirtualScreen))
	s.y = int(systemMetric(smYVirtualScreen))
	w := int(systemMetric(smCXVirtualScreen))
	h := int(systemMetric(smCYVirtualScreen))
	if w <= 0 || h <= 0 {
		return fmt.Errorf("无法获取 Windows 虚拟桌面尺寸")
	}
	s.w, s.h = w, h
	return nil
}

func (s *windowsScreen) Capture() (image.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refresh(); err != nil {
		return nil, err
	}
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("GetDC 失败")
	}
	defer procReleaseDC.Call(0, hdc)

	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC 失败")
	}
	defer procDeleteDC.Call(memDC)

	bitmap, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(s.w), uintptr(s.h))
	if bitmap == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap 失败")
	}
	defer procDeleteObject.Call(bitmap)
	old, _, _ := procSelectObject.Call(memDC, bitmap)
	defer procSelectObject.Call(memDC, old)

	if r, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(s.w), uintptr(s.h), hdc, uintptr(s.x), uintptr(s.y), srcCopy); r == 0 {
		return nil, fmt.Errorf("BitBlt 失败")
	}

	need := s.w * s.h * 4
	if len(s.buf) != need {
		s.buf = make([]byte, need)
		s.img = image.NewRGBA(image.Rect(0, 0, s.w, s.h))
	}
	bi := bitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(s.w), Height: -int32(s.h),
		Planes: 1, BitCount: 32, Compression: 0,
	}
	r, _, _ := procGetDIBits.Call(memDC, bitmap, 0, uintptr(s.h),
		uintptr(unsafe.Pointer(&s.buf[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors)
	if r == 0 {
		return nil, fmt.Errorf("GetDIBits 失败")
	}
	// GetDIBits 输出 BGRA，转换为 RGBA。
	dst := s.img.Pix
	for i := 0; i < need; i += 4 {
		dst[i] = s.buf[i+2]
		dst[i+1] = s.buf[i+1]
		dst[i+2] = s.buf[i]
		dst[i+3] = 0xff
	}
	return s.img, nil
}

func (s *windowsScreen) Close() error { return nil }

type windowsInput struct{}

func newInput() (Input, error) { return &windowsInput{}, nil }

func (i *windowsInput) Mouse(x, y float64, flag, scroll int) error {
	vx := systemMetric(smXVirtualScreen)
	vy := systemMetric(smYVirtualScreen)
	vw := systemMetric(smCXVirtualScreen)
	vh := systemMetric(smCYVirtualScreen)
	if vw <= 0 || vh <= 0 {
		return fmt.Errorf("无法获取 Windows 虚拟桌面尺寸")
	}
	px := vx + int32(clamp01(x)*float64(vw-1))
	py := vy + int32(clamp01(y)*float64(vh-1))
	procSetCursorPos.Call(uintptr(px), uintptr(py))

	switch flag {
	case MouseMove:
		return nil
	case MouseLeftDown:
		procMouseEvent.Call(mouseEventLeftDown, 0, 0, 0, 0)
	case MouseLeftUp:
		procMouseEvent.Call(mouseEventLeftUp, 0, 0, 0, 0)
	case MouseRightDown:
		procMouseEvent.Call(mouseEventRightDown, 0, 0, 0, 0)
	case MouseRightUp:
		procMouseEvent.Call(mouseEventRightUp, 0, 0, 0, 0)
	case MouseMiddleDn:
		procMouseEvent.Call(mouseEventMiddleDown, 0, 0, 0, 0)
	case MouseMiddleUp:
		procMouseEvent.Call(mouseEventMiddleUp, 0, 0, 0, 0)
	case MouseWheelV:
		if units := wheelSteps(scroll); units != 0 {
			procMouseEvent.Call(mouseEventWheel, 0, uintptr(uint32(int32(units)*120)), 0, 0)
		}
	case MouseWheelH:
		if units := wheelSteps(scroll); units != 0 {
			procMouseEvent.Call(mouseEventHWheel, 0, uintptr(uint32(int32(units)*120)), 0, 0)
		}
	}
	return nil
}

func (i *windowsInput) Key(vk int, down bool) error {
	flags := uintptr(0)
	if !down {
		flags = keyEventKeyUp
	}
	procKeybdEvent.Call(uintptr(vk), 0, flags, 0)
	return nil
}

func (i *windowsInput) Close() error { return nil }
