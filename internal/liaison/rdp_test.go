package liaison

import (
	"encoding/binary"
	"testing"

	grdpclient "github.com/tomatome/grdp/client"
)

// TestRDPBitmapToRect 验证 grdp Bitmap（BGR）→ RGBA rect 转换。
func TestRDPBitmapToRect(t *testing.T) {
	// 32bpp BGR：红(0,0,255) 蓝(255,0,0)
	b := grdpclient.Bitmap{
		DestLeft: 0, DestTop: 0, Width: 2, Height: 1,
		BitsPerPixel: 32,
		Data:         []byte{0x00, 0x00, 0xFF, 0x00, 0xFF, 0x00, 0x00, 0x00}, // B,G,R,A
	}
	rect := rdpBitmapToRect(b)
	if rect == nil {
		t.Fatal("rect 为 nil")
	}
	// 头 8 字节
	if binary.BigEndian.Uint16(rect[4:6]) != 2 || binary.BigEndian.Uint16(rect[6:8]) != 1 {
		t.Errorf("rect 尺寸 = %dx%d, want 2x1", binary.BigEndian.Uint16(rect[4:6]), binary.BigEndian.Uint16(rect[6:8]))
	}
	// 像素0 = 红 RGBA(255,0,0)
	if rect[8] != 255 || rect[9] != 0 || rect[10] != 0 {
		t.Errorf("像素0 = RGBA(%d,%d,%d), want (255,0,0)", rect[8], rect[9], rect[10])
	}
	// 像素1 = 蓝 RGBA(0,0,255)
	if rect[12] != 0 || rect[13] != 0 || rect[14] != 255 {
		t.Errorf("像素1 = RGBA(%d,%d,%d), want (0,0,255)", rect[12], rect[13], rect[14])
	}
}

// TestRDPBitmapToRect16 验证 16bpp RGB565 转换。
func TestRDPBitmapToRect16(t *testing.T) {
	// 红色 RGB565 = 0xF800（little-endian：0x00, 0xF8）
	b := grdpclient.Bitmap{
		Width: 1, Height: 1, BitsPerPixel: 16,
		Data: []byte{0x00, 0xF8},
	}
	rect := rdpBitmapToRect(b)
	if rect == nil {
		t.Fatal("rect 为 nil")
	}
	// 红色：R=255, G=0, B=0（近似）
	if rect[8] < 240 || rect[9] != 0 || rect[10] != 0 {
		t.Errorf("像素 = RGBA(%d,%d,%d), 期望红色", rect[8], rect[9], rect[10])
	}
}
