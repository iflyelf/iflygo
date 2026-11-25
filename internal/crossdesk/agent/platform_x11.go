//go:build linux && !android

package agent

import (
	"fmt"
	"image"
	"os"
	"sync"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/randr"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgb/xtest"
)

// x11Screen 通过 X11 协议直接抓取根窗口像素，纯 Go、无需 CGO。
type x11Screen struct {
	conn     *xgb.Conn
	root     xproto.Window
	bpp      int
	depth    byte
	lastW    int
	lastH    int
	cacheImg *image.RGBA

	displayMu sync.Mutex
	displays  []DisplayInfo
}

func newScreen() (Screen, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("无法连接 X11（DISPLAY=%q）: %w；Wayland 桌面需使用 X11 会话或 XWayland", xenv(), err)
	}
	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	bpp := 0
	for _, f := range setup.PixmapFormats {
		if f.Depth == screen.RootDepth {
			bpp = int(f.BitsPerPixel)
			break
		}
	}
	if bpp == 0 {
		bpp = 32
	}
	return &x11Screen{conn: conn, root: screen.Root, bpp: bpp, depth: screen.RootDepth}, nil
}

func xenv() string {
	return os.Getenv("DISPLAY")
}

func (s *x11Screen) Capture() (image.Image, error) {
	geom, err := xproto.GetGeometry(s.conn, xproto.Drawable(s.root)).Reply()
	if err != nil {
		return nil, err
	}
	return s.captureArea(0, 0, int(geom.Width), int(geom.Height))
}

// Displays 通过 RandR 枚举独立显示器；失败时退化为整个根窗口。
func (s *x11Screen) Displays() ([]DisplayInfo, error) {
	list := s.queryDisplays()
	s.displayMu.Lock()
	s.displays = list
	s.displayMu.Unlock()
	return append([]DisplayInfo(nil), list...), nil
}

func (s *x11Screen) queryDisplays() []DisplayInfo {
	geom, err := xproto.GetGeometry(s.conn, xproto.Drawable(s.root)).Reply()
	if err != nil {
		return nil
	}
	fallback := []DisplayInfo{{
		ID: 0, Name: "显示器 1", Width: int(geom.Width), Height: int(geom.Height), Primary: true,
	}}
	if err := randr.Init(s.conn); err != nil {
		return fallback
	}
	res, err := randr.GetScreenResources(s.conn, s.root).Reply()
	if err != nil {
		return fallback
	}
	var out []DisplayInfo
	for _, crtc := range res.Crtcs {
		info, err := randr.GetCrtcInfo(s.conn, crtc, res.ConfigTimestamp).Reply()
		if err != nil || info.Width == 0 || info.Height == 0 {
			continue
		}
		name := ""
		if len(info.Outputs) > 0 {
			if oi, err := randr.GetOutputInfo(s.conn, info.Outputs[0], res.ConfigTimestamp).Reply(); err == nil {
				name = string(oi.Name)
			}
		}
		id := len(out)
		if name == "" {
			name = fmt.Sprintf("显示器 %d", id+1)
		}
		out = append(out, DisplayInfo{
			ID: id, Name: name, X: int(info.X), Y: int(info.Y),
			Width: int(info.Width), Height: int(info.Height),
		})
	}
	if len(out) == 0 {
		return fallback
	}
	for i := range out {
		d := out[i]
		if d.X <= 0 && d.Y <= 0 && d.X+d.Width > 0 && d.Y+d.Height > 0 {
			out[i].Primary = true
			break
		}
	}
	return out
}

func (s *x11Screen) CaptureDisplay(id int) (image.Image, error) {
	s.displayMu.Lock()
	displays := append([]DisplayInfo(nil), s.displays...)
	s.displayMu.Unlock()
	if len(displays) == 0 {
		var err error
		if displays, err = s.Displays(); err != nil {
			return nil, err
		}
	}
	for _, d := range displays {
		if d.ID != id {
			continue
		}
		img, err := s.captureArea(d.X, d.Y, d.Width, d.Height)
		if err != nil {
			return nil, err
		}
		return img, nil
	}
	return nil, fmt.Errorf("未知显示器: %d", id)
}

