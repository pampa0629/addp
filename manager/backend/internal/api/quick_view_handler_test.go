package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
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

func TestQuickViewSourceFromPreviewCarriesCRSDefinition(t *testing.T) {
	definition := &datatype.CRSDefinition{
		ID:                 "ADDP:CRS:custom",
		DefinitionEncoding: datatype.CRSDefinitionEncodingESRIWKT,
		Definition:         `PROJCS["Custom_CRS"]`,
		Source:             datatype.CRSDefinitionSourceSidecarPRJ,
	}
	tablePreview := &models.TablePreview{
		EngineID:            26,
		EngineType:          "nfs",
		GeometryColumn:      "geometry",
		GeometryColumns:     []string{"geometry"},
		SourceCRS:           definition.ID,
		SourceCRSDefinition: definition,
		Extent:              []float64{1, 2, 3, 4},
		Total:               127,
	}

	source := quickViewSourceFromPreview(
		"addp://engine/26/path/shp/custom-crs.shp?type=file&item_id=99",
		nil,
		&preview.PreviewResult{},
		tablePreview,
	)

	if source.SpatialMeta == nil {
		t.Fatal("SpatialMeta is nil")
	}
	if source.SpatialMeta.SourceCRS != definition.ID || source.SpatialMeta.SourceCRSDefinition != definition {
		t.Fatalf("CRS = %q/%#v, want %q/%#v", source.SpatialMeta.SourceCRS, source.SpatialMeta.SourceCRSDefinition, definition.ID, definition)
	}
}

func TestQuickViewSourceFromPreviewDetectsRasterTIFFObject(t *testing.T) {
	locator := "addp://engine/26/path/rasters/small.tif?type=file&item_id=99"
	tablePreview := &models.TablePreview{
		EngineID:   26,
		EngineType: "nfs",
		Object: &models.ObjectPreview{
			EngineID: 26,
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "media",
					"format":    "tiff",
				},
				"storage": map[string]interface{}{
					"total_size":    int64(8 * 1024 * 1024),
					"physical_path": "rasters/small.tif",
				},
				"type_info": map[string]interface{}{
					"media": map[string]interface{}{
						"width":      int64(2048),
						"height":     int64(2048),
						"band_count": int64(3),
					},
				},
				"format_info": map[string]interface{}{
					"tiff": map[string]interface{}{
						"profile": "geotiff",
					},
				},
				"capabilities": map[string]interface{}{
					"spatial": map[string]interface{}{
						"srid":        4326,
						"extent":      []interface{}{110.0, 20.0, 120.0, 30.0},
						"extent_srid": 4326,
					},
				},
			},
		},
	}
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         locator,
			ItemFingerprint: "fp-small-tiff",
		},
	}

	source := quickViewSourceFromPreview(locator, nil, result, tablePreview)

	if source.Raster == nil {
		t.Fatal("Raster is nil, want TIFF raster source")
	}
	if source.DirectGeoJSON || source.GeoJSONURL != "" || source.CanTile {
		t.Fatalf("source routing = direct_geojson:%v geojson_url:%q can_tile:%v, want raster-only", source.DirectGeoJSON, source.GeoJSONURL, source.CanTile)
	}
	if source.EngineID != 26 || source.Identity.Locator != locator || source.Identity.ItemFingerprint != "fp-small-tiff" {
		t.Fatalf("source identity = engine:%d locator:%q fingerprint:%q", source.EngineID, source.Identity.Locator, source.Identity.ItemFingerprint)
	}
	if source.Raster.Profile != "geotiff" || source.Raster.Width != 2048 || source.Raster.Height != 2048 {
		t.Fatalf("raster facts = %#v, want geotiff 2048x2048", source.Raster)
	}
	if !strings.Contains(source.Raster.PreviewURL, "/api/v1/manager/storage-stream?") {
		t.Fatalf("preview_url = %q, want manager storage stream URL", source.Raster.PreviewURL)
	}
	if !strings.Contains(source.Raster.PreviewURL, "engine_id=26") || !strings.Contains(source.Raster.PreviewURL, "storage_ref=rasters%2Fsmall.tif") {
		t.Fatalf("preview_url = %q, want file storage_ref", source.Raster.PreviewURL)
	}
}

func TestApplyLocatorQuickViewURLsSetsRasterMosaicTileTemplate(t *testing.T) {
	capability := &service.QuickViewCapability{
		Locator:      "addp://engine/26/path/mosaics/srtm?type=directory&item_id=99",
		RenderSource: service.QuickViewRenderSourceRasterMosaic,
		QuickView: service.QuickViewRenderInfo{
			RenderSource: service.QuickViewRenderSourceRasterMosaic,
		},
	}

	applyLocatorQuickViewURLs(capability)

	if !strings.Contains(capability.QuickView.TileURLTemplate, "/api/v1/manager/raster_mosaic/tiles/{z}/{x}/{y}.png?") {
		t.Fatalf("tile_url_template = %q, want raster mosaic tile endpoint", capability.QuickView.TileURLTemplate)
	}
	if !strings.Contains(capability.QuickView.TileURLTemplate, "locator=") {
		t.Fatalf("tile_url_template = %q, want encoded locator", capability.QuickView.TileURLTemplate)
	}
	if !strings.Contains(capability.QuickView.TileURLTemplate, "gamma=0.6") {
		t.Fatalf("tile_url_template = %q, want default gamma", capability.QuickView.TileURLTemplate)
	}
}

