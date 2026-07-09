package imageproc

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestSquareIconPNGResizesAndSquares(t *testing.T) {
	src := makePNG(t, 200, 120) // non-square
	out, err := SquareIconPNG(src, 128)
	if err != nil {
		t.Fatalf("SquareIconPNG: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode out: %v", err)
	}
	if cfg.Width != 128 || cfg.Height != 128 {
		t.Fatalf("expected 128x128, got %dx%d", cfg.Width, cfg.Height)
	}
}

func TestSquareIconPNGRejectsGarbage(t *testing.T) {
	if _, err := SquareIconPNG([]byte("not an image"), 128); err == nil {
		t.Fatal("expected error for non-image input")
	}
}
