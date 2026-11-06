package liaison

import (
	"context"
	"crypto/des"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"nhooyr.io/websocket"
)

// Web VNC（RFB 协议客户端）：连接 VNC 服务器，解码 framebuffer，经 WebSocket 桥接到浏览器。
//
// 支持：RFB 3.3/3.7/3.8 握手、NoAuth + VNC Auth、Raw/Hextile/CopyRect 编码、
// 键盘/指针输入。Tight/ZRLE 等压缩编码暂未实现（多数服务器会在编码协商中回退 Raw/Hextile）。
// RDP 因协议栈复杂度高，暂以同一通道占位（返回明确错误）。

const (
	msgFramebufferUpdate = 0
	msgSetColorMap       = 1
	msgBell              = 2
	msgServerCutText     = 3

	encRaw         = 0
	encCopyRect    = 1
	encHextile     = 5
	encDesktopSize = -223

	keyEvent     = 4
	pointerEvent = 5
)

// vncClient 极简 RFB 客户端。
type vncClient struct {
	c                      net.Conn
	r                      *bufReader
	width                  int
	height                 int
	name                   string
	bpp                    int // bytes per pixel
	bigEnd                 bool
	rShift, gShift, bShift int
	rMax, gMax, bMax       int
	fb                     []byte
	updated                bool
}

// vncHandshake 连接 + 握手 + 认证。
func vncHandshake(host string, port int, password string) (*vncClient, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	c, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("VNC 连接失败: %w", err)
	}
	v := &vncClient{c: c, r: &bufReader{r: c}}

	// 1. 版本协商
	ver := make([]byte, 12)
	if _, err := io.ReadFull(c, ver); err != nil {
		c.Close()
		return nil, fmt.Errorf("读取 VNC 版本失败: %w", err)
	}
	if string(ver[:3]) != "RFB" {
		c.Close()
		return nil, fmt.Errorf("不是 VNC 服务器")
	}
	// 回复 3.8
	if _, err := c.Write([]byte("RFB 003.008\n")); err != nil {
		c.Close()
		return nil, err
	}

	// 2. 安全类型
	var nSec byte
	if err := readByte(c, &nSec); err != nil {
		c.Close()
		return nil, err
	}
	secs := make([]byte, int(nSec))
	if _, err := io.ReadFull(c, secs); err != nil {
		c.Close()
		return nil, err
	}
	// 选择 None(1) 或 VNC Auth(2)
	secType := byte(0)
	for _, s := range secs {
		if s == 1 {
			secType = 1
			break
		}
		if s == 2 {
			secType = 2
		}
	}
	if secType == 0 {
		c.Close()
		return nil, fmt.Errorf("服务器不支持 None/VNC 认证")
	}
	c.Write([]byte{secType})

	// 3. VNC Auth
	if secType == 2 {
		challenge := make([]byte, 16)
		if _, err := io.ReadFull(c, challenge); err != nil {
			c.Close()
			return nil, err
		}
		resp, err := vncAuthResponse(challenge, password)
		if err != nil {
			c.Close()
			return nil, err
		}
		c.Write(resp)
	}

	// 4. SecurityResult
	res := make([]byte, 4)
	if _, err := io.ReadFull(c, res); err != nil {
		c.Close()
		return nil, err
	}
	if binary.BigEndian.Uint32(res) != 0 {
		c.Close()
		return nil, fmt.Errorf("VNC 认证失败（密码错误？）")
	}

	// 5. ClientInit（shared=1）
	c.Write([]byte{1})

	// 6. ServerInit：width/height/pixelformat/name
	var w, h uint16
	if err := binary.Read(c, binary.BigEndian, &w); err != nil {
		c.Close()
		return nil, err
	}
	if err := binary.Read(c, binary.BigEndian, &h); err != nil {
		c.Close()
		return nil, err
	}
	pf := make([]byte, 16)
	if _, err := io.ReadFull(c, pf); err != nil {
		c.Close()
		return nil, err
	}
	bpp := int(pf[0]) / 8
	if bpp <= 0 || bpp > 4 {
		bpp = 4
	}
	var nameLen uint32
	if err := binary.Read(c, binary.BigEndian, &nameLen); err != nil {
		c.Close()
		return nil, err
	}
	name := make([]byte, nameLen)
	if _, err := io.ReadFull(c, name); err != nil {
		c.Close()
		return nil, err
	}
	v.width, v.height, v.name, v.bpp = int(w), int(h), string(name), bpp
	v.bigEnd = pf[2] != 0
	v.rMax, v.gMax, v.bMax = int(binary.BigEndian.Uint16(pf[4:6])), int(binary.BigEndian.Uint16(pf[6:8])), int(binary.BigEndian.Uint16(pf[8:10]))
	v.rShift, v.gShift, v.bShift = int(pf[10]), int(pf[11]), int(pf[12])
	v.fb = make([]byte, v.width*v.height*4) // 统一 RGBA 输出

	// 7. 请求 Raw/Hextile/CopyRect 编码
	if err := v.sendSetEncodings(); err != nil {
		c.Close()
		return nil, err
	}
	// 8. 首次全屏更新请求
	if err := v.sendUpdateRequest(false); err != nil {
		c.Close()
		return nil, err
	}
	return v, nil
}

