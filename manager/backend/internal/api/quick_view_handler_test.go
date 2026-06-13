package api

import (
	"testing"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	"github.com/gin-gonic/gin"
)

func TestQuickViewFeatureCollectionConvertsWKTToGeoJSON(t *testing.T) {
	itemID := uint(99)
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         "addp://engine/26/path/shp/farmland.shp?type=file&item_id=99",
			ItemID:          &itemID,
			ItemFingerprint: "fingerprint-99",
		},
	}
	tablePreview := &models.TablePreview{
		Rows: []map[string]interface{}{
			{"id": 1, "name": "alpha", "geometry": "POINT (120 30)"},
		},
		Total:           1,
		Page:            1,
		PageSize:        1,
		GeometryColumn:  "geometry",
		GeometryColumns: []string{"geometry"},
		SourceSRID:      4326,
	}

	collection, err := quickViewFeatureCollection(result, tablePreview, "")
	if err != nil {
		t.Fatalf("quickViewFeatureCollection returned error: %v", err)
	}
	if collection["type"] != "FeatureCollection" {
		t.Fatalf("type = %v, want FeatureCollection", collection["type"])
	}
	actualFeatures, ok := collection["features"].([]gin.H)
	if !ok {
		t.Fatalf("features type = %T, want []gin.H", collection["features"])
	}
	if len(actualFeatures) != 1 {
		t.Fatalf("features length = %d, want 1", len(actualFeatures))
	}
	geometry, ok := actualFeatures[0]["geometry"].(gin.H)
	if !ok {
		t.Fatalf("geometry type = %T, want gin.H", actualFeatures[0]["geometry"])
	}
	if geometry["type"] != "Point" {
		t.Fatalf("geometry.type = %v, want Point", geometry["type"])
	}
	metadata, ok := collection["metadata"].(gin.H)
	if !ok {
		t.Fatalf("metadata type = %T, want gin.H", collection["metadata"])
	}
	if metadata["locator"] != result.Metadata.Locator {
		t.Fatalf("metadata.locator = %v, want %s", metadata["locator"], result.Metadata.Locator)
	}
	if metadata["item_fingerprint"] != result.Metadata.ItemFingerprint {
		t.Fatalf("metadata.item_fingerprint = %v, want %s", metadata["item_fingerprint"], result.Metadata.ItemFingerprint)
	}
}

func TestQuickViewFeatureCollectionUsesRequestedGeometryColumn(t *testing.T) {
	tablePreview := &models.TablePreview{
		Rows: []map[string]interface{}{
			{
				"id": 1,
				"geom": map[string]interface{}{
					"type":        "Point",
					"coordinates": []interface{}{120.0, 30.0},
				},
			},
		},
		Total:           1,
		Page:            1,
		PageSize:        1,
		GeometryColumns: []string{"geom"},
		SourceSRID:      4326,
	}

	collection, err := quickViewFeatureCollection(nil, tablePreview, "geom")
	if err != nil {
		t.Fatalf("quickViewFeatureCollection returned error: %v", err)
	}
	actualFeatures, ok := collection["features"].([]gin.H)
	if !ok {
		t.Fatalf("features type = %T, want []gin.H", collection["features"])
	}
	if len(actualFeatures) != 1 {
		t.Fatalf("features length = %d, want 1", len(actualFeatures))
	}
	properties, ok := actualFeatures[0]["properties"].(gin.H)
	if !ok {
		t.Fatalf("properties type = %T, want gin.H", actualFeatures[0]["properties"])
	}
	if _, exists := properties["geom"]; exists {
		t.Fatal("geometry column leaked into feature properties")
	}
}
