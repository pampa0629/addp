package service

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
)

// ImageWindow is expressed in full-resolution source pixels. A nil window selects the full image.
type ImageWindow struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

var ErrSegmentImageUnsupported = errors.New("unsupported analysis window or source encoding")

const maxSegmentSourceBytes = 64 << 20

func renderSegmentWindow(r io.Reader, expectedFormat string, wanted *ImageWindow) ([]byte, ImageWindow, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxSegmentSourceBytes+1))
	if err != nil {
		return nil, ImageWindow{}, err
	}
	if len(data) > maxSegmentSourceBytes {
		return nil, ImageWindow{}, ErrSegmentImageUnsupported
	}
	c, decodedFormat, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || decodedFormat != expectedFormat || c.Width <= 0 || c.Height <= 0 || c.Width > 32768 || c.Height > 32768 || int64(c.Width)*int64(c.Height) > 32<<20 {
		return nil, ImageWindow{}, ErrSegmentImageUnsupported
	}
	w := ImageWindow{0, 0, c.Width, c.Height}
	if wanted != nil {
		w = *wanted
	}
	if w.X < 0 || w.Y < 0 || w.Width < 1 || w.Height < 1 || w.X > c.Width-w.Width || w.Y > c.Height-w.Height {
		return nil, w, ErrSegmentImageUnsupported
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, w, ErrSegmentImageUnsupported
	}
	ratio := math.Min(1, 1024/float64(max(w.Width, w.Height)))
	dst := image.NewRGBA(image.Rect(0, 0, max(1, int(math.Round(float64(w.Width)*ratio))), max(1, int(math.Round(float64(w.Height)*ratio)))))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, image.Rect(w.X, w.Y, w.X+w.Width, w.Y+w.Height), draw.Src, nil)
	var buf bytes.Buffer
	// The bounded interactive window stays lossless without spending request time on DEFLATE.
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	err = encoder.Encode(&buf, dst)
	return buf.Bytes(), w, err
}
