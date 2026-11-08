package service

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestSegmentWindowUsesSourcePixelCoordinates(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 2048, 64))
	for y := 0; y < 64; y++ {
		for x := 1024; x < 2048; x++ {
			im.Set(x, y, color.White)
		}
	}
	var src bytes.Buffer
	if err := png.Encode(&src, im); err != nil {
		t.Fatal(err)
	}
	want := ImageWindow{1024, 5, 512, 20}
	data, w, err := renderSegmentWindow(bytes.NewReader(src.Bytes()), "png", &want)
	if err != nil || w != want {
		t.Fatal(w, err)
	}
	result, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if result.Bounds().Dx() != 512 || result.Bounds().Dy() != 20 || color.GrayModel.Convert(result.At(0, 0)).(color.Gray).Y != 255 {
		t.Fatal("window not in original pixel space")
	}
	bad := ImageWindow{2040, 0, 32, 15}
	if _, _, err := renderSegmentWindow(bytes.NewReader(src.Bytes()), "png", &bad); !errors.Is(err, ErrSegmentImageUnsupported) {
		t.Fatal(err)
	}
}
