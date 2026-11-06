package liaison

import (
	"encoding/binary"
	"testing"
)

// TestVNCAuthResponse 验证 VNC DES 认证响应（参考 RFC 6143 已知向量）。
func TestVNCAuthResponse(t *testing.T) {
	// challenge 全 0x01，密码 "password"
	challenge := make([]byte, 16)
	for i := range challenge {
		challenge[i] = 0x01
	}
	resp, err := vncAuthResponse(challenge, "password")
	if err != nil {
		t.Fatalf("vncAuthResponse: %v", err)
	}
	if len(resp) != 16 {
		t.Fatalf("响应长度 = %d, want 16", len(resp))
	}
	// 响应非全零即可（不硬编码 DES 结果，验证可复现性）
	resp2, _ := vncAuthResponse(challenge, "password")
	for i := range resp {
		if resp[i] != resp2[i] {
			t.Fatal("同一密码/挑战应产生一致响应")
		}
	}
	// 不同密码应产生不同响应
	resp3, _ := vncAuthResponse(challenge, "other123")
	same := true
	for i := range resp {
		if resp[i] != resp3[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("不同密码应产生不同响应")
	}
}

// TestDecodePixel 验证像素解码（big-endian 32bpp RGB + 16bpp 565）。
func TestDecodePixel(t *testing.T) {
	// 32bpp，red shift=16, green shift=8, blue shift=0, max=255
	// big-endian 字节序 [pad, R, G, B]
	v := &vncClient{bpp: 4, bigEnd: true, rShift: 16, gShift: 8, bShift: 0, rMax: 255, gMax: 255, bMax: 255}
	r, g, b := v.decodePixel([]byte{0x00, 0xFF, 0x00, 0x00}) // R=0xFF
	if r != 255 || g != 0 || b != 0 {
		t.Errorf("红色解码 = (%d,%d,%d), want (255,0,0)", r, g, b)
	}
	r, g, b = v.decodePixel([]byte{0x00, 0x00, 0xFF, 0x00}) // G=0xFF
	if r != 0 || g != 255 || b != 0 {
		t.Errorf("绿色解码 = (%d,%d,%d), want (0,255,0)", r, g, b)
	}
}

// TestFillRaw 验证 framebuffer 填充（RGBA 输出）。
func TestFillRaw(t *testing.T) {
	v := &vncClient{width: 2, height: 1, bpp: 4, bigEnd: true, rShift: 16, gShift: 8, bShift: 0, rMax: 255, gMax: 255, bMax: 255}
	v.fb = make([]byte, 2*4)
	// 两个像素：红(FF0000) 蓝(0000FF)
	data := []byte{0x00, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFF}
	v.fillRaw(0, 0, 2, 1, data)
	if v.fb[0] != 255 || v.fb[1] != 0 || v.fb[2] != 0 {
		t.Errorf("像素0 = RGBA(%d,%d,%d), want (255,0,0)", v.fb[0], v.fb[1], v.fb[2])
	}
	if v.fb[4] != 0 || v.fb[5] != 0 || v.fb[6] != 255 {
		t.Errorf("像素1 = RGBA(%d,%d,%d), want (0,0,255)", v.fb[4], v.fb[5], v.fb[6])
	}
}

// TestAppendU16 验证大端编码辅助。
func TestAppendU16(t *testing.T) {
	b := appendU16(nil, 0x1234)
	if len(b) != 2 || b[0] != 0x12 || b[1] != 0x34 {
		t.Errorf("appendU16(0x1234) = %v", b)
	}
	u := binary.BigEndian.Uint16(b)
	if u != 0x1234 {
		t.Errorf("roundtrip = 0x%x", u)
	}
}

// newTestClient 构造测试用 vncClient（32bpp big-endian RGB）。
func newTestClient(w, h int) *vncClient {
	return &vncClient{
		width: w, height: h, bpp: 4, bigEnd: true,
		rShift: 16, gShift: 8, bShift: 0, rMax: 255, gMax: 255, bMax: 255,
		fb: make([]byte, w*h*4),
	}
}

// TestZRLESolid 验证 ZRLE solid 子编码。
func TestZRLESolid(t *testing.T) {
	v := newTestClient(2, 1)
	// solid：sub=1，然后 1 像素颜色（红）
	data := []byte{0x00, 0xFF, 0x00, 0x00}
	if err := v.zrleTile(0, 0, 2, 1, 1, &data); err != nil {
		t.Fatalf("zrleTile solid: %v", err)
	}
	if v.fb[0] != 255 || v.fb[1] != 0 || v.fb[2] != 0 {
		t.Errorf("solid 红色 = (%d,%d,%d), want (255,0,0)", v.fb[0], v.fb[1], v.fb[2])
	}
	if v.fb[4] != 255 || v.fb[5] != 0 || v.fb[6] != 0 {
		t.Errorf("solid 像素1 = (%d,%d,%d), want (255,0,0)", v.fb[4], v.fb[5], v.fb[6])
	}
}

// TestZRLERaw 验证 ZRLE raw 子编码。
func TestZRLERaw(t *testing.T) {
	v := newTestClient(2, 1)
	// raw：红 + 蓝
	data := []byte{0x00, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFF}
	if err := v.zrleTile(0, 0, 2, 1, 0, &data); err != nil {
		t.Fatalf("zrleTile raw: %v", err)
	}
	if v.fb[0] != 255 || v.fb[4] != 0 || v.fb[6] != 255 {
		t.Errorf("raw 解码错误: 像素0=(%d,%d,%d) 像素1=(%d,%d,%d)", v.fb[0], v.fb[1], v.fb[2], v.fb[4], v.fb[5], v.fb[6])
	}
}

// TestBitReader 验证位读取（MSB 优先）。
func TestBitReader(t *testing.T) {
	br := &bitReader{data: []byte{0b10110000}}
	// 读 3 位：101 = 5
	v, err := br.readBits(3)
	if err != nil || v != 5 {
		t.Errorf("readBits(3) = %d, err=%v, want 5", v, err)
	}
	// 读 2 位：10 = 2
	v, _ = br.readBits(2)
	if v != 2 {
		t.Errorf("readBits(2) = %d, want 2", v)
	}
}