func (s *x11Screen) captureArea(x, y, w, h int) (image.Image, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("X11 采集区域无效: %dx%d", w, h)
	}
	reply, err := xproto.GetImage(s.conn, xproto.ImageFormatZPixmap, xproto.Drawable(s.root),
		int16(x), int16(y), uint16(w), uint16(h), ^uint32(0)).Reply()
	if err != nil {
		return nil, err
	}
	stride := (w*s.bpp + 31) / 32 * 4
	if len(reply.Data) < stride*h {
		return nil, fmt.Errorf("X11 图像数据不完整: %d < %d", len(reply.Data), stride*h)
	}
	if s.cacheImg == nil || s.lastW != w || s.lastH != h {
		s.cacheImg = image.NewRGBA(image.Rect(0, 0, w, h))
		s.lastW, s.lastH = w, h
	}
	dst := s.cacheImg.Pix
	switch s.bpp {
	case 32:
		for yy := 0; yy < h; yy++ {
			src := reply.Data[yy*stride:]
			row := dst[yy*s.cacheImg.Stride:]
			for xx := 0; xx < w; xx++ {
				row[xx*4+0] = src[xx*4+2]
				row[xx*4+1] = src[xx*4+1]
				row[xx*4+2] = src[xx*4+0]
				row[xx*4+3] = 0xff
			}
		}
	case 24:
		for yy := 0; yy < h; yy++ {
			src := reply.Data[yy*stride:]
			row := dst[yy*s.cacheImg.Stride:]
			for xx := 0; xx < w; xx++ {
				row[xx*4+0] = src[xx*3+2]
				row[xx*4+1] = src[xx*3+1]
				row[xx*4+2] = src[xx*3+0]
				row[xx*4+3] = 0xff
			}
		}
	case 16:
		for yy := 0; yy < h; yy++ {
			src := reply.Data[yy*stride:]
			row := dst[yy*s.cacheImg.Stride:]
			for xx := 0; xx < w; xx++ {
				v := uint16(src[xx*2]) | uint16(src[xx*2+1])<<8
				r := uint8((v >> 11) & 0x1f)
				g := uint8((v >> 5) & 0x3f)
				b := uint8(v & 0x1f)
				row[xx*4+0] = r<<3 | r>>2
				row[xx*4+1] = g<<2 | g>>4
				row[xx*4+2] = b<<3 | b>>2
				row[xx*4+3] = 0xff
			}
		}
	default:
		return nil, fmt.Errorf("不支持的 X11 像素深度: %d bpp", s.bpp)
	}
	return s.cacheImg, nil
}

func (s *x11Screen) Close() error {
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	return nil
}

// x11Input 通过 XTEST 扩展注入键鼠事件。
type x11Input struct {
	conn   *xgb.Conn
	root   xproto.Window
	width  int
	height int
	// 当前采集区域（根窗口绝对坐标）；为零时使用整个根窗口。
	regionX, regionY, regionW, regionH int
	keymap                             map[uint32]xproto.Keycode
}

// SetDisplay 输入坐标映射到当前显示器区域。
func (i *x11Input) SetDisplay(x, y, w, h int) {
	i.regionX, i.regionY, i.regionW, i.regionH = x, y, w, h
}

func newInput() (Input, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("无法连接 X11: %w", err)
	}
	if err := xtest.Init(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("X11 缺少 XTEST 扩展: %w", err)
	}
	screen := xproto.Setup(conn).DefaultScreen(conn)
	in := &x11Input{conn: conn, root: screen.Root, keymap: map[uint32]xproto.Keycode{}}
	if err := in.refreshGeometry(); err != nil {
		conn.Close()
		return nil, err
	}
	return in, nil
}

