package writers

import (
	"math"
	"testing"

	"github.com/jonas-p/go-shp"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/wkb"
)

func TestToShapefileGeometryMultiPolygon(t *testing.T) {
	wktValue := "MULTIPOLYGON(((0 0,0 4,4 4,4 0,0 0),(1 1,2 1,2 2,1 2,1 1)),((10 10,10 14,14 14,14 10,10 10)))"
	shape, err := toShapefileGeometry(wktValue, shp.POLYGON)
	if err != nil {
		t.Fatalf("toShapefileGeometry failed: %v", err)
	}

	polygon, ok := shape.(*shp.Polygon)
	if !ok {
		t.Fatalf("expected Shapefile polygon, got %T", shape)
	}

	if polygon.NumParts != 3 {
		t.Fatalf("expected 3 parts (outer, hole, outer2), got %d", polygon.NumParts)
	}

	// Validate that first part (outer ring) orientation is clockwise (negative area)
	firstPart := polygon.Points[:polygon.Parts[1]]
	if area := signedAreaPoints(firstPart); area >= 0 {
		t.Fatalf("expected first ring to be clockwise, got area=%f", area)
	}

	// Validate that second part (hole) orientation is counter-clockwise
	secondPart := polygon.Points[polygon.Parts[1]:polygon.Parts[2]]
	if area := signedAreaPoints(secondPart); area <= 0 {
		t.Fatalf("expected second ring (hole) to be counter-clockwise, got area=%f", area)
	}
}

func TestPrepareValueGeometryFromWKT(t *testing.T) {
	writer := &JDBCWriter{
		driver: "postgres",
		geometryColumns: map[string]geometryColumnMeta{
			"geom": {SRID: 4326},
		},
	}

	data, err := writer.prepareValue("geom", "POINT(1 2)")
	if err != nil {
		t.Fatalf("prepareValue failed: %v", err)
	}

	bytes, ok := data.([]byte)
	if !ok || len(bytes) == 0 {
		t.Fatalf("expected []byte WKB, got %T", data)
	}

	geometry, err := wkb.Unmarshal(bytes)
	if err != nil {
		t.Fatalf("failed to parse WKB: %v", err)
	}

	point, ok := geometry.(*geom.Point)
	if !ok {
		t.Fatalf("expected geom.Point, got %T", geometry)
	}

	if !almostEqualFloat(point.X(), 1) || !almostEqualFloat(point.Y(), 2) {
		t.Fatalf("unexpected point coordinates: (%f, %f)", point.X(), point.Y())
	}
}

func signedAreaPoints(points []shp.Point) float64 {
	if len(points) < 3 {
		return 0
	}
	sum := 0.0
	for i := 0; i < len(points)-1; i++ {
		x1, y1 := points[i].X, points[i].Y
		x2, y2 := points[i+1].X, points[i+1].Y
		sum += (x1 * y2) - (x2 * y1)
	}
	return sum / 2
}

func almostEqualFloat(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9
}
