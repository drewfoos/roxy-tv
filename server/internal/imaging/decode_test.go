package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

// pngHeader is a PNG whose IHDR claims w x h; nothing after it, which DecodeConfig never reads.
func pngHeader(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func TestDecodeRefusesBombs(t *testing.T) {
	for _, wh := range [][2]uint32{{50000, 50000}, {9000, 10}, {7000, 7000}} {
		if _, _, err := Decode(pngHeader(wh[0], wh[1])); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%dx%d: want ErrTooLarge, got %v", wh[0], wh[1], err)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 20, 10))); err != nil {
		t.Fatal(err)
	}
	img, _, err := Decode(buf.Bytes())
	if err != nil || img.Bounds().Dx() != 20 {
		t.Fatalf("a small image must decode: %v", err)
	}
}
