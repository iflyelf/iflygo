//go:build linux && !android

package agent

import (
	"fmt"
	"image"
	"os"

	"github.com/BurntSushi/xgb"
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
	w, h := int(geom.Width), int(geom.Height)
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("X11 根窗口尺寸为空")
	}
	reply, err := xproto.GetImage(s.conn, xproto.ImageFormatZPixmap, xproto.Drawable(s.root),
		0, 0, geom.Width, geom.Height, ^uint32(0)).Reply()
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
		for y := 0; y < h; y++ {
			src := reply.Data[y*stride:]
			row := dst[y*s.cacheImg.Stride:]
			for x := 0; x < w; x++ {
				row[x*4+0] = src[x*4+2]
				row[x*4+1] = src[x*4+1]
				row[x*4+2] = src[x*4+0]
				row[x*4+3] = 0xff
			}
		}
	case 24:
		for y := 0; y < h; y++ {
			src := reply.Data[y*stride:]
			row := dst[y*s.cacheImg.Stride:]
			for x := 0; x < w; x++ {
				row[x*4+0] = src[x*3+2]
				row[x*4+1] = src[x*3+1]
				row[x*4+2] = src[x*3+0]
				row[x*4+3] = 0xff
			}
		}
	case 16:
		for y := 0; y < h; y++ {
			src := reply.Data[y*stride:]
			row := dst[y*s.cacheImg.Stride:]
			for x := 0; x < w; x++ {
				v := uint16(src[x*2]) | uint16(src[x*2+1])<<8
				r := uint8((v >> 11) & 0x1f)
				g := uint8((v >> 5) & 0x3f)
				b := uint8(v & 0x1f)
				row[x*4+0] = r<<3 | r>>2
				row[x*4+1] = g<<2 | g>>4
				row[x*4+2] = b<<3 | b>>2
				row[x*4+3] = 0xff
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
	keymap map[uint32]xproto.Keycode
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
	return nil
}

func (i *x11Input) Mouse(x, y float64, flag, scroll int) error {
	if err := i.refreshGeometry(); err != nil {
		return err
	}
	px := int16(clamp01(x) * float64(i.width-1))
	py := int16(clamp01(y) * float64(i.height-1))
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
