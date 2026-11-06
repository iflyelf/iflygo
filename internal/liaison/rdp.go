package liaison

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"strconv"
	"sync"

	"nhooyr.io/websocket"

	grdpclient "github.com/tomatome/grdp/client"
)

// Web RDP（基于 grdp 客户端，fork 于 third_party/grdp，已移除 CGO 依赖）。
// grdp 处理 RDP 连接/认证/位图解压，此处负责 WebSocket 桥接：framebuffer → 前端、输入 → RDP。

// handleRDP 建立 RDP 会话并桥接 WebSocket。
func (s *Service) handleRDP(conn *websocket.Conn, hs wsHandshake) {
	port := hs.Port
	if port <= 0 {
		port = 3389
	}
	host := net.JoinHostPort(hs.Host, strconv.Itoa(port))

	setting := &grdpclient.Setting{Width: 1024, Height: 768}
	if hs.Cols > 0 {
		setting.Width = hs.Cols
	}
	if hs.Rows > 0 {
		setting.Height = hs.Rows
	}

	cli := grdpclient.NewClient(host, hs.User, hs.Password, grdpclient.TC_RDP, setting)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	closed := false
	closeOnce := func() {
		mu.Lock()
		if !closed {
			closed = true
			cancel()
		}
		mu.Unlock()
	}

	cli.OnReady(func() {
		ready, _ := json.Marshal(map[string]any{"type": "ready", "width": setting.Width, "height": setting.Height, "name": hs.Host})
		mu.Lock()
		_ = conn.Write(ctx, websocket.MessageText, ready)
		mu.Unlock()
	})
	cli.OnBitmap(func(bs []grdpclient.Bitmap) {
		for _, b := range bs {
			rect := rdpBitmapToRect(b)
			if rect == nil {
				continue
			}
			mu.Lock()
			if werr := conn.Write(ctx, websocket.MessageBinary, rect); werr != nil {
				mu.Unlock()
				closeOnce()
				return
			}
			mu.Unlock()
		}
	})
	cli.OnError(func(e error) {
		sendWSError(conn, "RDP 错误: "+e.Error())
		closeOnce()
	})
	cli.OnClose(func() {
		closeOnce()
	})

	// 登录（阻塞，后台执行）
	go func() {
		if err := cli.Login(); err != nil {
			sendWSError(conn, "RDP 登录失败: "+err.Error())
			closeOnce()
			return
		}
	}()

	// 输入循环
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		rdpHandleInput(cli, data)
	}
}

// rdpBitmapToRect 把 grdp 的 Bitmap（BGR/RGB565 像素）转为 RGBA rect：
// [x(2) y(2) w(2) h(2)][RGBA 像素]。
func rdpBitmapToRect(b grdpclient.Bitmap) []byte {
	w, h := b.Width, b.Height
	if w <= 0 || h <= 0 || len(b.Data) == 0 {
		return nil
	}
	out := make([]byte, 8+w*h*4)
	binary.BigEndian.PutUint16(out[0:2], uint16(b.DestLeft))
	binary.BigEndian.PutUint16(out[2:4], uint16(b.DestTop))
	binary.BigEndian.PutUint16(out[4:6], uint16(w))
	binary.BigEndian.PutUint16(out[6:8], uint16(h))
	px := out[8:]
	idx := 0
	bpp := grdpclient.Bpp(uint16(b.BitsPerPixel))
	for i := 0; i < w*h; i++ {
		var r, g, bl byte
		switch bpp {
		case 4: // 32bpp BGRX
			if idx+4 > len(b.Data) {
				return out[:8+i*4]
			}
			bl, g, r = b.Data[idx], b.Data[idx+1], b.Data[idx+2]
			idx += 4
		case 3: // 24bpp BGR
			if idx+3 > len(b.Data) {
				return out[:8+i*4]
			}
			bl, g, r = b.Data[idx], b.Data[idx+1], b.Data[idx+2]
			idx += 3
		case 2: // 16bpp RGB565
			if idx+2 > len(b.Data) {
				return out[:8+i*4]
			}
			v := binary.LittleEndian.Uint16(b.Data[idx : idx+2])
			idx += 2
			r = byte((v >> 11) & 0x1F << 3)
			g = byte((v >> 5) & 0x3F << 2)
			bl = byte(v & 0x1F << 3)
		default:
			return out[:8+i*4]
		}
		o := i * 4
		px[o], px[o+1], px[o+2], px[o+3] = r, g, bl, 255
	}
	return out
}

// rdpHandleInput 处理前端输入（复用 VNC 的二进制格式）。
// 键盘：[1][down][2 字节 0][4 字节 scancode]；指针：[2][buttonmask][2 字节 x][2 字节 y]。
func rdpHandleInput(cli *grdpclient.Client, data []byte) {
	if len(data) < 1 {
		return
	}
	switch data[0] {
	case 1: // 键盘
		if len(data) < 8 {
			return
		}
		sc := int(binary.BigEndian.Uint32(data[4:8]))
		if data[1] == 1 {
			cli.KeyDown(sc, "")
		} else {
			cli.KeyUp(sc, "")
		}
	case 2: // 指针
		if len(data) < 7 {
			return
		}
		mask := data[1]
		x := int(binary.BigEndian.Uint16(data[2:4]))
		y := int(binary.BigEndian.Uint16(data[4:6]))
		// buttonmask bit0=左, bit1=中, bit2=右；grdp button 0=左,1=中,2=右
		if mask&0x01 != 0 {
			cli.MouseDown(0, x, y)
		}
		if mask&0x02 != 0 {
			cli.MouseDown(1, x, y)
		}
		if mask&0x04 != 0 {
			cli.MouseDown(2, x, y)
		}
		cli.MouseMove(x, y)
	}
}
