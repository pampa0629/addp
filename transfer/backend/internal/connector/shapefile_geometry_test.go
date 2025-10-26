package connector

import (
	"math"
	"testing"

	"github.com/jonas-p/go-shp"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/wkb"
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
		t.Fatalf("expected outer ring to be CCW (positive area), got %f", area)
	}

	if area := signedAreaCoords(innerRing); area >= 0 {
		t.Fatalf("expected inner ring to be CW (negative area), got %f", area)
	}
}

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
