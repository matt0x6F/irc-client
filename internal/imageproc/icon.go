// Package imageproc processes user-uploaded network icons into a normalized,
// square PNG suitable for the side-rail tiles.
package imageproc

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	_ "image/gif" // register decoders
	_ "image/jpeg"

	xdraw "golang.org/x/image/draw"
)

// SquareIconPNG decodes an uploaded image (PNG/JPEG/GIF), center-crops it to a
// square, scales it to size×size with high-quality resampling, and returns PNG
// bytes. It returns an error for undecodable input.
func SquareIconPNG(src []byte, size int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	side := w
	if h < side {
		side = h
	}
	// Center crop rect.
	ox := b.Min.X + (w-side)/2
	oy := b.Min.Y + (h-side)/2
	cropRect := image.Rect(ox, oy, ox+side, oy+side)

	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, cropRect, xdraw.Over, nil)

	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return out.Bytes(), nil
}