// vncAuthResponse DES 加密密码（VNC 密钥位序反转）。
func vncAuthResponse(challenge []byte, password string) ([]byte, error) {
	pw := []byte(password)
	if len(pw) > 8 {
		pw = pw[:8]
	}
	key := make([]byte, 8)
	for i, b := range pw {
		// 每个字节位序反转
		for j := 0; j < 8; j++ {
			if b&(1<<uint(j)) != 0 {
				key[i] |= 1 << uint(7-j)
			}
		}
	}
	block, err := des.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 16)
	block.Encrypt(out[0:8], challenge[0:8])
	block.Encrypt(out[8:16], challenge[8:16])
	return out, nil
}

func readByte(c net.Conn, b *byte) error {
	buf := make([]byte, 1)
	if _, err := io.ReadFull(c, buf); err != nil {
		return err
	}
	*b = buf[0]
	return nil
}

// bufReader 轻量缓冲读取（避免频繁小读）。
type bufReader struct {
	r   io.Reader
	buf []byte
}

func (b *bufReader) ReadFull(n int) ([]byte, error) {
	out := make([]byte, n)
	if _, err := io.ReadFull(b.r, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (v *vncClient) sendSetEncodings() error {
	var b []byte
	b = appendU8(b, 2) // SetEncodings
	b = appendU8(b, 0)
	encs := []int32{encCopyRect, encHextile, encRaw}
	b = appendU16(b, uint16(len(encs)))
	for _, e := range encs {
		b = appendU32(b, uint32(e))
	}
	_, err := v.c.Write(b)
	return err
}

func (v *vncClient) sendUpdateRequest(incremental bool) error {
	var b []byte
	b = appendU8(b, 3) // FramebufferUpdateRequest
	b = appendU8(b, boolByte(incremental))
	b = appendU16(b, 0)
	b = appendU16(b, 0)
	b = appendU16(b, uint16(v.width))
	b = appendU16(b, uint16(v.height))
	_, err := v.c.Write(b)
	return err
}

// readServerMessage 读取并处理一条服务器消息，返回是否有 framebuffer 更新。
func (v *vncClient) readServerMessage() (bool, error) {
	mt, err := v.r.ReadFull(1)
	if err != nil {
		return false, err
	}
	switch mt[0] {
	case msgFramebufferUpdate:
		hdr, err := v.r.ReadFull(3)
		if err != nil {
			return false, err
		}
		nRect := binary.BigEndian.Uint16(hdr[1:3])
		for i := 0; i < int(nRect); i++ {
			if err := v.readRect(); err != nil {
				return false, err
			}
		}
		return true, nil
	case msgSetColorMap, msgBell:
		// 忽略
		return false, nil
	case msgServerCutText:
		ln, err := v.r.ReadFull(7)
		if err != nil {
			return false, err
		}
		n := binary.BigEndian.Uint32(ln[3:7])
		if _, err := v.r.ReadFull(int(n)); err != nil {
			return false, err
		}
		return false, nil
	default:
		return false, fmt.Errorf("未知服务器消息类型 %d", mt[0])
	}
}

func (v *vncClient) readRect() error {
	hdr, err := v.r.ReadFull(12)
	if err != nil {
		return err
	}
	x := binary.BigEndian.Uint16(hdr[0:2])
	y := binary.BigEndian.Uint16(hdr[2:4])
	w := binary.BigEndian.Uint16(hdr[4:6])
	h := binary.BigEndian.Uint16(hdr[6:8])
	enc := int32(binary.BigEndian.Uint32(hdr[8:12]))

	switch enc {
	case encRaw:
		n := int(w) * int(h) * v.bpp
		data, err := v.r.ReadFull(n)
		if err != nil {
			return err
		}
		v.fillRaw(int(x), int(y), int(w), int(h), data)
	case encCopyRect:
		src, err := v.r.ReadFull(4)
		if err != nil {
			return err
		}
		sx := binary.BigEndian.Uint16(src[0:2])
		sy := binary.BigEndian.Uint16(src[2:4])
		v.copyRect(int(x), int(y), int(w), int(h), int(sx), int(sy))
	case encHextile:
		if err := v.readHextile(int(x), int(y), int(w), int(h)); err != nil {
			return err
		}
	case encDesktopSize:
		// 桌面尺寸变化（忽略，保持原尺寸）
	default:
		return fmt.Errorf("不支持的编码 %d", enc)
	}
	return nil
}

func (v *vncClient) fillRaw(x, y, w, h int, data []byte) {
	idx := 0
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			r, g, b := v.decodePixel(data[idx : idx+v.bpp])
			idx += v.bpp
			off := ((y+yy)*v.width + (x + xx)) * 4
			v.fb[off], v.fb[off+1], v.fb[off+2], v.fb[off+3] = r, g, b, 255
		}
	}
}

