package shapefile

import (
	"testing"

	"github.com/addp/common/format"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/jonas-p/go-shp"
)

func TestShapeToWKTReturnsRealWKT(t *testing.T) {
	t.Parallel()

	got, err := shapeToWKT(&shp.Point{X: 116.4, Y: 39.9})
	if err != nil {
		t.Fatalf("shapeToWKT() error = %v", err)
	}
	if got != "POINT (116.4 39.9)" {
		t.Fatalf("shapeToWKT() = %q, want real WKT", got)
	}
}

func TestShapeToGeoJSONPreservesPolygonHole(t *testing.T) {
	t.Parallel()

	geometry, err := shapeToGeoJSON(&shp.Polygon{
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
		t.Fatalf("shapeToGeoJSON() error = %v", err)
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

	geometry, err := shapeToGeoJSON(&shp.Polygon{
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
		t.Fatalf("shapeToGeoJSON() error = %v", err)
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

func TestShapeToRowValueSupportsGeometryEncoding(t *testing.T) {
	t.Parallel()

	value, err := shapeToRowValue(&shp.Point{X: 116.4, Y: 39.9}, &format.ParseOptions{
		GeometryEncoding: format.GeometryEncodingEWKB,
	}, 4326)
	if err != nil {
		t.Fatalf("shapeToRowValue() error = %v", err)
	}
	data, ok := value.([]byte)
	if !ok {
		t.Fatalf("geometry value type = %T, want []byte", value)
	}
	geometry, err := commonSpatial.ParseGeometryValue(data)
	if err != nil {
		t.Fatalf("ParseGeometryValue() error = %v", err)
	}
	if got := geometry.SRID(); got != 4326 {
		t.Fatalf("SRID = %d, want 4326", got)
	}
}
