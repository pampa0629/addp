package pipeline

import (
	"math"
	"testing"

	"github.com/twpayne/go-geom"
)

// TestWGS84ToWebMercator tests coordinate transformation from WGS84 to Web Mercator
func TestWGS84ToWebMercator(t *testing.T) {
	transformer := &WGS84ToWebMercator{}

	tests := []struct {
		name      string
		lon       float64
		lat       float64
		expectX   float64
		expectY   float64
		tolerance float64
	}{
		{
			name:      "Origin (0, 0)",
			lon:       0,
			lat:       0,
			expectX:   0,
			expectY:   0,
			tolerance: 0.01,
		},
		{
			name:      "Beijing (116.4074, 39.9042)",
			lon:       116.4074,
			lat:       39.9042,
			expectX:   12958412.0,
			expectY:   4852031.0,
			tolerance: 1.0, // 1 meter tolerance
		},
		{
			name:      "New York (-74.0060, 40.7128)",
			lon:       -74.0060,
			lat:       40.7128,
			expectX:   -8238310.0,
			expectY:   4970072.0,
			tolerance: 1.0,
		},
		{
			name:      "Equator (100, 0)",
			lon:       100,
			lat:       0,
			expectX:   11131949.0,
			expectY:   0,
			tolerance: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, err := transformer.Transform(tt.lon, tt.lat)
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}

			if math.Abs(x-tt.expectX) > tt.tolerance {
				t.Errorf("X coordinate mismatch: got %f, expect %f (tolerance %f)", x, tt.expectX, tt.tolerance)
			}
			if math.Abs(y-tt.expectY) > tt.tolerance {
				t.Errorf("Y coordinate mismatch: got %f, expect %f (tolerance %f)", y, tt.expectY, tt.tolerance)
			}
		})
	}
}

// TestWebMercatorToWGS84 tests coordinate transformation from Web Mercator to WGS84
func TestWebMercatorToWGS84(t *testing.T) {
	transformer := &WebMercatorToWGS84{}

	tests := []struct {
		name      string
		x         float64
		y         float64
		expectLon float64
		expectLat float64
		tolerance float64
	}{
		{
			name:      "Origin (0, 0)",
			x:         0,
			y:         0,
			expectLon: 0,
			expectLat: 0,
			tolerance: 0.0001,
		},
		{
			name:      "Beijing Web Mercator",
			x:         12958412.0,
			y:         4852031.0,
			expectLon: 116.4074,
			expectLat: 39.9042,
			tolerance: 0.001, // ~100m tolerance
		},
		{
			name:      "New York Web Mercator",
			x:         -8238310.0,
			y:         4970072.0,
			expectLon: -74.0060,
			expectLat: 40.7128,
			tolerance: 0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lon, lat, err := transformer.Transform(tt.x, tt.y)
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}

			if math.Abs(lon-tt.expectLon) > tt.tolerance {
				t.Errorf("Longitude mismatch: got %f, expect %f (tolerance %f)", lon, tt.expectLon, tt.tolerance)
			}
			if math.Abs(lat-tt.expectLat) > tt.tolerance {
				t.Errorf("Latitude mismatch: got %f, expect %f (tolerance %f)", lat, tt.expectLat, tt.tolerance)
			}
		})
	}
}

// TestRoundTripTransformation tests WGS84 -> Web Mercator -> WGS84 round trip
func TestRoundTripTransformation(t *testing.T) {
	wgs84ToMercator := &WGS84ToWebMercator{}
	mercatorToWGS84 := &WebMercatorToWGS84{}

	testPoints := []struct {
		name string
		lon  float64
		lat  float64
	}{
		{"Origin", 0, 0},
		{"Beijing", 116.4074, 39.9042},
		{"New York", -74.0060, 40.7128},
		{"Sydney", 151.2093, -33.8688},
		{"London", -0.1276, 51.5074},
	}

	for _, tt := range testPoints {
		t.Run(tt.name, func(t *testing.T) {
			// WGS84 -> Web Mercator
			x, y, err := wgs84ToMercator.Transform(tt.lon, tt.lat)
			if err != nil {
				t.Fatalf("Forward transform failed: %v", err)
			}

			// Web Mercator -> WGS84
			lon, lat, err := mercatorToWGS84.Transform(x, y)
			if err != nil {
				t.Fatalf("Reverse transform failed: %v", err)
			}

			// Verify round trip accuracy (within 0.0001 degrees ~ 10 meters)
			tolerance := 0.0001
			if math.Abs(lon-tt.lon) > tolerance {
				t.Errorf("Round-trip longitude error: started with %f, ended with %f (diff %f)",
					tt.lon, lon, math.Abs(lon-tt.lon))
			}
			if math.Abs(lat-tt.lat) > tolerance {
				t.Errorf("Round-trip latitude error: started with %f, ended with %f (diff %f)",
					tt.lat, lat, math.Abs(lat-tt.lat))
			}
		})
	}
}

