//go:build !windows

package agent

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestCommandScreenPNG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frame.png")
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	img.Set(3, 4, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	file.Close()

	screen, err := newCommandScreen("cat " + path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := screen.Capture()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got.Bounds().Dx() != 32 || got.Bounds().Dy() != 24 {
		t.Fatalf("unexpected bounds: %v", got.Bounds())
	}
	r, g, b, _ := got.At(3, 4).RGBA()
	if r>>8 != 10 || g>>8 != 20 || b>>8 != 30 {
		t.Fatalf("unexpected pixel: %d,%d,%d", r>>8, g>>8, b>>8)
	}
}

func TestCommandScreenBadOutput(t *testing.T) {
	screen, err := newCommandScreen("printf not-an-image")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := screen.Capture(); err == nil {
		t.Fatal("invalid output accepted")
	}
}
