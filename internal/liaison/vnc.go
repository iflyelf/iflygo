package liaison

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/des"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"net"
	"strconv"
	"time"

	"nhooyr.io/websocket"

	kzlib "github.com/klauspost/compress/zlib"
)

// Web VNC（RFB 协议客户端）：连接 VNC 服务器，解码 framebuffer，经 WebSocket 桥接到浏览器。
//
// 支持：RFB 3.3/3.7/3.8 握手、NoAuth + VNC Auth、Raw/Hextile（含子矩形）/CopyRect/Tight/ZRLE 编码、
// 键盘/指针输入。RDP 在 rdp.go 中基于 grdp 客户端实现。

const (
	msgFramebufferUpdate = 0
	msgSetColorMap       = 1
	msgBell              = 2
	msgServerCutText     = 3

	encRaw         = 0
	encCopyRect    = 1
	encHextile     = 5
	encTight       = 7
	encZRLE        = 16
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
	// Tight 编码的跨 tile zlib 流（klauspost 支持 io.ByteReader 精确读取，不预读）
	tzr io.ReadCloser
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
	encs := []int32{encCopyRect, encHextile, encTight, encZRLE, encRaw}
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
	case encTight:
		if err := v.readTight(int(x), int(y), int(w), int(h)); err != nil {
			return err
		}
	case encZRLE:
		if err := v.readZRLE(int(x), int(y), int(w), int(h)); err != nil {
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
	var bg, fg []byte // 背景/前景色跨 tile 持久（未指定时沿用上一个 tile）
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
				data, err := v.r.ReadFull(tw * th * v.bpp)
				if err != nil {
					return err
				}
				v.fillRaw(x+tx, y+ty, tw, th, data)
				continue
			}
			// 非 Raw：背景/前景 + 子矩形
			if flag&0x02 != 0 { // BackgroundSpecified
				if bg, err = v.r.ReadFull(v.bpp); err != nil {
					return err
				}
			}
			if len(bg) > 0 {
				v.fillSolid(x+tx, y+ty, tw, th, bg)
			}
			if flag&0x04 != 0 { // ForegroundSpecified
				if fg, err = v.r.ReadFull(v.bpp); err != nil {
					return err
				}
			}
			if flag&0x08 != 0 { // AnySubrects
				nb, err := v.r.ReadFull(1)
				if err != nil {
					return err
				}
				n := int(nb[0])
				coloured := flag&0x10 != 0 // SubrectsColoured
				for i := 0; i < n; i++ {
					var color []byte
					if coloured {
						if color, err = v.r.ReadFull(v.bpp); err != nil {
							return err
						}
					} else {
						color = fg
					}
					pos, err := v.r.ReadFull(1)
					if err != nil {
						return err
					}
					size, err := v.r.ReadFull(1)
					if err != nil {
						return err
					}
					sx := int(pos[0]>>4) & 0x0F
					sy := int(pos[0]) & 0x0F
					sw := int(size[0]>>4)&0x0F + 1
					sh := int(size[0])&0x0F + 1
					if sx+sw > tw {
						sw = tw - sx
					}
					if sy+sh > th {
						sh = th - sy
					}
					if len(color) > 0 && sw > 0 && sh > 0 {
						v.fillSolid(x+tx+sx, y+ty+sy, sw, sh, color)
					}
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

// readZRLE 解码 ZRLE 编码（encoding 16）：4 字节长度 + zlib 压缩的 tile 数据。
func (v *vncClient) readZRLE(x, y, w, h int) error {
	ln, err := v.r.ReadFull(4)
	if err != nil {
		return err
	}
	length := binary.BigEndian.Uint32(ln)
	data, err := v.r.ReadFull(int(length))
	if err != nil {
		return err
	}
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	dec, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		return err
	}
	d := dec
	for ty := 0; ty < h; ty += 64 {
		for tx := 0; tx < w; tx += 64 {
			tw := 64
			if tx+tw > w {
				tw = w - tx
			}
			th := 64
			if ty+th > h {
				th = h - ty
			}
			if len(d) < 1 {
				return io.ErrUnexpectedEOF
			}
			sub := d[0]
			d = d[1:]
			if err := v.zrleTile(x+tx, y+ty, tw, th, sub, &d); err != nil {
				return err
			}
		}
	}
	return nil
}

func (v *vncClient) zrleTile(x, y, w, h int, sub byte, d *[]byte) error {
	pix := w * h
	switch {
	case sub == 0: // Raw
		n := pix * v.bpp
		if len(*d) < n {
			return io.ErrUnexpectedEOF
		}
		v.fillRaw(x, y, w, h, (*d)[:n])
		*d = (*d)[n:]
	case sub == 1: // Solid
		if len(*d) < v.bpp {
			return io.ErrUnexpectedEOF
		}
		v.fillSolid(x, y, w, h, (*d)[:v.bpp])
		*d = (*d)[v.bpp:]
	case sub >= 2 && sub <= 16: // Packed palette
		ps := int(sub)
		palette := make([]byte, ps*v.bpp)
		if len(*d) < len(palette) {
			return io.ErrUnexpectedEOF
		}
		copy(palette, (*d)[:len(palette)])
		*d = (*d)[len(palette):]
		bits := 0
		for ps > 1 {
			bits++
			ps >>= 1
		}
		br := &bitReader{data: *d}
		row := make([]byte, 0, w*v.bpp)
		for i := 0; i < pix; i++ {
			idx, err := br.readBits(bits)
			if err != nil {
				return err
			}
			off := int(idx) * v.bpp
			if off >= len(palette) {
				return io.ErrUnexpectedEOF
			}
			row = append(row, palette[off:off+v.bpp]...)
			if len(row) == w*v.bpp {
				v.fillRaw(x, y+(i+1-len(row)/v.bpp)/1, w, 1, row)
				// 逐行填充：更简单的方式是累积整个 tile 再 fill
			}
		}
		// 简化：累积整个 tile 的像素，一次 fillRaw
		tile := make([]byte, 0, pix*v.bpp)
		for i := 0; i < pix; i++ {
			idx, err := br.readBits(bits)
			if err != nil {
				return err
			}
			off := int(idx) * v.bpp
			if off >= len(palette) {
				return io.ErrUnexpectedEOF
			}
			tile = append(tile, palette[off:off+v.bpp]...)
		}
		*d = br.remaining()
		v.fillRaw(x, y, w, h, tile)
	case sub == 128: // Plain RLE
		yy := 0
		xx := 0
		for yy < h {
			if len(*d) < v.bpp {
				return io.ErrUnexpectedEOF
			}
			color := (*d)[:v.bpp]
			*d = (*d)[v.bpp:]
			run := 1
			for {
				if len(*d) < 1 {
					return io.ErrUnexpectedEOF
				}
				b := (*d)[0]
				*d = (*d)[1:]
				run += int(b)
				if b != 255 {
					break
				}
			}
			for i := 0; i < run; i++ {
				if xx >= w {
					xx = 0
					yy++
					if yy >= h {
						break
					}
				}
				v.fillSolid(x+xx, y+yy, 1, 1, color)
				xx++
			}
		}
	case sub >= 130 && sub <= 255: // Palette RLE
		ps := int(sub) - 128
		palette := make([]byte, ps*v.bpp)
		if len(*d) < len(palette) {
			return io.ErrUnexpectedEOF
		}
		copy(palette, (*d)[:len(palette)])
		*d = (*d)[len(palette):]
		yy, xx := 0, 0
		for yy < h {
			if len(*d) < 1 {
				return io.ErrUnexpectedEOF
			}
			idx := int((*d)[0])
			*d = (*d)[1:]
			if idx >= ps {
				return io.ErrUnexpectedEOF
			}
			color := palette[idx*v.bpp : idx*v.bpp+v.bpp]
			run := 1
			for {
				if len(*d) < 1 {
					return io.ErrUnexpectedEOF
				}
				b := (*d)[0]
				*d = (*d)[1:]
				run += int(b)
				if b != 255 {
					break
				}
			}
			for i := 0; i < run; i++ {
				if xx >= w {
					xx = 0
					yy++
					if yy >= h {
						break
					}
				}
				v.fillSolid(x+xx, y+yy, 1, 1, color)
				xx++
			}
		}
	default:
		return fmt.Errorf("未知 ZRLE 子编码 %d", sub)
	}
	return nil
}

// bitReader 从字节切片按 MSB 优先读位。
type bitReader struct {
	data []byte
	pos  int
}

func (b *bitReader) readBits(n int) (uint32, error) {
	var val uint32
	for i := 0; i < n; i++ {
		byteIdx := b.pos / 8
		if byteIdx >= len(b.data) {
			return 0, io.ErrUnexpectedEOF
		}
		bit := (b.data[byteIdx] >> uint(7-b.pos%8)) & 1
		val = (val << 1) | uint32(bit)
		b.pos++
	}
	return val, nil
}

func (b *bitReader) remaining() []byte {
	out := b.data[b.pos/8:]
	if b.pos%8 != 0 {
		out = b.data[b.pos/8+1:]
	}
	return out
}

// readTight 解码 Tight 编码（encoding 7）：16x16 tile + control byte。
// readTight 解码 Tight 编码（encoding 7）：16x16 tile + control byte。
// fill/jpeg 子编码独立可靠；basic/palette/gradient 的 zlib 数据在 Tight 规范中跨 tile 连续，
// 但 Go 标准库的流式 zlib（bufio 预读）无法在 tile 边界精确暂停/恢复，
// 故 basic/palette 采用「每 tile 独立 zlib 流」（对 reset-per-tile 的服务器正确）。
func (v *vncClient) readTight(x, y, w, h int) error {
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
			ctrl, err := v.r.ReadFull(1)
			if err != nil {
				return err
			}
			if ctrl[0]&0x01 != 0 { // reset zlib 流
				if v.tzr != nil {
					_ = v.tzr.Close()
					v.tzr = nil
				}
			}
			comp := ctrl[0] >> 4
			switch comp {
			case 0x08: // fill
				color, err := v.r.ReadFull(v.bpp)
				if err != nil {
					return err
				}
				v.fillSolid(x+tx, y+ty, tw, th, color)
			case 0x09: // jpeg
				if err := v.readTightJPEG(x+tx, y+ty, tw, th); err != nil {
					return err
				}
			default: // basic (0-7)
				if err := v.readTightBasic(x+tx, y+ty, tw, th, comp); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// readTightBasic 解码 Tight basic 子编码（zlib 像素 / palette / gradient）。
func (v *vncClient) readTightBasic(x, y, w, h int, comp byte) error {
	switch {
	case comp == 0: // 纯 zlib 像素
		raw := make([]byte, w*h*v.bpp)
		if _, err := v.readTightZlib(raw); err != nil {
			return err
		}
		v.fillRaw(x, y, w, h, raw)
	case comp >= 1 && comp <= 4: // palette
		size, err := v.r.ReadFull(1)
		if err != nil {
			return err
		}
		ps := int(size[0]) + 1
		palette := make([]byte, ps*v.bpp)
		if _, err := v.readTightZlib(palette); err != nil {
			return err
		}
		// Tight 规范：2 色用 1 bit/像素，否则 8 bit/像素
		bits := 8
		if ps == 2 {
			bits = 1
		}
		idxRaw := make([]byte, (w*h*bits+7)/8)
		if _, err := v.readTightZlib(idxRaw); err != nil {
			return err
		}
		br := &bitReader{data: idxRaw}
		tile := make([]byte, 0, w*h*v.bpp)
		for i := 0; i < w*h; i++ {
			idx, err := br.readBits(bits)
			if err != nil {
				return err
			}
			if int(idx) >= ps {
				return io.ErrUnexpectedEOF
			}
			tile = append(tile, palette[int(idx)*v.bpp:int(idx)*v.bpp+v.bpp]...)
		}
		v.fillRaw(x, y, w, h, tile)
	default: // gradient (5-7)
		return v.readTightGradient(x, y, w, h)
	}
	return nil
}

// readTightZlib 用跨 tile 复用的 zlib 流解压（klauspost 支持 io.ByteReader 精确读取，
// 不预读后续 tile 的 control 字节，从而正确处理 Tight 的跨 tile 连续流）。
func (v *vncClient) readTightZlib(dst []byte) (int, error) {
	if v.tzr == nil {
		zr, err := kzlib.NewReader(&tightByteReader{r: v.r.r})
		if err != nil {
			return 0, err
		}
		v.tzr = zr
	}
	return io.ReadFull(v.tzr, dst)
}

// tightByteReader 从底层 VNC 流逐字节读取；实现 io.ByteReader 使 klauspost zlib
// 精确读取（不多读），保证跨 tile 的 control 字节不被吞掉。
type tightByteReader struct {
	r io.Reader
}

func (t *tightByteReader) ReadByte() (byte, error) {
	var b [1]byte
	if _, err := io.ReadFull(t.r, b[:]); err != nil {
		return 0, err
	}
	return b[0], nil
}

func (t *tightByteReader) Read(p []byte) (int, error) {
	return t.r.Read(p)
}

// readTightJPEG 解码 Tight JPEG 子编码（3 字节长度 + JPEG 数据）。
func (v *vncClient) readTightJPEG(x, y, w, h int) error {
	ln, err := v.r.ReadFull(3)
	if err != nil {
		return err
	}
	length := int(ln[0])<<16 | int(ln[1])<<8 | int(ln[2])
	data, err := v.r.ReadFull(length)
	if err != nil {
		return err
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	bounds := img.Bounds()
	dw, dh := bounds.Dx(), bounds.Dy()
	if dw != w || dh != h {
		return fmt.Errorf("Tight JPEG 尺寸不匹配：%dx%d != %dx%d", dw, dh, w, h)
	}
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			rr, gg, bb, _ := img.At(bounds.Min.X+xx, bounds.Min.Y+yy).RGBA()
			off := ((y+yy)*v.width + (x + xx)) * 4
			v.fb[off], v.fb[off+1], v.fb[off+2], v.fb[off+3] = byte(rr>>8), byte(gg>>8), byte(bb>>8), 255
		}
	}
	return nil
}

// readTightGradient 解码 Tight gradient 子编码（compression 5-7）。
// gradient 数据：每行首像素 v.bpp 字节 + 其余像素 RGB565 相对梯度（zlib 压缩）。
func (v *vncClient) readTightGradient(x, y, w, h int) error {
	// gradient 数据：每行首像素 bpp 字节 + 其余像素相对梯度 delta。
	// delta 位数：bpp<=2 用 8bit（RGB332，每像素 1 字节）；bpp>=3 用 16bit（RGB565，每像素 2 字节）。
	deltaBytes := 2
	if v.bpp <= 2 {
		deltaBytes = 1
	}
	raw := make([]byte, h*(v.bpp+(w-1)*deltaBytes))
	n, err := v.readTightZlib(raw)
	if err != nil {
		return err
	}
	raw = raw[:n]
	idx := 0
	for yy := 0; yy < h; yy++ {
		if idx+v.bpp > len(raw) {
			return io.ErrUnexpectedEOF
		}
		first := raw[idx : idx+v.bpp]
		idx += v.bpp
		fr, fg, fb := v.decodePixel(first)
		off := ((y+yy)*v.width + x) * 4
		v.fb[off], v.fb[off+1], v.fb[off+2], v.fb[off+3] = fr, fg, fb, 255
		pr, pg, pb := int(fr), int(fg), int(fb)
		for xx := 1; xx < w; xx++ {
			if deltaBytes == 1 {
				// 8bit RGB332 delta
				if idx+1 > len(raw) {
					return io.ErrUnexpectedEOF
				}
				d := raw[idx]
				idx++
				dr := int(d>>5) & 0x07
				dg := int(d>>2) & 0x07
				db := int(d) & 0x03
				if dr&0x04 != 0 {
					dr -= 8
				}
				if dg&0x04 != 0 {
					dg -= 8
				}
				if db&0x02 != 0 {
					db -= 4
				}
				pr, pg, pb = clamp8(pr+dr), clamp8(pg+dg), clamp8(pb+db)
			} else {
				// 16bit RGB565 delta
				if idx+2 > len(raw) {
					return io.ErrUnexpectedEOF
				}
				delta := int16(binary.LittleEndian.Uint16(raw[idx : idx+2]))
				idx += 2
				dr := int(delta & 0x1F)
				dg := int((delta >> 5) & 0x1F)
				db := int((delta >> 10) & 0x1F)
				if dr&0x10 != 0 {
					dr -= 32
				}
				if dg&0x10 != 0 {
					dg -= 32
				}
				if db&0x10 != 0 {
					db -= 32
				}
				pr, pg, pb = clamp8(pr+dr), clamp8(pg+dg), clamp8(pb+db)
			}
			o := ((y+yy)*v.width + (x + xx)) * 4
			v.fb[o], v.fb[o+1], v.fb[o+2], v.fb[o+3] = byte(pr), byte(pg), byte(pb), 255
		}
	}
	return nil
}

func clamp8(n int) int {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return n
}

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