// TestGetCoordTransformer tests transformer factory
func TestGetCoordTransformer(t *testing.T) {
	tests := []struct {
		name       string
		sourceSRID int
		targetSRID int
		expectErr  bool
	}{
		{"WGS84 to Web Mercator", 4326, 3857, false},
		{"Web Mercator to WGS84", 3857, 4326, false},
		{"Unsupported transformation", 4326, 2154, true},
		{"Same SRID", 4326, 4326, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer, err := GetCoordTransformer(tt.sourceSRID, tt.targetSRID)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if transformer != nil {
					t.Error("Expected nil transformer but got one")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if transformer == nil {
					t.Error("Expected transformer but got nil")
				}
			}
		})
	}
}

// TestTransformPoint tests point geometry transformation
func TestTransformPoint(t *testing.T) {
	transformer := &WGS84ToWebMercator{}

	// Create a WGS84 point (Beijing)
	point := geom.NewPoint(geom.XY).MustSetCoords(geom.Coord{116.4074, 39.9042})

	// Transform to Web Mercator
	transformedGeom, err := TransformGeometry(point, transformer)
	if err != nil {
		t.Fatalf("TransformGeometry failed: %v", err)
	}

	transformedPoint, ok := transformedGeom.(*geom.Point)
	if !ok {
		t.Fatalf("Expected *geom.Point, got %T", transformedGeom)
	}

	coords := transformedPoint.Coords()
	expectX := 12958412.0
	expectY := 4852031.0
	tolerance := 1.0

	if math.Abs(coords.X()-expectX) > tolerance {
		t.Errorf("X coordinate mismatch: got %f, expect %f", coords.X(), expectX)
	}
	if math.Abs(coords.Y()-expectY) > tolerance {
		t.Errorf("Y coordinate mismatch: got %f, expect %f", coords.Y(), expectY)
	}
}

// TestTransformLineString tests linestring geometry transformation
func TestTransformLineString(t *testing.T) {
	transformer := &WGS84ToWebMercator{}

	// Create a WGS84 linestring
	coords := []geom.Coord{
		{0, 0},
		{10, 10},
		{20, 20},
	}
	lineString := geom.NewLineString(geom.XY).MustSetCoords(coords)

	// Transform to Web Mercator
	transformedGeom, err := TransformGeometry(lineString, transformer)
	if err != nil {
		t.Fatalf("TransformGeometry failed: %v", err)
	}

	transformedLS, ok := transformedGeom.(*geom.LineString)
	if !ok {
		t.Fatalf("Expected *geom.LineString, got %T", transformedGeom)
	}

	// Verify number of coordinates preserved
	if transformedLS.NumCoords() != 3 {
		t.Errorf("Expected 3 coordinates, got %d", transformedLS.NumCoords())
	}

	// Verify first point is origin (0, 0) -> (0, 0)
	firstCoord := transformedLS.Coord(0)
	if math.Abs(firstCoord.X()) > 0.01 || math.Abs(firstCoord.Y()) > 0.01 {
		t.Errorf("First coordinate should be near origin, got (%f, %f)", firstCoord.X(), firstCoord.Y())
	}
}