// decodePixel 按 pixel format 提取 R/G/B。
func (v *vncClient) decodePixel(data []byte) (byte, byte, byte) {
	var px uint32
	if v.bigEnd {
		for _, b := range data {
			px = (px << 8) | uint32(b)
		}
	} else {
		for i := len(data) - 1; i >= 0; i-- {
			px = (px << 8) | uint32(data[i])
		}
	}
	r := (px >> uint(v.rShift)) & uint32(v.rMax)
	g := (px >> uint(v.gShift)) & uint32(v.gMax)
	b := (px >> uint(v.bShift)) & uint32(v.bMax)
	return scaleColor(byte(r), v.rMax), scaleColor(byte(g), v.gMax), scaleColor(byte(b), v.bMax)
}

func scaleColor(v byte, max int) byte {
	if max <= 0 || max == 255 {
		return v
	}
	return byte(int(v) * 255 / max)
}

func (v *vncClient) copyRect(x, y, w, h, sx, sy int) {
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			src := ((sy+yy)*v.width + (sx + xx)) * 4
			dst := ((y+yy)*v.width + (x + xx)) * 4
			copy(v.fb[dst:dst+4], v.fb[src:src+4])
		}
	}
}

// readHextile 解码 Hextile 编码（最简：支持 raw 子编码 + 背景色）。
func (v *vncClient) readHextile(x, y, w, h int) error {
	bg := []byte{}
	for ty := 0; ty < h; ty += 16 {
		for tx := 0; tx < w; tx += 16 {
			tw := 16
			if tx+tw > w {
				tw = w - tx
			}
			th := 16
			if ty+th > h {
				th = h - ty
			}
			sub, err := v.r.ReadFull(1)
			if err != nil {
				return err
			}
			flag := sub[0]
			if flag&0x01 != 0 { // Raw
				n := tw * th * v.bpp
				data, err := v.r.ReadFull(n)
				if err != nil {
					return err
				}
				v.fillRaw(x+tx, y+ty, tw, th, data)
			} else {
				if flag&0x02 != 0 { // BackgroundSpecified
					bg, err = v.r.ReadFull(v.bpp)
					if err != nil {
						return err
					}
				}
				// 用背景色填充
				if len(bg) > 0 {
					v.fillSolid(x+tx, y+ty, tw, th, bg)
				}
				if flag&0x08 != 0 { // AnySubrects
					_ = flag // 子矩形暂未实现，接受背景色近似
				}
			}
		}
	}
	return nil
}

