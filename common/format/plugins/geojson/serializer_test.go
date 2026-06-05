package geojsonformat

import (
	"encoding/json"
	"testing"
)

func TestGeoJSONFeatureCollectionToJSON(t *testing.T) {
	collection := &GeoJSONFeatureCollection{
		Type: "FeatureCollection",
		Features: []GeoJSONFeature{{
			Type:       "Feature",
			ID:         1,
			Geometry:   json.RawMessage(`{"type":"Point","coordinates":[1,2]}`),
			Properties: map[string]interface{}{"name": "A"},
		}},
		Count: 1,
	}

	text, err := collection.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("ToJSON output is invalid JSON: %v; output=%s", err, text)
	}
	if parsed["type"] != "FeatureCollection" || parsed["count"].(float64) != 1 {
		t.Fatalf("collection = %#v", parsed)
	}
}
