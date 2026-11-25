//go:build windows

package agent

import (
	"fmt"
	"image"
	"sort"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modGdi32    = windows.NewLazySystemDLL("gdi32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSetProcessDPIAware  = modUser32.NewProc("SetProcessDPIAware")
	procGetDC               = modUser32.NewProc("GetDC")
	procReleaseDC           = modUser32.NewProc("ReleaseDC")
	procGetSystemMetrics    = modUser32.NewProc("GetSystemMetrics")
	procSetCursorPos        = modUser32.NewProc("SetCursorPos")
	procMouseEvent          = modUser32.NewProc("mouse_event")
	procKeybdEvent          = modUser32.NewProc("keybd_event")
	procEnumDisplayMonitors = modUser32.NewProc("EnumDisplayMonitors")

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

type winRect struct {
	Left, Top, Right, Bottom int32
}

var (
	monitorMu       sync.Mutex
	monitorRects    []winRect
	monitorCallback = windows.NewCallback(func(_, _, lprc, _ uintptr) uintptr {
		monitorRects = append(monitorRects, *(*winRect)(unsafe.Pointer(lprc)))
		return 1
	})
)

func enumMonitors() []winRect {
	monitorMu.Lock()
	defer monitorMu.Unlock()
	monitorRects = monitorRects[:0]
	procEnumDisplayMonitors.Call(0, 0, monitorCallback, 0)
	out := append([]winRect(nil), monitorRects...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Left == out[j].Left {
			return out[i].Top < out[j].Top
		}
		return out[i].Left < out[j].Left
	})
	return out
}

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
	return s.captureRectLocked(s.x, s.y, s.w, s.h)
}

func (s *windowsScreen) Displays() ([]DisplayInfo, error) {
	rects := enumMonitors()
	if len(rects) == 0 {
		if err := s.refresh(); err != nil {
			return nil, err
		}
		return []DisplayInfo{{ID: 0, Name: "显示器 1", X: s.x, Y: s.y, Width: s.w, Height: s.h, Primary: true}}, nil
	}
	out := make([]DisplayInfo, 0, len(rects))
	for _, r := range rects {
		w, h := int(r.Right-r.Left), int(r.Bottom-r.Top)
		if w <= 0 || h <= 0 {
			continue
		}
		out = append(out, DisplayInfo{
			ID: len(out), Name: fmt.Sprintf("显示器 %d", len(out)+1),
			X: int(r.Left), Y: int(r.Top), Width: w, Height: h,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("未检测到显示器")
	}
	for i := range out {
		d := out[i]
		if d.X <= 0 && d.Y <= 0 && d.X+d.Width > 0 && d.Y+d.Height > 0 {
			out[i].Primary = true
			break
		}
	}
	return out, nil
}

func (s *windowsScreen) CaptureDisplay(id int) (image.Image, error) {
	displays, err := s.Displays()
	if err != nil {
		return nil, err
	}
	for _, d := range displays {
		if d.ID == id {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.captureRectLocked(d.X, d.Y, d.Width, d.Height)
		}
	}
	return nil, fmt.Errorf("未知显示器: %d", id)
}

func (s *windowsScreen) captureRectLocked(x, y, w, h int) (image.Image, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("Windows 采集区域无效: %dx%d", w, h)
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

	bitmap, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	if bitmap == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap 失败")
	}
	defer procDeleteObject.Call(bitmap)
	old, _, _ := procSelectObject.Call(memDC, bitmap)
	defer procSelectObject.Call(memDC, old)

	if r, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), hdc, uintptr(x), uintptr(y), srcCopy); r == 0 {
		return nil, fmt.Errorf("BitBlt 失败")
	}

	need := w * h * 4
	if len(s.buf) != need {
		s.buf = make([]byte, need)
		s.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	bi := bitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(h),
		Planes: 1, BitCount: 32, Compression: 0,
	}
	r, _, _ := procGetDIBits.Call(memDC, bitmap, 0, uintptr(h),
		uintptr(unsafe.Pointer(&s.buf[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors)
	if r == 0 {
		return nil, fmt.Errorf("GetDIBits 失败")
	}
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

type windowsInput struct {
	x, y, w, h int
}

func newInput() (Input, error) { return &windowsInput{}, nil }

// SetDisplay 输入坐标映射到当前显示器区域。
func (i *windowsInput) SetDisplay(x, y, w, h int) { i.x, i.y, i.w, i.h = x, y, w, h }

func (i *windowsInput) Mouse(x, y float64, flag, scroll int) error {
	vx := systemMetric(smXVirtualScreen)
	vy := systemMetric(smYVirtualScreen)
	vw := systemMetric(smCXVirtualScreen)
	vh := systemMetric(smCYVirtualScreen)
	if vw <= 0 || vh <= 0 {
		return fmt.Errorf("无法获取 Windows 虚拟桌面尺寸")
	}
	regionX, regionY, regionW, regionH := i.x, i.y, i.w, i.h
	if regionW <= 0 || regionH <= 0 {
		regionX, regionY, regionW, regionH = int(vx), int(vy), int(vw), int(vh)
	}
	px := int32(regionX) + int32(clamp01(x)*float64(regionW-1))
	py := int32(regionY) + int32(clamp01(y)*float64(regionH-1))
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
