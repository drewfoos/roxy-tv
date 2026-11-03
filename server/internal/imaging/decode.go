package imaging

import (
	"bytes"
	"errors"
	"fmt"
	"image"
)

// Limits for an image Roxy will decode. A tiny PNG can declare 50000x50000 pixels and
// would allocate ~10 GB when decoded, so the declared size is checked first.
const (
	MaxSide   = 8000
	MaxPixels = 40_000_000
)

// ErrTooLarge: the image declares more pixels than Roxy decodes.
var ErrTooLarge = errors.New("image too large")

// CheckConfig reads only the header and refuses images larger than the limits. It returns
// the config and format so callers need not read the header twice.
func CheckConfig(data []byte) (image.Config, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return cfg, format, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxSide || cfg.Height > MaxSide || cfg.Width*cfg.Height > MaxPixels {
		return cfg, format, fmt.Errorf("%w: %dx%d", ErrTooLarge, cfg.Width, cfg.Height)
	}
	return cfg, format, nil
}

// Decode is image.Decode after CheckConfig.
func Decode(data []byte) (image.Image, string, error) {
	if _, _, err := CheckConfig(data); err != nil {
		return nil, "", err
	}
	return image.Decode(bytes.NewReader(data))
}