func TestRasterMosaicTileStyleQueryParsing(t *testing.T) {
	gamma, ok := parseOptionalPositiveFloat("0.7")
	if !ok || gamma != 0.7 {
		t.Fatalf("gamma = %v ok=%v, want 0.7 true", gamma, ok)
	}
	if _, ok := parseOptionalPositiveFloat("bad"); ok {
		t.Fatal("invalid gamma ok = true, want false")
	}

	minValue, maxValue, ok := parseOptionalDisplayRange("10", "4200")
	if !ok || minValue == nil || *minValue != 10 || maxValue == nil || *maxValue != 4200 {
		t.Fatalf("display range = %v %v ok=%v, want 10 4200 true", minValue, maxValue, ok)
	}
	if _, _, ok := parseOptionalDisplayRange("4200", "10"); ok {
		t.Fatal("invalid display range ok = true, want false")
	}

	invert, ok := parseOptionalBool("true")
	if !ok || !invert {
		t.Fatalf("invert = %v ok=%v, want true true", invert, ok)
	}
	if _, ok := parseOptionalBool("maybe"); ok {
		t.Fatal("invalid invert ok = true, want false")
	}
}

func TestQuickViewSourceFromPreviewDetectsRasterTIFFObjectCatalogItem(t *testing.T) {
	locator := "addp://engine/9/path/addp/image/srtm_40_01.tif?type=object&item_id=254"
	tablePreview := &models.TablePreview{
		EngineID:   9,
		EngineType: "minio",
		Object: &models.ObjectPreview{
			EngineID: 9,
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"layout":    "multi",
					"data_type": "media",
					"format":    "tiff",
					"refs": []interface{}{
						map[string]interface{}{"path": "addp/image/srtm_40_01.tif", "role": "main", "primary": true},
						map[string]interface{}{"path": "addp/image/srtm_40_01.tfw", "role": "world_file"},
						map[string]interface{}{"path": "addp/image/srtm_40_01.hdr", "role": "header"},
						map[string]interface{}{"path": "addp/image/srtm_40_01.tif.aux.xml", "role": "auxiliary_metadata"},
					},
				},
				"storage": map[string]interface{}{
					"bucket":        "addp",
					"path":          "image/",
					"name":          "srtm_40_01.tif",
					"physical_path": "addp/image/srtm_40_01.tif",
					"total_size":    int64(160),
				},
				"type_info": map[string]interface{}{
					"media": map[string]interface{}{
						"width":      int64(3601),
						"height":     int64(3601),
						"band_count": int64(1),
					},
				},
				"format_info": map[string]interface{}{
					"tiff": map[string]interface{}{
						"profile": "geotiff",
					},
				},
				"capabilities": map[string]interface{}{
					"spatial": map[string]interface{}{
						"srid":        4326,
						"extent":      []interface{}{40.0, 0.0, 41.0, 1.0},
						"extent_srid": 4326,
					},
				},
			},
		},
	}
	result := &preview.PreviewResult{
		Metadata: &preview.PreviewMetadata{
			Locator:         locator,
			ItemFingerprint: "fp-object-tiff",
		},
	}

	source := quickViewSourceFromPreview(locator, nil, result, tablePreview)

	if source.Raster == nil {
		t.Fatal("Raster is nil, want object catalog TIFF raster source")
	}
	if source.DirectGeoJSON || source.CanTile {
		t.Fatalf("source routing = direct_geojson:%v can_tile:%v, want raster-only", source.DirectGeoJSON, source.CanTile)
	}
	if source.EngineID != 9 || source.Identity.Locator != locator || source.Identity.ItemFingerprint != "fp-object-tiff" {
		t.Fatalf("source identity = engine:%d locator:%q fingerprint:%q", source.EngineID, source.Identity.Locator, source.Identity.ItemFingerprint)
	}
	if source.Raster.Profile != "geotiff" || source.Raster.SizeBytes != 160 || source.Raster.Width != 3601 || source.Raster.Height != 3601 {
		t.Fatalf("raster facts = %#v, want object GeoTIFF facts", source.Raster)
	}
	if !strings.Contains(source.Raster.PreviewURL, "/api/v1/manager/storage-stream?") {
		t.Fatalf("preview_url = %q, want manager storage stream URL", source.Raster.PreviewURL)
	}
	if !strings.Contains(source.Raster.PreviewURL, "engine_id=9") || !strings.Contains(source.Raster.PreviewURL, "storage_ref=addp%2Fimage%2Fsrtm_40_01.tif") {
		t.Fatalf("preview_url = %q, want object bucket/path storage_ref", source.Raster.PreviewURL)
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
