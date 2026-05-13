package shapefile

import (
	"testing"

	"github.com/jonas-p/go-shp"
)

func TestShapeToWKTReturnsRealWKT(t *testing.T) {
	t.Parallel()

	got, err := ShapeToWKT(&shp.Point{X: 116.4, Y: 39.9})
	if err != nil {
		t.Fatalf("ShapeToWKT() error = %v", err)
	}
	if got != "POINT (116.4 39.9)" {
		t.Fatalf("ShapeToWKT() = %q, want real WKT", got)
	}
}

func TestShapeToGeoJSONPreservesPolygonHole(t *testing.T) {
	t.Parallel()

	geometry, err := ShapeToGeoJSON(&shp.Polygon{
		Parts: []int32{0, 5},
		Points: []shp.Point{
			{X: 0, Y: 0},
			{X: 0, Y: 10},
			{X: 10, Y: 10},
			{X: 10, Y: 0},
			{X: 0, Y: 0},
			{X: 2, Y: 2},
			{X: 8, Y: 2},
			{X: 8, Y: 8},
			{X: 2, Y: 8},
			{X: 2, Y: 2},
		},
	})
	if err != nil {
		t.Fatalf("ShapeToGeoJSON() error = %v", err)
	}
	if geometry["type"] != "Polygon" {
		t.Fatalf("geometry type = %v, want Polygon", geometry["type"])
	}
	coords, ok := geometry["coordinates"].([][][]float64)
	if !ok {
		t.Fatalf("coordinates type = %T", geometry["coordinates"])
	}
	if len(coords) != 2 {
		t.Fatalf("ring count = %d, want outer ring plus hole", len(coords))
	}
}

func TestShapeToGeoJSONBuildsMultiPolygonForMultipleOuterRings(t *testing.T) {
	t.Parallel()

	geometry, err := ShapeToGeoJSON(&shp.Polygon{
		Parts: []int32{0, 5},
		Points: []shp.Point{
			{X: 0, Y: 0},
			{X: 0, Y: 1},
			{X: 1, Y: 1},
			{X: 1, Y: 0},
			{X: 0, Y: 0},
			{X: 10, Y: 10},
			{X: 10, Y: 11},
			{X: 11, Y: 11},
			{X: 11, Y: 10},
			{X: 10, Y: 10},
		},
	})
	if err != nil {
		t.Fatalf("ShapeToGeoJSON() error = %v", err)
	}
	if geometry["type"] != "MultiPolygon" {
		t.Fatalf("geometry type = %v, want MultiPolygon", geometry["type"])
	}
	coords, ok := geometry["coordinates"].([][][][]float64)
	if !ok {
		t.Fatalf("coordinates type = %T", geometry["coordinates"])
	}
	if len(coords) != 2 {
		t.Fatalf("polygon count = %d, want 2", len(coords))
	}
}