func (i *x11Input) refreshGeometry() error {
	geom, err := xproto.GetGeometry(i.conn, xproto.Drawable(i.root)).Reply()
	if err != nil {
		return err
	}
	i.width, i.height = int(geom.Width), int(geom.Height)
	if i.regionW <= 0 || i.regionH <= 0 {
		i.regionX, i.regionY, i.regionW, i.regionH = 0, 0, i.width, i.height
	}
	return nil
}

func (i *x11Input) Mouse(x, y float64, flag, scroll int) error {
	if err := i.refreshGeometry(); err != nil {
		return err
	}
	px := int16(i.regionX) + int16(clamp01(x)*float64(i.regionW-1))
	py := int16(i.regionY) + int16(clamp01(y)*float64(i.regionH-1))
	switch flag {
	case MouseMove:
		return xtest.FakeInputChecked(i.conn, xproto.MotionNotify, 0, 0, i.root, px, py, 0).Check()
	case MouseLeftDown:
		return i.button(1, true, px, py)
	case MouseLeftUp:
		return i.button(1, false, px, py)
	case MouseRightDown:
		return i.button(3, true, px, py)
	case MouseRightUp:
		return i.button(3, false, px, py)
	case MouseMiddleDn:
		return i.button(2, true, px, py)
	case MouseMiddleUp:
		return i.button(2, false, px, py)
	case MouseWheelV:
		btn := byte(4)
		units := wheelSteps(scroll)
		if units < 0 {
			btn = 5
			units = -units
		}
		for n := 0; n < units; n++ {
			if err := i.button(btn, true, px, py); err != nil {
				return err
			}
			if err := i.button(btn, false, px, py); err != nil {
				return err
			}
		}
		return nil
	case MouseWheelH:
		btn := byte(7) // 向右
		units := wheelSteps(scroll)
		if units < 0 {
			btn = 6
			units = -units
		}
		for n := 0; n < units; n++ {
			if err := i.button(btn, true, px, py); err != nil {
				return err
			}
			if err := i.button(btn, false, px, py); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

func (i *x11Input) button(btn byte, down bool, x, y int16) error {
	typ := byte(xproto.ButtonPress)
	if !down {
		typ = xproto.ButtonRelease
	}
	return xtest.FakeInputChecked(i.conn, typ, btn, 0, i.root, x, y, 0).Check()
}

func (i *x11Input) Key(vk int, down bool) error {
	code, err := i.keycode(vk)
	if err != nil {
		return err
	}
	typ := byte(xproto.KeyPress)
	if !down {
		typ = xproto.KeyRelease
	}
	return xtest.FakeInputChecked(i.conn, typ, byte(code), 0, i.root, 0, 0, 0).Check()
}

func (i *x11Input) keycode(vk int) (xproto.Keycode, error) {
	keysym := vkToKeysym(vk)
	if keysym == 0 {
		return 0, fmt.Errorf("不支持的按键: %d", vk)
	}
	if code, ok := i.keymap[keysym]; ok {
		return code, nil
	}
	setup := xproto.Setup(i.conn)
	count := int(setup.MaxKeycode - setup.MinKeycode + 1)
	reply, err := xproto.GetKeyboardMapping(i.conn, setup.MinKeycode, byte(count)).Reply()
	if err != nil {
		return 0, err
	}
	per := int(reply.KeysymsPerKeycode)
	if per <= 0 {
		return 0, fmt.Errorf("X11 键盘映射为空")
	}
	for idx := 0; idx < count; idx++ {
		for k := 0; k < per; k++ {
			pos := idx*per + k
			if pos >= len(reply.Keysyms) {
				break
			}
			ks := uint32(reply.Keysyms[pos])
			if ks == 0 {
				continue
			}
			if _, exists := i.keymap[ks]; !exists {
				i.keymap[ks] = xproto.Keycode(int(setup.MinKeycode) + idx)
			}
		}
	}
	if code, ok := i.keymap[keysym]; ok {
		return code, nil
	}
	return 0, fmt.Errorf("X11 键盘布局中找不到按键 keysym=0x%x", keysym)
}

func (i *x11Input) Close() error {
	if i.conn != nil {
		i.conn.Close()
		i.conn = nil
	}
	return nil
}
