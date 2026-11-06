package liaison

import (
	"bytes"
	"compress/zlib"
	"testing"
)

// TestTightCrossTileZlib 验证 Tight 的跨 tile zlib 流：
// tile1=basic(zlib 像素) + tile2=fill，确保 zlib 精确读取不吞掉 fill 的 control 字节。
func TestTightCrossTileZlib(t *testing.T) {
	// 32x1 两个 16x16 tile：tile1(x0-15)=basic 红色，tile2(x16-31)=fill 绿色
	const (
		bpp   = 4
		width = 32
	)

	// 16 个红色像素（big-endian 32bpp RGB：byte1=R=FF）
	redPixels := make([]byte, 0, 16*bpp)
	for i := 0; i < 16; i++ {
		redPixels = append(redPixels, 0x00, 0xFF, 0x00, 0x00)
	}
	// zlib 压缩
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	zw.Write(redPixels)
	zw.Close()

	// Tight 数据流：tile1 ctrl=basic(0x00) + zlib；tile2 ctrl=fill(0x80) + 绿色
	data := append([]byte{0x00}, zbuf.Bytes()...)
	data = append(data, 0x80)                   // ctrl=fill
	data = append(data, 0x00, 0x00, 0xFF, 0x00) // 绿色（byte2=G=FF）

	// 用 bytes.Reader 作为底层 VNC 流
	br := &bufReader{r: bytes.NewReader(data)}
	v := &vncClient{
		r: br, width: width, height: 1, bpp: bpp, bigEnd: true,
		rShift: 16, gShift: 8, bShift: 0, rMax: 255, gMax: 255, bMax: 255,
		fb: make([]byte, width*4),
	}

	if err := v.readTight(0, 0, width, 1); err != nil {
		t.Fatalf("readTight: %v", err)
	}

	// 像素 0-15 应为红色
	for i := 0; i < 16; i++ {
		o := i * 4
		if v.fb[o] != 255 || v.fb[o+1] != 0 || v.fb[o+2] != 0 {
			t.Fatalf("像素%d = RGBA(%d,%d,%d), 期望红色（zlib 跨 tile 失败）", i, v.fb[o], v.fb[o+1], v.fb[o+2])
		}
	}
	// 像素 16-31 应为绿色（fill tile 的 control 字节未被 zlib 预读吞掉）
	for i := 16; i < 32; i++ {
		o := i * 4
		if v.fb[o] != 0 || v.fb[o+1] != 255 || v.fb[o+2] != 0 {
			t.Fatalf("像素%d = RGBA(%d,%d,%d), 期望绿色（fill control 字节被吞）", i, v.fb[o], v.fb[o+1], v.fb[o+2])
		}
	}
}

// TestTightBasicZlib 验证 Tight basic 子编码（单 tile 纯 zlib）。
func TestTightBasicZlib(t *testing.T) {
	// 4x1 单 tile（< 16x16），basic 蓝色（byte3=B=FF）
	const bpp = 4
	bluePixels := make([]byte, 0, 4*bpp)
	for i := 0; i < 4; i++ {
		bluePixels = append(bluePixels, 0x00, 0x00, 0x00, 0xFF) // B=FF
	}
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	zw.Write(bluePixels)
	zw.Close()

	data := append([]byte{0x00}, zbuf.Bytes()...) // ctrl=basic
	br := &bufReader{r: bytes.NewReader(data)}
	v := &vncClient{
		r: br, width: 4, height: 1, bpp: bpp, bigEnd: true,
		rShift: 16, gShift: 8, bShift: 0, rMax: 255, gMax: 255, bMax: 255,
		fb: make([]byte, 4*4),
	}
	if err := v.readTight(0, 0, 4, 1); err != nil {
		t.Fatalf("readTight: %v", err)
	}
	for i := 0; i < 4; i++ {
		o := i * 4
		if v.fb[o] != 0 || v.fb[o+1] != 0 || v.fb[o+2] != 255 {
			t.Fatalf("像素%d = RGBA(%d,%d,%d), 期望蓝色", i, v.fb[o], v.fb[o+1], v.fb[o+2])
		}
	}
}
