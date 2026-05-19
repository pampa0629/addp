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
	if info.Format != format.FormatPNG {
		t.Fatalf("Format = %q, want png", info.Format)
	}
	if info.MediaType != "image" {
		t.Fatalf("MediaType = %q, want image", info.MediaType)
	}
	if info.Width != 2 || info.Height != 3 {
		t.Fatalf("size = %dx%d, want 2x3", info.Width, info.Height)
	}
	if info.MIMEType != "image/png" {
		t.Fatalf("MIMEType = %q, want image/png", info.MIMEType)
	}
}

func TestListImageMediaInfoProviders(t *testing.T) {
	formats := format.ListMediaInfoProviderFormats()
	for _, want := range []format.FormatType{format.FormatImage, format.FormatJPEG, format.FormatPNG, format.FormatGIF, format.FormatTIFF} {
		if !containsFormat(formats, want) {
			t.Fatalf("ListMediaInfoProviderFormats() = %#v, want %s", formats, want)
		}
	}
}

func containsFormat(values []format.FormatType, target format.FormatType) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
