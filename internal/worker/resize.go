package worker

import (
	"bytes"
	"fmt"
	"io"

	"github.com/disintegration/imaging"
)

// Resize decodes an image from r, resizes it to a square of size×size
// (keeping aspect ratio, filling the box), and encodes it as JPEG.
func Resize(r io.Reader, size int) ([]byte, error) {
	src, err := imaging.Decode(r, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if size <= 0 {
		return nil, fmt.Errorf("invalid size: %d", size)
	}
	// Fill crops to a square, then resizes — so a 1920x1080 becomes 100x100, not 100x56.
	thumb := imaging.Fill(src, size, size, imaging.Center, imaging.Lanczos)

	var buf bytes.Buffer
	if err := imaging.Encode(&buf, thumb, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
		return nil, fmt.Errorf("encode image: %w", err)
	}
	return buf.Bytes(), nil
}