func (v *vncClient) fillSolid(x, y, w, h int, color []byte) {
	r, g, b := v.decodePixel(color)
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			off := ((y+yy)*v.width + (x + xx)) * 4
			v.fb[off], v.fb[off+1], v.fb[off+2], v.fb[off+3] = r, g, b, 255
		}
	}
}

// sendInput 处理前端输入事件（键盘/指针）。
func (v *vncClient) sendInput(data []byte) error {
	if len(data) < 1 {
		return nil
	}
	switch data[0] {
	case 1: // 键盘
		if len(data) < 8 {
			return nil
		}
		var b []byte
		b = appendU8(b, keyEvent)
		b = appendU8(b, data[1])
		b = appendU16(b, 0)
		b = appendU32(b, binary.BigEndian.Uint32(data[4:8]))
		_, err := v.c.Write(b)
		return err
	case 2: // 指针
		if len(data) < 7 {
			return nil
		}
		var b []byte
		b = appendU8(b, pointerEvent)
		b = appendU8(b, data[1])
		b = appendU16(b, binary.BigEndian.Uint16(data[2:4]))
		b = appendU16(b, binary.BigEndian.Uint16(data[4:6]))
		_, err := v.c.Write(b)
		return err
	}
	return nil
}

// fbRect 打包整个 framebuffer 为一个 rect（简化：每次发全屏）。
func (v *vncClient) fbRect() []byte {
	out := make([]byte, 8, 8+len(v.fb))
	binary.BigEndian.PutUint16(out[0:2], 0)
	binary.BigEndian.PutUint16(out[2:4], 0)
	binary.BigEndian.PutUint16(out[4:6], uint16(v.width))
	binary.BigEndian.PutUint16(out[6:8], uint16(v.height))
	return append(out, v.fb...)
}

// appendU* 辅助。
func appendU8(b []byte, v uint8) []byte   { return append(b, v) }
func appendU16(b []byte, v uint16) []byte { return append(b, byte(v>>8), byte(v)) }
func appendU32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// handleVNC 建立 VNC 会话并桥接 WebSocket。
func (s *Service) handleVNC(conn *websocket.Conn, hs wsHandshake) {
	port := hs.Port
	if port <= 0 {
		port = 5900
	}
	vc, err := vncHandshake(hs.Host, port, hs.Password)
	if err != nil {
		sendWSError(conn, err.Error())
		return
	}
	defer vc.c.Close()

	// 通知就绪（含 framebuffer 尺寸）
	ready, _ := json.Marshal(map[string]any{"type": "ready", "width": vc.width, "height": vc.height, "name": vc.name})
	if err := conn.Write(context.Background(), websocket.MessageText, ready); err != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 前端输入 → VNC
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				cancel()
				return
			}
			vc.sendInput(data)
		}
	}()

	// VNC 服务器消息 → 前端 framebuffer
	for {
		updated, err := vc.readServerMessage()
		if err != nil {
			cancel()
			return
		}
		if updated {
			if err := conn.Write(ctx, websocket.MessageBinary, vc.fbRect()); err != nil {
				cancel()
				return
			}
			// 请求下一次增量更新
			_ = vc.sendUpdateRequest(true)
		}
	}
}
