// Package imaging holds the small image helpers the server needs (poster resizing)
// without pulling in an image library.
package imaging

import (
	"image"
	"image/color"
)

// Resize scales img to width w (height follows the aspect ratio) with a box filter:
// each output pixel is the mean of the source pixels it covers. Images are never
// enlarged; a w at or above the source width returns the source unchanged.
func Resize(img image.Image, w int) image.Image {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if w <= 0 || sw == 0 || sh == 0 || w >= sw {
		return img
	}
	h := (sh*w + sw/2) / sw
	if h < 1 {
		h = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for dy := 0; dy < h; dy++ {
		y0, y1 := dy*sh/h, (dy+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < w; dx++ {
			x0, x1 := dx*sw/w, (dx+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					cr, cg, cb, ca := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
					r += uint64(cr)
					g += uint64(cg)
					bl += uint64(cb)
					a += uint64(ca)
					n++
				}
			}
			dst.SetRGBA(dx, dy, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), uint8(a / n >> 8)})
		}
	}
	return dst
}
