package liaison

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
)

// startMockVNCEncoded 启动 mock VNC 服务器，发送一个指定编码的 framebuffer 更新。
// width/height 是 framebuffer 尺寸；encoding 是 RFB 编码号；rectData 是编码后的像素数据。
func startMockVNCEncoded(t *testing.T, width, height int, encoding int32, rectData []byte) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Write([]byte("RFB 003.008\n"))
		buf := make([]byte, 12)
		io.ReadFull(c, buf)
		c.Write([]byte{1, 1}) // None
		b := make([]byte, 1)
		io.ReadFull(c, b)
		c.Write([]byte{0, 0, 0, 0}) // SecurityResult 成功
		io.ReadFull(c, b)           // ClientInit
		// ServerInit
		init := make([]byte, 0, 24)
		init = append(init, byte(width>>8), byte(width), byte(height>>8), byte(height))
		pf := []byte{32, 24, 1, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0}
		init = append(init, pf...)
		name := []byte("mock")
		init = append(init, 0, 0, 0, byte(len(name)))
		init = append(init, name...)
		c.Write(init)
		// SetEncodings
		hdr := make([]byte, 4)
		io.ReadFull(c, hdr)
		n := binary.BigEndian.Uint16(hdr[2:4])
		io.ReadFull(c, make([]byte, n*4))
		// FramebufferUpdateRequest
		io.ReadFull(c, make([]byte, 10))
		// FramebufferUpdate：1 个 rect
		fb := make([]byte, 0, 16+len(rectData))
		fb = append(fb, 0, 0) // type + padding
		fb = append(fb, 0, 1) // nRect=1
		fb = append(fb, 0, 0, 0, 0, byte(width>>8), byte(width), byte(height>>8), byte(height))
		enc := make([]byte, 4)
		binary.BigEndian.PutUint32(enc, uint32(int32ToUint32(encoding)))
		fb = append(fb, enc...)
		fb = append(fb, rectData...)
		c.Write(fb)
		<-make(chan struct{})
	}()

	return ln.Addr().String()
}

func int32ToUint32(v int32) uint32 { return uint32(v) }

// dialVNC 连接 mock VNC 并完成握手，返回客户端。
func dialVNC(t *testing.T, addr string) *vncClient {
	t.Helper()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	vc, err := vncHandshake(host, port, "")
	if err != nil {
		t.Fatalf("vncHandshake: %v", err)
	}
	t.Cleanup(func() { vc.c.Close() })
	return vc
}

// TestVNCHextileEndToEnd 端到端：mock 服务器用 Hextile（含子矩形）发送 framebuffer。
func TestVNCHextileEndToEnd(t *testing.T) {
	// 2x2 framebuffer，Hextile 单 tile：
	// 背景红色，子矩形 (1,0) 1x1 前景色蓝色。
	hextile := []byte{
		0x0E,                   // BackgroundSpecified|ForegroundSpecified|AnySubrects
		0x00, 0xFF, 0x00, 0x00, // 背景红
		0x00, 0x00, 0x00, 0xFF, // 前景蓝
		0x01,       // 1 个子矩形
		0x10, 0x00, // 坐标(1,0) 大小1x1
	}
	addr := startMockVNCEncoded(t, 2, 2, encHextile, hextile)
	vc := dialVNC(t, addr)
	updated, err := vc.readServerMessage()
	if err != nil || !updated {
		t.Fatalf("readServerMessage: updated=%v err=%v", updated, err)
	}
	// (0,0) 背景红，(1,0) 前景蓝，(0,1) 红，(1,1) 红
	checkRGBA(t, vc, 0, 0, 255, 0, 0) // 红
	checkRGBA(t, vc, 1, 0, 0, 0, 255) // 蓝
	checkRGBA(t, vc, 0, 1, 255, 0, 0) // 红
	checkRGBA(t, vc, 1, 1, 255, 0, 0) // 红
}

// TestVNCZRLEEndToEnd 端到端：mock 服务器用 ZRLE（solid）发送 framebuffer。
func TestVNCZRLEEndToEnd(t *testing.T) {
	// 2x1 framebuffer，ZRLE solid 红色。
	decomp := []byte{0x01, 0x00, 0xFF, 0x00, 0x00} // sub=solid, 颜色红
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	zw.Write(decomp)
	zw.Close()

	zrle := make([]byte, 4+zbuf.Len())
	binary.BigEndian.PutUint32(zrle[0:4], uint32(zbuf.Len()))
	copy(zrle[4:], zbuf.Bytes())

	addr := startMockVNCEncoded(t, 2, 1, encZRLE, zrle)
	vc := dialVNC(t, addr)
	updated, err := vc.readServerMessage()
	if err != nil || !updated {
		t.Fatalf("readServerMessage: updated=%v err=%v", updated, err)
	}
	checkRGBA(t, vc, 0, 0, 255, 0, 0)
	checkRGBA(t, vc, 1, 0, 255, 0, 0)
}

// TestVNCTightEndToEnd 端到端：mock 服务器用 Tight（fill）发送 framebuffer。
func TestVNCTightEndToEnd(t *testing.T) {
	// 2x1 framebuffer，Tight 单 tile：fill 绿色。
	tight := []byte{0x80, 0x00, 0x00, 0xFF, 0x00} // ctrl=fill, 颜色绿
	addr := startMockVNCEncoded(t, 2, 1, encTight, tight)
	vc := dialVNC(t, addr)
	updated, err := vc.readServerMessage()
	if err != nil || !updated {
		t.Fatalf("readServerMessage: updated=%v err=%v", updated, err)
	}
	checkRGBA(t, vc, 0, 0, 0, 255, 0)
	checkRGBA(t, vc, 1, 0, 0, 255, 0)
}

func checkRGBA(t *testing.T, vc *vncClient, x, y int, r, g, b byte) {
	t.Helper()
	o := (y*vc.width + x) * 4
	if vc.fb[o] != r || vc.fb[o+1] != g || vc.fb[o+2] != b {
		t.Errorf("像素(%d,%d) = RGBA(%d,%d,%d), 期望 (%d,%d,%d)", x, y, vc.fb[o], vc.fb[o+1], vc.fb[o+2], r, g, b)
	}
}
