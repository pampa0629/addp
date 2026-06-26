package spatial

import (
	"testing"

	"github.com/twpayne/go-geom"
)

func TestGeometryBatchArrowRoundTrip(t *testing.T) {
	source := [][]byte{
		mustEWKB(t, geom.NewPointFlat(geom.XY, []float64{1, 2}), 3857),
		nil,
		mustEWKB(t, geom.NewPointFlat(geom.XY, []float64{3, 4}), 3857),
	}

	payload, err := EncodeGeometryBatchArrow(source, GeometryBatchArrowOptions{
		GeometryColumn:   "geom",
		GeometryEncoding: GeometryBatchArrowEncodingEWKB,
		SourceCRS:        "EPSG:3857",
		TargetCRS:        "EPSG:4326",
	})
	if err != nil {
		t.Fatalf("EncodeGeometryBatchArrow returned error: %v", err)
	}

	decoded, err := DecodeGeometryBatchArrow(payload)
	if err != nil {
		t.Fatalf("DecodeGeometryBatchArrow returned error: %v", err)
	}
	if decoded.GeometryColumn != "geom" {
		t.Fatalf("geometry column = %q, want geom", decoded.GeometryColumn)
	}
	if decoded.GeometryEncoding != GeometryBatchArrowEncodingEWKB {
		t.Fatalf("geometry encoding = %q, want ewkb", decoded.GeometryEncoding)
	}
	if decoded.SourceCRS != "EPSG:3857" || decoded.TargetCRS != "EPSG:4326" {
		t.Fatalf("CRS metadata = %#v, want source/target CRS", decoded)
	}
	if len(decoded.Geometries) != len(source) {
		t.Fatalf("geometry count = %d, want %d", len(decoded.Geometries), len(source))
	}
}

func mustEWKB(t *testing.T, point geom.T, srid int) []byte {
	t.Helper()
	data, err := GeomToEWKB(point, srid)
	if err != nil {
		t.Fatalf("marshal ewkb: %v", err)
	}
	return data
}
