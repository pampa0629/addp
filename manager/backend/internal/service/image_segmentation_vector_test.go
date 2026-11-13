package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"github.com/addp/common/inference"
)

func TestSegmentVectorsPreservesInstancesAndHoles(t *testing.T) {
	mask := image.NewGray(image.Rect(0, 0, 4, 4))
	for _, p := range []image.Point{
		{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0},
		{X: 0, Y: 1}, {X: 2, Y: 1},
		{X: 0, Y: 2}, {X: 1, Y: 2}, {X: 2, Y: 2}, {X: 3, Y: 3},
	} {
		mask.SetGray(p.X, p.Y, color.Gray{Y: 255})
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, mask); err != nil {
		t.Fatal(err)
	}
	prediction := inference.InstanceMask{X: 1, Y: 2, Width: 4, Height: 4, Score: 0.75, PNG: base64.StdEncoding.EncodeToString(pngData.Bytes())}
	geojson, count, err := segmentVectors([]inference.InstanceMask{prediction, prediction},
		segmentGeoReference{[6]float64{-122, 0.01, 0, 38, 0, -0.02}, 4326},
		ImageWindow{X: 4, Y: 6, Width: 16, Height: 8}, 8, 8)
	if err != nil || count != 2 {
		t.Fatalf("count=%d, error=%v", count, err)
	}
	var result struct {
		Features []segmentVectorFeature `json:"features"`
	}
	if err := json.Unmarshal(geojson, &result); err != nil {
		t.Fatal(err)
	}
	for i, feature := range result.Features {
		p := feature.Geometry.Coordinates
		if len(p) != 2 || len(p[0]) != 2 || len(p[1]) != 1 || feature.Properties.InstanceID != i+1 || feature.Properties.Score != prediction.Score {
			t.Fatalf("unexpected feature: %#v", feature)
		}
		wantCorner := [2]float64{-121.94, 37.84}
		if got := p[0][0][0]; math.Abs(got[0]-wantCorner[0]) > 1e-10 || math.Abs(got[1]-wantCorner[1]) > 1e-10 {
			t.Fatalf("source corner=%v want %v", got, wantCorner)
		}
	}
	prediction.X = 5
	if _, _, err := segmentVectors([]inference.InstanceMask{prediction},
		segmentGeoReference{[6]float64{0, 1, 0, 0, 0, -1}, 4326}, ImageWindow{Width: 8, Height: 8}, 8, 8); err == nil {
		t.Fatal("out-of-bounds model mask was accepted")
	}
}
