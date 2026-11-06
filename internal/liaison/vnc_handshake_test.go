package liaison

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
)

// startMockVNC 启动一个极简 VNC 服务器（NoAuth + Raw 编码单像素）。
func startMockVNC(t *testing.T) (addr string) {
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
		// 1. 版本
		c.Write([]byte("RFB 003.008\n"))
		buf := make([]byte, 12)
		io.ReadFull(c, buf) // 客户端版本
		// 2. 安全类型 None
		c.Write([]byte{1, 1})
		b := make([]byte, 1)
		io.ReadFull(c, b) // 选择 None
		// 3. SecurityResult 成功
		c.Write([]byte{0, 0, 0, 0})
		io.ReadFull(c, b) // ClientInit
		// 4. ServerInit：2x1 32bpp RGB
		init := make([]byte, 0, 24)
		init = append(init, 0, 2) // width=2
		init = append(init, 0, 1) // height=1
		// pixel format: 32bpp, 24depth, big-endian, true-color, max=255, shift R16 G8 B0
		pf := []byte{32, 24, 1, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0}
		init = append(init, pf...)
		name := []byte("mock")
		init = append(init, 0, 0, 0, byte(len(name)))
		init = append(init, name...)
		c.Write(init)
		// 5. 读 SetEncodings（忽略）
		hdr := make([]byte, 4)
		io.ReadFull(c, hdr)
		n := binary.BigEndian.Uint16(hdr[2:4])
		io.ReadFull(c, make([]byte, n*4))
		// 6. 读 FramebufferUpdateRequest（忽略）
		io.ReadFull(c, make([]byte, 10))
		// 7. FramebufferUpdate：1 个 Raw rect（红 + 蓝）
		fb := make([]byte, 0, 20)
		fb = append(fb, 0, 0)                   // type + padding
		fb = append(fb, 0, 1)                   // nRect=1
		fb = append(fb, 0, 0, 0, 0, 0, 2, 0, 1) // x,y,w,h
		fb = append(fb, 0, 0, 0, 0)             // encoding=Raw
		// 像素：红(00 FF 00 00) 蓝(00 00 00 FF)
		fb = append(fb, 0x00, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFF)
		c.Write(fb)
		// 保持连接直到测试结束
		<-make(chan struct{})
	}()

	return ln.Addr().String()
}

func TestVNCClientHandshakeAndFramebuffer(t *testing.T) {
	addr := startMockVNC(t)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	vc, err := vncHandshake(host, port, "")
	if err != nil {
		t.Fatalf("vncHandshake: %v", err)
	}
	defer vc.c.Close()

	if vc.width != 2 || vc.height != 1 {
		t.Errorf("framebuffer = %dx%d, want 2x1", vc.width, vc.height)
	}
	if vc.name != "mock" {
		t.Errorf("name = %q, want mock", vc.name)
	}

	// 读取 framebuffer 更新
	updated, err := vc.readServerMessage()
	if err != nil {
		t.Fatalf("readServerMessage: %v", err)
	}
	if !updated {
		t.Fatal("应收到 framebuffer 更新")
	}
	// 像素0 = 红，像素1 = 蓝
	if vc.fb[0] != 255 || vc.fb[1] != 0 || vc.fb[2] != 0 {
		t.Errorf("像素0 = (%d,%d,%d), want (255,0,0)", vc.fb[0], vc.fb[1], vc.fb[2])
	}
	if vc.fb[4] != 0 || vc.fb[5] != 0 || vc.fb[6] != 255 {
		t.Errorf("像素1 = (%d,%d,%d), want (0,0,255)", vc.fb[4], vc.fb[5], vc.fb[6])
	}
}
