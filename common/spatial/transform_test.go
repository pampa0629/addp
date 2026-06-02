package spatial

import (
	"context"
	"math"
	"testing"
)

func TestTransformGeoJSONToWGS84_PureGo3857(t *testing.T) {
	t.Parallel()

	input := map[string]interface{}{
		"type":        "Point",
		"coordinates": []interface{}{12958412.49, 4852030.63},
	}

	result, err := TransformGeoJSONToWGS84(context.Background(), input, SRIDWebMercator, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if result.Status != TransformStatusTransformed {
		t.Fatalf("expected transformed status, got %s", result.Status)
	}
	if result.Engine != "pure_go" {
		t.Fatalf("expected pure_go engine, got %s", result.Engine)
	}

	geometry, ok := result.GeoJSON.(map[string]interface{})
	if !ok {
		t.Fatalf("expected geojson map, got %T", result.GeoJSON)
	}
	coords, ok := geometry["coordinates"].([]interface{})
	if !ok || len(coords) < 2 {
		t.Fatalf("unexpected coordinates: %#v", geometry["coordinates"])
	}

	lon, lonOK := coords[0].(float64)
	lat, latOK := coords[1].(float64)
	if !lonOK || !latOK {
		t.Fatalf("coordinates are not float64: %#v", coords)
	}

	if math.Abs(lon-116.4074) > 0.01 {
		t.Fatalf("unexpected lon: got %.6f", lon)
	}
	if math.Abs(lat-39.9042) > 0.01 {
		t.Fatalf("unexpected lat: got %.6f", lat)
	}
	if len(result.BoundingBox) != 4 {
		t.Fatalf("expected bbox, got %#v", result.BoundingBox)
	}
}

func TestTransformGeoJSONToWGS84_UnknownCRS(t *testing.T) {
	t.Parallel()

	input := map[string]interface{}{
		"type":        "Point",
		"coordinates": []interface{}{500000, 4500000},
	}

	result, err := TransformGeoJSONToWGS84(context.Background(), input, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != TransformStatusUnknownCRS {
		t.Fatalf("expected unknown_crs, got %s", result.Status)
	}
	if result.GeoJSON != nil {
		t.Fatalf("expected no transformed geometry, got %#v", result.GeoJSON)
	}
}

func TestTransformGeoJSONToWGS84_Noop4326(t *testing.T) {
	t.Parallel()

	input := map[string]interface{}{
		"type":        "Point",
		"coordinates": []interface{}{116.4074, 39.9042},
	}

	result, err := TransformGeoJSONToWGS84(context.Background(), input, SRIDWGS84, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != TransformStatusNoop {
		t.Fatalf("expected noop, got %s", result.Status)
	}
	if result.Engine != "none" {
		t.Fatalf("expected none engine, got %s", result.Engine)
	}
	if len(result.BoundingBox) != 4 {
		t.Fatalf("expected bbox, got %#v", result.BoundingBox)
	}
}

func TestResolveTransformExecutor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source CRS
		target CRS
		want   string
	}{
		{
			name:   "prefer pure go for 3857 to 4326",
			source: normalizeSourceCRS("", SRIDWebMercator),
			target: normalizeTargetCRS(SRIDWGS84),
			want:   "pure_go",
		},
		{
			name:   "generic epsg uses proj when available",
			source: normalizeSourceCRS("", 32650),
			target: normalizeTargetCRS(SRIDWGS84),
			want:   expectedGenericExecutor(),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			executor := resolveTransformExecutor(tt.source, tt.target)
			if tt.want == "" {
				if executor != nil {
					t.Fatalf("expected no executor, got %s", executor.Name())
				}
				return
			}
			if executor == nil {
				t.Fatalf("expected executor %s, got nil", tt.want)
			}
			if executor.Name() != tt.want {
				t.Fatalf("expected executor %s, got %s", tt.want, executor.Name())
			}
		})
	}
}

func expectedGenericExecutor() string {
	if (projExecutor{}).CanTransform(normalizeSourceCRS("EPSG:32650", 32650), normalizeTargetCRS(SRIDWGS84)) {
		return "proj"
	}
	return ""
}
