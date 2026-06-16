package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/service"
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

func TestPositivePathIntAcceptsMVTSuffixOnY(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "y", Value: "52.mvt"}}

	if got := positivePathInt(c, "y", 0, 0); got != 52 {
		t.Fatalf("positivePathInt() = %d, want 52", got)
	}
	if c.IsAborted() {
		t.Fatal("positivePathInt aborted valid MVT tile y parameter")
	}
}

func TestPositivePathIntAcceptsGinParamNameWithMVTSuffix(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "y.mvt", Value: "419.mvt"}}

	if got := positivePathInt(c, "y", 0, 0); got != 419 {
		t.Fatalf("positivePathInt() = %d, want 419", got)
	}
	if c.IsAborted() {
		t.Fatal("positivePathInt aborted valid Gin MVT tile y parameter")
	}
}

func TestApplyTileResponseHeadersExposesRealtimeTimeoutRecommendation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	applyTileResponseHeaders(c, &service.TileResponse{
		Data:                  []byte{},
		FromCache:             false,
		Duration:              2500 * time.Millisecond,
		RenderSource:          service.QuickViewRenderSourceRealtimeTile,
		Status:                service.TileStatusTimeout,
		PerformanceMode:       service.RealtimeTilePerformanceReady3857Target,
		TimeoutBudget:         2500 * time.Millisecond,
		TimeoutRecommendation: service.RealtimeTileRecommendationTileCacheGeneration,
		TimeoutRetryPolicy:    service.RealtimeTileTimeoutRetryTTL,
		TimeoutRetryAfter:     45 * time.Second,
	})

	headers := w.Header()
	if got := headers.Get("X-ADDP-Tile-Performance-Mode"); got != service.RealtimeTilePerformanceReady3857Target {
		t.Fatalf("performance header = %s, want %s", got, service.RealtimeTilePerformanceReady3857Target)
	}
	if got := headers.Get("X-ADDP-Tile-Recommendation"); got != service.RealtimeTileRecommendationTileCacheGeneration {
		t.Fatalf("recommendation header = %s, want %s", got, service.RealtimeTileRecommendationTileCacheGeneration)
	}
	if got := headers.Get("X-ADDP-Tile-Retry-Policy"); got != service.RealtimeTileTimeoutRetryTTL {
		t.Fatalf("retry policy header = %s, want %s", got, service.RealtimeTileTimeoutRetryTTL)
	}
	if got := headers.Get("X-ADDP-Tile-Timeout-Budget-MS"); got != "2500" {
		t.Fatalf("timeout budget header = %s, want 2500", got)
	}
	if got := headers.Get("Retry-After"); got != "45" {
		t.Fatalf("Retry-After = %s, want 45", got)
	}
	if got := headers.Get("Access-Control-Expose-Headers"); !strings.Contains(got, "X-ADDP-Tile-Recommendation") ||
		!strings.Contains(got, "X-ADDP-Tile-Timeout-Budget-MS") ||
		!strings.Contains(got, "X-ADDP-Tile-Retry-Policy") ||
		!strings.Contains(got, "Retry-After") {
		t.Fatalf("expose headers = %q, want tile recommendation headers exposed", got)
	}
}
