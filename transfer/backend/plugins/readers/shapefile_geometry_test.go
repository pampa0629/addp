package readers

import (
	"math"
	"testing"

	"github.com/jonas-p/go-shp"
	"github.com/twpayne/go-geom"
)

func TestShapeToGeomMultiLineString(t *testing.T) {
	shape := &shp.PolyLine{
		NumParts:  2,
		NumPoints: 4,
		Parts:     []int32{0, 2},
		Points: []shp.Point{
			{X: 0, Y: 0},
			{X: 1, Y: 1},
			{X: 10, Y: 10},
			{X: 11, Y: 11},
		},
	}

	geomValue, err := shapeToGeom(shape)
	if err != nil {
		t.Fatalf("shapeToGeom failed: %v", err)
	}

	multi, ok := geomValue.(*geom.MultiLineString)
	if !ok {
		t.Fatalf("expected MultiLineString, got %T", geomValue)
	}
	if multi.NumLineStrings() != 2 {
		t.Fatalf("expected 2 parts, got %d", multi.NumLineStrings())
	}

	first := multi.LineString(0).Coords()
	if len(first) != 2 || !coordsEqual(first[0], geom.Coord{0, 0}) || !coordsEqual(first[1], geom.Coord{1, 1}) {
		t.Fatalf("unexpected first line coords: %v", first)
	}

	second := multi.LineString(1).Coords()
	if len(second) != 2 || !coordsEqual(second[0], geom.Coord{10, 10}) || !coordsEqual(second[1], geom.Coord{11, 11}) {
		t.Fatalf("unexpected second line coords: %v", second)
	}
}

func TestShapeToGeomPolygonWithHole(t *testing.T) {
	outer := []shp.Point{
		{X: 0, Y: 0},
		{X: 0, Y: 4},
		{X: 4, Y: 4},
		{X: 4, Y: 0},
		{X: 0, Y: 0},
	}
	hole := []shp.Point{
		{X: 1, Y: 1},
		{X: 2, Y: 1},
		{X: 2, Y: 2},
		{X: 1, Y: 2},
		{X: 1, Y: 1},
	}
	points := append(outer, hole...)

	shape := &shp.Polygon{
		NumParts:  2,
		NumPoints: int32(len(points)),
		Parts:     []int32{0, int32(len(outer))},
		Points:    points,
	}

	geomValue, err := shapeToGeom(shape)
	if err != nil {
		t.Fatalf("shapeToGeom failed: %v", err)
	}

	polygon, ok := geomValue.(*geom.Polygon)
	if !ok {
		t.Fatalf("expected Polygon, got %T", geomValue)
	}
	if polygon.NumLinearRings() != 2 {
		t.Fatalf("expected polygon with hole, got %d rings", polygon.NumLinearRings())
	}

	outerRing := polygon.LinearRing(0).Coords()
	innerRing := polygon.LinearRing(1).Coords()
	if area := signedAreaCoords(outerRing); area <= 0 {
		t.Fatalf("expected outer ring to be CCW, got %f", area)
	}
	if area := signedAreaCoords(innerRing); area >= 0 {
		t.Fatalf("expected inner ring to be CW, got %f", area)
	}
}

func coordsEqual(a, b geom.Coord) bool {
	return almostEqualFloat(a[0], b[0]) && almostEqualFloat(a[1], b[1])
}

func signedAreaCoords(coords []geom.Coord) float64 {
	if len(coords) < 3 {
		return 0
	}
	sum := 0.0
	for i := 0; i < len(coords)-1; i++ {
		x1, y1 := coords[i][0], coords[i][1]
		x2, y2 := coords[i+1][0], coords[i+1][1]
		sum += (x1 * y2) - (x2 * y1)
	}
	return sum / 2
}

func almostEqualFloat(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9
}
