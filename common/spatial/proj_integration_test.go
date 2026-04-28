//go:build proj

package spatial

import (
	"context"
	"math"
	"testing"
)

func TestTransformGeoJSONToWGS84_PROJ3857(t *testing.T) {
	t.Parallel()

	input := map[string]interface{}{
		"type":        "Point",
		"coordinates": []interface{}{12958412.49, 4852030.63},
	}

	result, err := TransformGeoJSON(context.Background(), GeoJSONTransformRequest{
		GeoJSON:    input,
		SourceCRS:  "PROJCS[\"WGS 84 / Pseudo-Mercator\",GEOGCS[\"WGS 84\",DATUM[\"WGS_1984\",SPHEROID[\"WGS 84\",6378137,298.257223563]],PRIMEM[\"Greenwich\",0],UNIT[\"degree\",0.0174532925199433]],PROJECTION[\"Mercator_1SP\"],PARAMETER[\"central_meridian\",0],PARAMETER[\"scale_factor\",1],PARAMETER[\"false_easting\",0],PARAMETER[\"false_northing\",0],UNIT[\"metre\",1],AXIS[\"X\",EAST],AXIS[\"Y\",NORTH],AUTHORITY[\"EPSG\",\"3857\"]]",
		TargetSRID: SRIDWGS84,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != TransformStatusTransformed {
		t.Fatalf("expected transformed, got %s", result.Status)
	}
	if result.Engine != "proj" {
		t.Fatalf("expected proj engine, got %s", result.Engine)
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
}
