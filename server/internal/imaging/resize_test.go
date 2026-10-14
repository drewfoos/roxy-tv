package imaging

import (
	"image"
	"image/color"
	"testing"
)

func TestResizeBoxAverages(t *testing.T) {
	// 4x2: left half white, right half black. Shrinking to 2x1 must average each half.
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			c := color.RGBA{255, 255, 255, 255}
			if x >= 2 {
				c = color.RGBA{0, 0, 0, 255}
			}
			src.SetRGBA(x, y, c)
		}
	}
	out := Resize(src, 2)
	if b := out.Bounds(); b.Dx() != 2 || b.Dy() != 1 {
		t.Fatalf("bounds = %v, want 2x1", b)
	}
	if r, _, _, a := out.At(0, 0).RGBA(); r>>8 != 255 || a>>8 != 255 {
		t.Errorf("left pixel = %d, want white", r>>8)
	}
	if r, _, _, _ := out.At(1, 0).RGBA(); r>>8 != 0 {
		t.Errorf("right pixel = %d, want black", r>>8)
	}

	// A 2x2 checkerboard shrunk to 1x1 is mid grey.
	chk := image.NewRGBA(image.Rect(0, 0, 2, 2))
	chk.SetRGBA(0, 0, color.RGBA{255, 255, 255, 255})
	chk.SetRGBA(1, 1, color.RGBA{255, 255, 255, 255})
	chk.SetRGBA(1, 0, color.RGBA{0, 0, 0, 255})
	chk.SetRGBA(0, 1, color.RGBA{0, 0, 0, 255})
	if r, _, _, _ := Resize(chk, 1).At(0, 0).RGBA(); r>>8 < 126 || r>>8 > 128 {
		t.Errorf("checkerboard mean = %d, want ~127", r>>8)
	}
}

func TestResizeKeepsAspectAndNeverEnlarges(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1000, 1500))
	out := Resize(src, 300)
	if b := out.Bounds(); b.Dx() != 300 || b.Dy() != 450 {
		t.Fatalf("bounds = %v, want 300x450", b)
	}
	if Resize(src, 1000) != src || Resize(src, 4000) != src || Resize(src, 0) != src {
		t.Error("expected the source back when not shrinking")
	}
	// Non-zero origin rectangles (sub-images) must be handled.
	sub := src.SubImage(image.Rect(100, 100, 500, 300))
	if b := Resize(sub, 100).Bounds(); b.Dx() != 100 || b.Dy() != 50 {
		t.Errorf("sub-image bounds = %v, want 100x50", b)
	}
}