// TestTransformPolygon tests polygon geometry transformation
func TestTransformPolygon(t *testing.T) {
	transformer := &WGS84ToWebMercator{}

	// Create a WGS84 polygon (square)
	exteriorRing := []geom.Coord{
		{0, 0},
		{10, 0},
		{10, 10},
		{0, 10},
		{0, 0}, // Close ring
	}
	polygon := geom.NewPolygon(geom.XY).MustSetCoords([][]geom.Coord{exteriorRing})

	// Transform to Web Mercator
	transformedGeom, err := TransformGeometry(polygon, transformer)
	if err != nil {
		t.Fatalf("TransformGeometry failed: %v", err)
	}

	transformedPoly, ok := transformedGeom.(*geom.Polygon)
	if !ok {
		t.Fatalf("Expected *geom.Polygon, got %T", transformedGeom)
	}

	// Verify number of rings preserved
	if transformedPoly.NumLinearRings() != 1 {
		t.Errorf("Expected 1 ring, got %d", transformedPoly.NumLinearRings())
	}

	// Verify ring closure (first == last)
	ring := transformedPoly.LinearRing(0)
	firstCoord := ring.Coord(0)
	lastCoord := ring.Coord(ring.NumCoords() - 1)

	if firstCoord.X() != lastCoord.X() || firstCoord.Y() != lastCoord.Y() {
		t.Errorf("Polygon ring not closed: first=(%f,%f), last=(%f,%f)",
			firstCoord.X(), firstCoord.Y(), lastCoord.X(), lastCoord.Y())
	}
}

// TestTransformOutOfRangeCoordinates tests error handling for invalid coordinates
func TestTransformOutOfRangeCoordinates(t *testing.T) {
	transformer := &WGS84ToWebMercator{}

	tests := []struct {
		name string
		lon  float64
		lat  float64
	}{
		{"Longitude too large", 181, 0},
		{"Longitude too small", -181, 0},
		{"Latitude too large", 0, 86},
		{"Latitude too small", 0, -86},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := transformer.Transform(tt.lon, tt.lat)
			if err == nil {
				t.Error("Expected error for out-of-range coordinates, got none")
			}
		})
	}
}

// TestTransformMultiPoint tests multipoint geometry transformation
func TestTransformMultiPoint(t *testing.T) {
	transformer := &WGS84ToWebMercator{}

	// Create multipoint with 3 points
	coords := []geom.Coord{
		{0, 0},
		{10, 10},
		{20, 20},
	}
	multiPoint := geom.NewMultiPoint(geom.XY).MustSetCoords(coords)

	// Transform
	transformedGeom, err := TransformGeometry(multiPoint, transformer)
	if err != nil {
		t.Fatalf("TransformGeometry failed: %v", err)
	}

	transformedMP, ok := transformedGeom.(*geom.MultiPoint)
	if !ok {
		t.Fatalf("Expected *geom.MultiPoint, got %T", transformedGeom)
	}

	// Verify number of points preserved
	if transformedMP.NumPoints() != 3 {
		t.Errorf("Expected 3 points, got %d", transformedMP.NumPoints())
	}
}

// BenchmarkWGS84ToWebMercator benchmarks coordinate transformation performance
func BenchmarkWGS84ToWebMercator(b *testing.B) {
	transformer := &WGS84ToWebMercator{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = transformer.Transform(116.4074, 39.9042)
	}
}

// BenchmarkTransformPoint benchmarks point geometry transformation
func BenchmarkTransformPoint(b *testing.B) {
	transformer := &WGS84ToWebMercator{}
	point := geom.NewPoint(geom.XY).MustSetCoords(geom.Coord{116.4074, 39.9042})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = TransformGeometry(point, transformer)
	}
}

// BenchmarkTransformLineString benchmarks linestring transformation with 100 points
func BenchmarkTransformLineString(b *testing.B) {
	transformer := &WGS84ToWebMercator{}

	// Create linestring with 100 points
	coords := make([]geom.Coord, 100)
	for i := 0; i < 100; i++ {
		coords[i] = geom.Coord{float64(i), float64(i)}
	}
	lineString := geom.NewLineString(geom.XY).MustSetCoords(coords)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = TransformGeometry(lineString, transformer)
	}
}
