package agent

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestChunkFrameRoundTrip(t *testing.T) {
	jpeg := bytes.Repeat([]byte{0x5a}, frameChunkPayload*2+17)
	chunks := chunkFrame(42, 1280, 720, jpeg)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		seq, index, total, w, h, ok := frameHeader(chunk)
		if !ok {
			t.Fatalf("chunk %d header invalid", i)
		}
		if seq != 42 || index != i || total != 3 || w != 1280 || h != 720 {
			t.Fatalf("chunk %d header mismatch: seq=%d index=%d total=%d %dx%d", i, seq, index, total, w, h)
		}
		if len(chunk) > frameHeaderSize+frameChunkPayload {
			t.Fatalf("chunk %d too large: %d", i, len(chunk))
		}
	}
	got, err := reassembleFrame(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, jpeg) {
		t.Fatal("reassembled frame differs")
	}
}

func TestChunkFrameEmpty(t *testing.T) {
	chunks := chunkFrame(1, 16, 16, nil)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	got, err := reassembleFrame(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty payload, got %d", len(got))
	}
}

func TestFrameHeaderRejectsGarbage(t *testing.T) {
	if _, _, _, _, _, ok := frameHeader([]byte("not a frame")); ok {
		t.Fatal("garbage accepted")
	}
	bad := chunkFrame(1, 10, 10, []byte("x"))[0]
	bad[0] = 'X'
	if _, _, _, _, _, ok := frameHeader(bad); ok {
		t.Fatal("bad magic accepted")
	}
}

func TestEncodeJPEGScalesDown(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	data, w, h, err := encodeJPEG(img, 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	if w != 100 || h != 50 {
		t.Fatalf("unexpected size %dx%d", w, h)
	}
	if len(data) < 100 {
		t.Fatalf("jpeg too small: %d", len(data))
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 100 || decoded.Bounds().Dy() != 50 {
		t.Fatalf("decoded size mismatch: %v", decoded.Bounds())
	}
}

func TestNormalizeSignalURL(t *testing.T) {
	cases := map[string]string{
		"https://iflygo.example.com":           "wss://iflygo.example.com/crossdesk/signal",
		"http://127.0.0.1:8080":                "ws://127.0.0.1:8080/crossdesk/signal",
		"wss://iflygo.example.com/custom/path": "wss://iflygo.example.com/custom/path",
		"wss://iflygo.example.com/":            "wss://iflygo.example.com/crossdesk/signal",
		"ws://127.0.0.1:8080":                  "ws://127.0.0.1:8080/crossdesk/signal",
		"https://iflygo.example.com/":          "wss://iflygo.example.com/crossdesk/signal",
	}
	for in, want := range cases {
		got, err := normalizeSignalURL(in, "/crossdesk/signal")
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got != want {
			t.Fatalf("%s: got %s want %s", in, got, want)
		}
	}
	if _, err := normalizeSignalURL("", "/crossdesk/signal"); err == nil {
		t.Fatal("empty server accepted")
	}
}

func TestWheelSteps(t *testing.T) {
	cases := map[int]int{
		0: 0, 1: 1, -1: -1, 100: 1, -100: -1, 250: 2, -250: -2, 55: 1, 5000: 10, -5000: -10,
	}
	for in, want := range cases {
		if got := wheelSteps(in); got != want {
			t.Fatalf("wheelSteps(%d)=%d want %d", in, got, want)
		}
	}
}

func TestVKToKeysym(t *testing.T) {
	cases := map[int]uint32{
		'A':    0x61,
		'Z':    0x7a,
		'0':    0x30,
		13:     0xff0d,
		27:     0xff1b,
		37:     0xff51,
		40:     0xff54,
		112:    0xffbe,
		46:     0xffff,
		186:    0x3b,
		999999: 0,
	}
	for vk, want := range cases {
		if got := vkToKeysym(vk); got != want {
			t.Fatalf("vk %d: got 0x%x want 0x%x", vk, got, want)
		}
	}
}
