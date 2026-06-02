package image

import (
	"bytes"
	"context"
	stdimage "image"
	"image/color"
	"image/png"
	"testing"

	"github.com/addp/common/format"
)

func TestImageMediaInfoProviderDescribePNG(t *testing.T) {
	var buf bytes.Buffer
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	provider, err := format.GetMediaInfoProvider(format.FormatPNG)
	if err != nil {
		t.Fatalf("GetMediaInfoProvider(png) error = %v", err)
	}
	info, err := provider.DescribeMedia(context.Background(), bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("DescribeMedia() error = %v", err)
	}
	if info.Media == nil {
		t.Fatal("media info missing")
	}
	if info.Media.Kind != "image" {
		t.Fatalf("Kind = %q, want image", info.Media.Kind)
	}
	if info.Media.Width != 2 || info.Media.Height != 3 {
		t.Fatalf("size = %dx%d, want 2x3", info.Media.Width, info.Media.Height)
	}
	if info.Media.MIMEType != "image/png" {
		t.Fatalf("MIMEType = %q, want image/png", info.Media.MIMEType)
	}
}

func TestListImageMediaInfoProviders(t *testing.T) {
	for _, want := range []format.FormatType{format.FormatImage, format.FormatJPEG, format.FormatPNG, format.FormatGIF, format.FormatTIFF} {
		if _, err := format.GetMediaInfoProvider(want); err != nil {
			t.Fatalf("GetMediaInfoProvider(%s) error = %v", want, err)
		}
	}
}
