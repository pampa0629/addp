package service

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func buildQuickViewStatusForTest(t *testing.T, svc *QuickViewService, tenantID, engineID uint, schema, table string) (*QuickViewCapability, error) {
	t.Helper()
	return svc.BuildCapability(context.Background(), QuickViewIdentity{
		TenantID:        tenantID,
		ItemFingerprint: spatialItemFingerprint(engineID, schema, table),
		Locator:         tableLocator(engineID, schema, table),
	}, engineID, schema, table)
}

func TestQuickViewCapabilityUsesDirectGeoJSONForSmallSpatialTable(t *testing.T) {
	for _, recordCount := range []int64{100, 127} {
		t.Run(fmt.Sprintf("record_count_%d", recordCount), func(t *testing.T) {
			db := newTileCacheTaskServiceTestDB(t)
			svc := NewQuickViewService(db, nil)
			svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectGeoJSONMaxRows: 2000})
			svc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
				return &SpatialMetadataResult{
					GeomColumn:      "shape",
					GeometryColumns: []string{"shape"},
					SRID:            4490,
					ExtentSRID:      4490,
					Extent:          []float64{120, 30, 121, 31},
					PrimaryKey:      "id",
					RecordCount:     recordCount,
				}, nil
			})

			capability, err := buildQuickViewStatusForTest(t, svc, 7, 11, "public", "farmland")
			if err != nil {
				t.Fatalf("get quick view status: %v", err)
			}

			if !capability.CanUseQuickView {
				t.Fatalf("can_use_quick_view = false, want true; reason = %s", capability.UnavailableReason)
			}
			if capability.RenderSource != QuickViewRenderSourceDirectGeoJSON {
				t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceDirectGeoJSON)
			}
			if capability.DefaultTileCacheID != nil {
				t.Fatalf("default_tile_cache_id = %#v, want nil for direct GeoJSON", capability.DefaultTileCacheID)
			}
			if !strings.Contains(capability.QuickView.GeoJSONURL, fmt.Sprintf("page_size=%d", recordCount)) {
				t.Fatalf("geojson_url = %s, want page_size=%d", capability.QuickView.GeoJSONURL, recordCount)
			}
			if !strings.Contains(capability.QuickView.GeoJSONURL, "geometry_column=shape") {
				t.Fatalf("geojson_url = %s, want geometry_column=shape", capability.QuickView.GeoJSONURL)
			}

			var preferenceCount int64
			if err := db.Model(&models.QuickView{}).Count(&preferenceCount).Error; err != nil {
				t.Fatalf("count quick view preferences: %v", err)
			}
			if preferenceCount != 0 {
				t.Fatalf("quick_view preference count = %d, want 0 because capability is dynamic", preferenceCount)
			}
		})
	}
}

func TestQuickViewCapabilityUsesLocatorDirectGeoJSONForSmallSpatialItem(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectGeoJSONMaxRows: 2000})
	locator := "addp://engine/26/path/shp/farmland.shp?type=file&item_id=99"
	itemFingerprint := commonModels.GenerateItemFingerprint(26, "shp/farmland.shp")

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: itemFingerprint,
			Locator:         locator,
		},
		EngineID:      26,
		DirectGeoJSON: true,
		GeoJSONURL:    "/api/v1/manager/quick-view/geojson?locator=addp%3A%2F%2Fengine%2F26%2Fpath%2Fshp%2Ffarmland.shp%3Ftype%3Dfile%26item_id%3D99&page=1&page_size=127&geometry_column=geometry",
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "geometry",
			GeometryColumns: []string{"geometry"},
			SRID:            4326,
			ExtentSRID:      4326,
			Extent:          []float64{110, 20, 120, 30},
			RecordCount:     127,
		},
	})
	if err != nil {
		t.Fatalf("build locator quick view capability: %v", err)
	}
	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want true; reason=%s", capability.UnavailableReason)
	}
	if capability.CanGenerateTileCache {
		t.Fatal("can_generate_tile_cache = true, want false for non-tile locator direct GeoJSON source")
	}
	if capability.RenderSource != QuickViewRenderSourceDirectGeoJSON {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceDirectGeoJSON)
	}
	if capability.Locator != locator {
		t.Fatalf("locator = %s, want %s", capability.Locator, locator)
	}
	if !strings.Contains(capability.QuickView.GeoJSONURL, "/manager/quick-view/geojson?") {
		t.Fatalf("geojson_url = %s, want manager quick-view geojson URL", capability.QuickView.GeoJSONURL)
	}
	if !strings.Contains(capability.QuickView.GeoJSONURL, "page_size=127") {
		t.Fatalf("geojson_url = %s, want full page_size=127", capability.QuickView.GeoJSONURL)
	}
}

func TestQuickViewCapabilityUsesDirectGeoJSONForSmallPGTableAndKeepsRealtimeAlternative(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectGeoJSONMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "small_points"),
			Locator:         tableLocator(11, "public", "small_points"),
		},
		EngineID:      11,
		Schema:        "public",
		Table:         "small_points",
		DirectGeoJSON: true,
		GeoJSONURL:    "/api/v1/manager/quick-view/geojson?locator=small_points&page=1&page_size=1000&geometry_column=geom",
		CanTile:       true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "geom",
			GeometryColumns: []string{"geom"},
			SRID:            4326,
			ExtentSRID:      4326,
			Extent:          []float64{120, 30, 121, 31},
			RecordCount:     1000,
		},
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:                     "public",
			Table:                      "small_points",
			GeomColumn:                 "geom",
			SRID:                       4326,
			PerformanceMode:            RealtimeTilePerformanceSourceTransform,
			OptimizationRecommended:    true,
			OptimizationRecommendation: RealtimeTileRecommendationQuickViewOptimization,
		},
	})
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}
	if capability.RenderSource != QuickViewRenderSourceDirectGeoJSON {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceDirectGeoJSON)
	}
	if capability.RealtimeTile == nil || !capability.RealtimeTile.Available {
		t.Fatalf("realtime_tile = %#v, want available alternative", capability.RealtimeTile)
	}
	if capability.RealtimeTile.PerformanceMode != RealtimeTilePerformanceSourceTransform {
		t.Fatalf("performance_mode = %s, want %s", capability.RealtimeTile.PerformanceMode, RealtimeTilePerformanceSourceTransform)
	}
	if capability.RealtimeTile.TimeoutRetryPolicy != RealtimeTileTimeoutRetrySuppressTile {
		t.Fatalf("timeout_retry_policy = %s, want %s", capability.RealtimeTile.TimeoutRetryPolicy, RealtimeTileTimeoutRetrySuppressTile)
	}
}

func TestQuickViewPreferenceUsesStandardItemFingerprint(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	itemFingerprint := spatialItemFingerprint(11, "public", "roads")
	locator := "addp://engine/11/path/public/roads?type=table&item_id=99"

	err := svc.UpdatePreferredModeByIdentity(context.Background(), QuickViewIdentity{
		TenantID:        7,
		ItemFingerprint: itemFingerprint,
		Locator:         locator,
	}, "table_geojson", nil)
	if err != nil {
		t.Fatalf("update preferred mode by standard item fingerprint: %v", err)
	}

	repo := repository.NewQuickViewRepository(db)
	preference, err := repo.GetByIdentity(7, itemFingerprint, locator)
	if err != nil {
		t.Fatalf("load quick view preference: %v", err)
	}
	if preference.ItemFingerprint != itemFingerprint {
		t.Fatalf("item_fingerprint = %s, want %s", preference.ItemFingerprint, itemFingerprint)
	}
	if strings.HasPrefix(preference.ItemFingerprint, "locator:") {
		t.Fatalf("item_fingerprint = %s, must not be derived from locator", preference.ItemFingerprint)
	}
	if preference.Locator != locator {
		t.Fatalf("locator = %s, want %s", preference.Locator, locator)
	}
}

func TestQuickViewPreferenceRejectsLocatorWithoutItemFingerprint(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)

	err := svc.UpdatePreferredModeByIdentity(context.Background(), QuickViewIdentity{
		TenantID: 7,
		Locator:  "addp://engine/11/path/public/roads?type=table&item_id=99",
	}, "table_geojson", nil)
	if err == nil || !strings.Contains(err.Error(), "item identity is missing") {
		t.Fatalf("update preferred mode error = %v, want missing item identity", err)
	}
}

func TestQuickViewCapabilityPrefersReadyTileCacheResultOverDirectGeoJSON(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:  "shape",
			SRID:        4490,
			ExtentSRID:  4490,
			Extent:      []float64{120, 30, 121, 31},
			RecordCount: 127,
		}, nil
	})

	tileCacheResult := createQuickViewTestTileCacheResult(t, db, models.TileCacheStatusReady)
	capability, err := buildQuickViewStatusForTest(t, svc, tileCacheResult.TenantID, 11, "public", "farmland")
	if err != nil {
		t.Fatalf("get quick view status: %v", err)
	}

	if capability.RenderSource != QuickViewRenderSourceCachedTile {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceCachedTile)
	}
	if capability.DefaultTileCacheID == nil || *capability.DefaultTileCacheID != tileCacheResult.ID {
		t.Fatalf("default_tile_cache_id = %#v, want %d", capability.DefaultTileCacheID, tileCacheResult.ID)
	}
	if capability.QuickView.TileFormat != "mvt" {
		t.Fatalf("tile_format = %s, want mvt", capability.QuickView.TileFormat)
	}
}

func TestQuickViewCapabilityUsesSourceTransformRealtimeTileForLargePGTableWithoutQuickViewOptimizationTarget(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectGeoJSONMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "farmland"),
			Locator:         tableLocator(11, "public", "farmland"),
		},
		EngineID:      11,
		Schema:        "public",
		Table:         "farmland",
		DirectGeoJSON: true,
		CanTile:       true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "shape",
			GeometryColumns: []string{"shape"},
			SRID:            2360,
			ExtentSRID:      2360,
			Extent:          []float64{120, 30, 121, 31},
			PrimaryKey:      "id",
			RecordCount:     73090,
		},
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:                     "public",
			Table:                      "farmland",
			GeomColumn:                 "shape",
			SRID:                       2360,
			PerformanceMode:            RealtimeTilePerformanceSourceTransform,
			OptimizationRecommended:    true,
			OptimizationRecommendation: RealtimeTileRecommendationQuickViewOptimization,
		},
	})
	if err != nil {
		t.Fatalf("build quick view capability: %v", err)
	}

	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want source-transform realtime tile; reason=%s", capability.UnavailableReason)
	}
	if !capability.CanGenerateTileCache {
		t.Fatal("can_generate_tile_cache = false, want true for PostGIS source")
	}
	if capability.RenderSource != QuickViewRenderSourceRealtimeTile {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceRealtimeTile)
	}
	if capability.RealtimeTile == nil {
		t.Fatal("realtime_tile is nil")
	}
	if capability.RealtimeTile.PerformanceMode != RealtimeTilePerformanceSourceTransform {
		t.Fatalf("performance_mode = %s, want %s", capability.RealtimeTile.PerformanceMode, RealtimeTilePerformanceSourceTransform)
	}
	if !capability.RealtimeTile.OptimizationRecommended ||
		capability.RealtimeTile.TimeoutRecommendation != RealtimeTileRecommendationQuickViewOptimization ||
		capability.RealtimeTile.TimeoutRetryPolicy != RealtimeTileTimeoutRetrySuppressTile {
		t.Fatalf("realtime_tile = %#v, want optimization recommendation and suppress retry", capability.RealtimeTile)
	}
}

func TestQuickViewCapabilityUsesSource3857RealtimeTileWhenResolved(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{
		DirectGeoJSONMaxRows:  2000,
		RealtimeTileTimeoutMS: 2500,
	})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "farmland"),
			Locator:         tableLocator(11, "public", "farmland"),
		},
		EngineID:      11,
		Schema:        "public",
		Table:         "farmland",
		DirectGeoJSON: true,
		CanTile:       true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "shape",
			GeometryColumns: []string{"shape"},
			SRID:            3857,
			ExtentSRID:      3857,
			Extent:          []float64{13469658, 3503549, 13490000, 3520000},
			PrimaryKey:      "id",
			RecordCount:     73090,
		},
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:          "public",
			Table:           "farmland",
			GeomColumn:      "shape",
			SRID:            3857,
			PerformanceMode: RealtimeTilePerformanceSource3857Index,
		},
	})
	if err != nil {
		t.Fatalf("build quick view capability: %v", err)
	}

	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want realtime tile; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceRealtimeTile {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceRealtimeTile)
	}
	if !capability.CanGenerateTileCache {
		t.Fatal("can_generate_tile_cache = false, want true for PostGIS source")
	}
	if capability.RealtimeTile == nil {
		t.Fatal("realtime_tile is nil")
	}
	if capability.RealtimeTile.PerformanceMode != RealtimeTilePerformanceSource3857Index {
		t.Fatalf("performance_mode = %s, want %s", capability.RealtimeTile.PerformanceMode, RealtimeTilePerformanceSource3857Index)
	}
	if capability.RealtimeTile.TimeoutBudgetMS != 2500 {
		t.Fatalf("timeout_budget_ms = %d, want 2500", capability.RealtimeTile.TimeoutBudgetMS)
	}
	if capability.RealtimeTile.TimeoutRecommendation != RealtimeTileRecommendationTileCacheGeneration ||
		capability.RealtimeTile.TimeoutRetryPolicy != RealtimeTileTimeoutRetryTTL {
		t.Fatalf("realtime_tile = %#v, want tile cache recommendation and ttl retry", capability.RealtimeTile)
	}
}

func TestQuickViewCapabilityUsesRealtimeTileForLargeSpatialTableWithQuickViewOptimizationTarget(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectGeoJSONMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "farmland"),
			Locator:         tableLocator(11, "public", "farmland"),
		},
		EngineID:      11,
		Schema:        "public",
		Table:         "farmland",
		DirectGeoJSON: true,
		CanTile:       true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "shape",
			GeometryColumns: []string{"shape"},
			SRID:            2360,
			ExtentSRID:      2360,
			Extent:          []float64{36139988, 2312732, 36911720, 2923289},
			PrimaryKey:      "id",
			RecordCount:     73090,
		},
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:                      "public",
			Table:                       "farmland_mv3857",
			GeomColumn:                  "geom_3857",
			SRID:                        3857,
			QuickViewOptimizationTarget: true,
		},
	})
	if err != nil {
		t.Fatalf("build quick view capability: %v", err)
	}

	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want realtime tile quick view; reason=%s", capability.UnavailableReason)
	}
	if !capability.CanGenerateTileCache {
		t.Fatal("can_generate_tile_cache = false, want true for realtime tile source")
	}
	if capability.RenderSource != QuickViewRenderSourceRealtimeTile {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceRealtimeTile)
	}
	if capability.QuickView.TileURLTemplate == "" {
		t.Fatal("tile_url_template is empty, want unified tile endpoint")
	}
	if len(capability.QuickView.Extent) != 0 || capability.QuickView.ExtentSRID != 0 {
		t.Fatalf("quick_view extent = %#v srid=%d, want empty renderable extent for non-WGS84 metadata",
			capability.QuickView.Extent, capability.QuickView.ExtentSRID)
	}
	if capability.DefaultTileCacheID != nil {
		t.Fatalf("default_tile_cache_id = %#v, want nil for realtime tile", capability.DefaultTileCacheID)
	}
}

func TestQuickViewCapabilityUsesRealtimeTileForLargePGTableWithoutQuickViewOptimizationTarget(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectGeoJSONMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "a2"),
			Locator:         tableLocator(11, "public", "a2"),
		},
		EngineID:      11,
		Schema:        "public",
		Table:         "a2",
		DirectGeoJSON: true,
		CanTile:       true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "SmGeometry",
			GeometryColumns: []string{"SmGeometry"},
			SRID:            4549,
			ExtentSRID:      4549,
			Extent:          []float64{120, 30, 121, 31},
			PrimaryKey:      "SmID",
			RecordCount:     146180,
		},
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:                     "public",
			Table:                      "a2",
			GeomColumn:                 "SmGeometry",
			SRID:                       4549,
			PerformanceMode:            RealtimeTilePerformanceSourceTransform,
			OptimizationRecommended:    true,
			OptimizationRecommendation: RealtimeTileRecommendationQuickViewOptimization,
		},
	})
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}
	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want realtime tile; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceRealtimeTile {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceRealtimeTile)
	}
	if capability.RealtimeTile == nil {
		t.Fatal("realtime_tile is nil")
	}
	if capability.RealtimeTile.PerformanceMode != RealtimeTilePerformanceSourceTransform {
		t.Fatalf("performance_mode = %s, want source transform path", capability.RealtimeTile.PerformanceMode)
	}
	if !capability.RealtimeTile.OptimizationRecommended ||
		capability.RealtimeTile.TimeoutRecommendation != RealtimeTileRecommendationQuickViewOptimization ||
		capability.RealtimeTile.TimeoutRetryPolicy != RealtimeTileTimeoutRetrySuppressTile {
		t.Fatalf("realtime_tile = %#v, want optimization recommendation and suppress retry", capability.RealtimeTile)
	}
}

func TestQuickViewCapabilityReady3857RealtimeTimeoutRecommendsTileCache(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectGeoJSONMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "a2"),
			Locator:         tableLocator(11, "public", "a2"),
		},
		EngineID:      11,
		Schema:        "public",
		Table:         "a2",
		DirectGeoJSON: true,
		CanTile:       true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "SmGeometry",
			GeometryColumns: []string{"SmGeometry"},
			SRID:            4549,
			ExtentSRID:      4549,
			Extent:          []float64{120, 30, 121, 31},
			PrimaryKey:      "SmID",
			RecordCount:     146180,
		},
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:                      "public",
			Table:                       "addp_qvo_a2",
			GeomColumn:                  "geom_3857",
			SRID:                        3857,
			QuickViewOptimizationTarget: true,
			PerformanceMode:             RealtimeTilePerformanceReady3857Target,
		},
	})
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}
	if capability.RealtimeTile == nil {
		t.Fatal("realtime_tile is nil")
	}
	if capability.RealtimeTile.TimeoutRecommendation != RealtimeTileRecommendationTileCacheGeneration {
		t.Fatalf("timeout_recommendation = %s, want %s", capability.RealtimeTile.TimeoutRecommendation, RealtimeTileRecommendationTileCacheGeneration)
	}
	if capability.RealtimeTile.TimeoutRetryPolicy != RealtimeTileTimeoutRetryTTL {
		t.Fatalf("timeout_retry_policy = %s, want %s", capability.RealtimeTile.TimeoutRetryPolicy, RealtimeTileTimeoutRetryTTL)
	}
}

func TestQuickViewCapabilityUsesRenderExtentForNonWGS84SpatialTable(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectGeoJSONMaxRows: 2000})
	renderExtent := []float64{104.39407266464883, 20.860819209527108, 112.12280883568947, 26.419285005545643}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "dltb"),
			Locator:         tableLocator(11, "public", "dltb"),
		},
		EngineID:      11,
		Schema:        "public",
		Table:         "dltb",
		DirectGeoJSON: true,
		CanTile:       true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:         "SmGeometry",
			GeometryColumns:    []string{"SmGeometry"},
			SRID:               2360,
			ExtentSRID:         2360,
			Extent:             []float64{36139988.055131, 2312732.766837, 36911717.357651, 2923289.6009},
			RenderExtent:       renderExtent,
			RenderExtentSRID:   4326,
			RenderExtentSource: "source_extent_transformed",
			PrimaryKey:         "objectid",
			RecordCount:        10597882,
		},
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:                      "public",
			Table:                       "dltb_mv3857",
			GeomColumn:                  "geom_3857",
			SRID:                        3857,
			QuickViewOptimizationTarget: true,
		},
	})
	if err != nil {
		t.Fatalf("build quick view capability: %v", err)
	}

	if !reflect.DeepEqual(capability.QuickView.Extent, renderExtent) {
		t.Fatalf("quick_view extent = %#v, want %#v", capability.QuickView.Extent, renderExtent)
	}
	if capability.QuickView.ExtentSRID != 4326 {
		t.Fatalf("quick_view extent_srid = %d, want 4326", capability.QuickView.ExtentSRID)
	}
	if capability.QuickView.MinZoom < 3 {
		t.Fatalf("quick_view min_zoom = %d, want >= 3", capability.QuickView.MinZoom)
	}
	if capability.QuickView.MaxZoom != 12 {
		t.Fatalf("quick_view max_zoom = %d, want 12 for million-row table", capability.QuickView.MaxZoom)
	}
	if capability.RenderFacts == nil {
		t.Fatal("render_facts is nil, want render facts")
	}
	if !reflect.DeepEqual(capability.RenderFacts.RenderExtent, renderExtent) {
		t.Fatalf("render_facts render_extent = %#v, want %#v", capability.RenderFacts.RenderExtent, renderExtent)
	}
	if capability.RenderFacts.RenderExtentSRID != 4326 {
		t.Fatalf("render_facts render_extent_srid = %d, want 4326", capability.RenderFacts.RenderExtentSRID)
	}
	if capability.RenderFacts.ZoomRecommendation == nil || capability.RenderFacts.ZoomRecommendation.MaxZoom != 12 {
		t.Fatalf("zoom_recommendation = %#v, want max_zoom=12", capability.RenderFacts.ZoomRecommendation)
	}
}

func TestQuickViewCapabilityIgnoresGeneratingAndFailedTileCacheResultsWhenReadyExists(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:  "shape",
			SRID:        4490,
			ExtentSRID:  4490,
			Extent:      []float64{120, 30, 121, 31},
			RecordCount: 127,
		}, nil
	})

	ready := createQuickViewTestTileCacheResult(t, db, models.TileCacheStatusReady)
	repo := repository.NewTileCacheRepository(db)
	latest := time.Now().Add(time.Hour)
	for _, status := range []string{models.TileCacheStatusGenerating, models.TileCacheStatusFailed} {
		tileCacheResult := &models.TileCache{
			TenantID:        ready.TenantID,
			ItemFingerprint: ready.ItemFingerprint,
			Locator:         ready.Locator,
			TileFormat:      "mvt",
			StorageRef:      "minio://manager/tile-cache/public/farmland/" + status,
			Status:          status,
			CreatedAt:       latest,
			UpdatedAt:       latest,
		}
		if err := repo.CreateTileCache(context.Background(), tileCacheResult); err != nil {
			t.Fatalf("create %s tile cache result: %v", status, err)
		}
	}

	capability, err := buildQuickViewStatusForTest(t, svc, ready.TenantID, 11, "public", "farmland")
	if err != nil {
		t.Fatalf("get quick view status: %v", err)
	}
	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want ready tile cache result; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceCachedTile {
		t.Fatalf("render_source = %s, want tile cache", capability.RenderSource)
	}
	if capability.DefaultTileCacheID == nil || *capability.DefaultTileCacheID != ready.ID {
		t.Fatalf("default_tile_cache_id = %#v, want ready tile cache result %d", capability.DefaultTileCacheID, ready.ID)
	}
}

func TestQuickViewCapabilityFindsReadyTileCacheResultByItemFingerprint(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:  "shape",
			SRID:        4490,
			ExtentSRID:  4490,
			Extent:      []float64{120, 30, 121, 31},
			RecordCount: 73090,
		}, nil
	})

	repo := repository.NewTileCacheRepository(db)
	extentSRID := 4326
	minZoom := 7
	maxZoom := 13
	tileCacheResult := &models.TileCache{
		TenantID:        7,
		ItemFingerprint: spatialItemFingerprint(11, "public", "farmland"),
		Locator:         "addp://engine/11/path/public/renamed_farmland?type=table",
		TileFormat:      "mvt",
		StorageRef:      "minio://manager/tile-cache/public/farmland",
		Extent:          datatypes.JSON([]byte(`[120,30,121,31]`)),
		ExtentSRID:      &extentSRID,
		MinZoom:         &minZoom,
		MaxZoom:         &maxZoom,
		Status:          models.TileCacheStatusReady,
	}
	if err := repo.CreateTileCache(context.Background(), tileCacheResult); err != nil {
		t.Fatalf("create tile cache result: %v", err)
	}

	capability, err := svc.BuildCapability(context.Background(), QuickViewIdentity{
		TenantID:        tileCacheResult.TenantID,
		ItemFingerprint: tileCacheResult.ItemFingerprint,
		Locator:         tableLocator(11, "public", "farmland"),
	}, 11, "public", "farmland")
	if err != nil {
		t.Fatalf("get quick view status: %v", err)
	}
	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want ready tile cache result; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceCachedTile {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceCachedTile)
	}
	if capability.DefaultTileCacheID == nil || *capability.DefaultTileCacheID != tileCacheResult.ID {
		t.Fatalf("default_tile_cache_id = %#v, want %d", capability.DefaultTileCacheID, tileCacheResult.ID)
	}
}

func TestQuickViewCapabilityUsesRenderExtentWhenReadyTileCacheExtentIsNotWGS84(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	renderExtent := []float64{104.4, 20.9, 112.1, 26.4}
	svc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:         "SmGeometry",
			GeometryColumns:    []string{"SmGeometry"},
			SRID:               2360,
			ExtentSRID:         2360,
			Extent:             []float64{36139988, 2312732, 36911720, 2923289},
			RenderExtent:       renderExtent,
			RenderExtentSRID:   4326,
			RenderExtentSource: "source_extent_transformed",
			RecordCount:        10597882,
		}, nil
	})

	repo := repository.NewTileCacheRepository(db)
	extentSRID := 3857
	minZoom := 6
	maxZoom := 12
	tileCacheResult := &models.TileCache{
		TenantID:        7,
		ItemFingerprint: spatialItemFingerprint(11, "public", "dltb"),
		Locator:         tableLocator(11, "public", "dltb"),
		TileFormat:      "mvt",
		StorageRef:      "minio://manager/tile-cache/public/dltb",
		Extent:          datatypes.JSON([]byte(`[11621047,2378680,12481335,3049324]`)),
		ExtentSRID:      &extentSRID,
		MinZoom:         &minZoom,
		MaxZoom:         &maxZoom,
		Status:          models.TileCacheStatusReady,
	}
	if err := repo.CreateTileCache(context.Background(), tileCacheResult); err != nil {
		t.Fatalf("create tile cache result: %v", err)
	}

	capability, err := buildQuickViewStatusForTest(t, svc, 7, 11, "public", "dltb")
	if err != nil {
		t.Fatalf("get quick view status: %v", err)
	}
	if capability.RenderSource != QuickViewRenderSourceCachedTile {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceCachedTile)
	}
	if !reflect.DeepEqual(capability.QuickView.Extent, renderExtent) {
		t.Fatalf("quick_view extent = %#v, want %#v", capability.QuickView.Extent, renderExtent)
	}
	if capability.QuickView.ExtentSRID != 4326 {
		t.Fatalf("quick_view extent_srid = %d, want 4326", capability.QuickView.ExtentSRID)
	}
}

func TestQuickViewCapabilityFallsBackToDirectGeoJSONAfterDefaultTileCacheResultDeleted(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:  "shape",
			SRID:        4490,
			ExtentSRID:  4490,
			Extent:      []float64{120, 30, 121, 31},
			RecordCount: 127,
		}, nil
	})

	tileCacheResult := createQuickViewTestTileCacheResult(t, db, models.TileCacheStatusReady)
	capability, err := buildQuickViewStatusForTest(t, svc, tileCacheResult.TenantID, 11, "public", "farmland")
	if err != nil {
		t.Fatalf("get quick view status before delete: %v", err)
	}
	if capability.DefaultTileCacheID == nil {
		t.Fatal("default_tile_cache_id is nil before delete, want ready tile cache result")
	}

	repo := repository.NewTileCacheRepository(db)
	if err := repo.DeleteTileCache(context.Background(), tileCacheResult.ID, tileCacheResult.TenantID); err != nil {
		t.Fatalf("delete tile cache result: %v", err)
	}

	capability, err = buildQuickViewStatusForTest(t, svc, tileCacheResult.TenantID, 11, "public", "farmland")
	if err != nil {
		t.Fatalf("get quick view status after delete: %v", err)
	}
	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false after delete, want direct GeoJSON fallback; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceDirectGeoJSON {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceDirectGeoJSON)
	}
	if capability.DefaultTileCacheID != nil {
		t.Fatalf("default_tile_cache_id = %#v, want nil after deleted tile cache result fallback", capability.DefaultTileCacheID)
	}
}

func TestQuickViewCapabilityUsesLatestReadyTileCacheResult(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:  "shape",
			SRID:        4490,
			ExtentSRID:  4490,
			Extent:      []float64{120, 30, 121, 31},
			RecordCount: 73090,
		}, nil
	})
	repo := repository.NewTileCacheRepository(db)
	extentSRID := 4326
	minZoom := 0
	maxZoom := 8
	olderUpdatedAt := time.Now().Add(-time.Hour)
	latestUpdatedAt := time.Now()
	olderReady := &models.TileCache{
		TenantID:        7,
		ItemFingerprint: spatialItemFingerprint(11, "public", "farmland"),
		Locator:         tableLocator(11, "public", "farmland"),
		TileFormat:      "mvt",
		StorageRef:      "minio://manager/tile-cache/public/farmland/older",
		Extent:          datatypes.JSON([]byte(`[120,30,121,31]`)),
		ExtentSRID:      &extentSRID,
		MinZoom:         &minZoom,
		MaxZoom:         &maxZoom,
		Status:          models.TileCacheStatusReady,
		CreatedAt:       olderUpdatedAt,
		UpdatedAt:       olderUpdatedAt,
	}
	if err := repo.CreateTileCache(context.Background(), olderReady); err != nil {
		t.Fatalf("create older ready tile cache: %v", err)
	}
	latestReady := &models.TileCache{
		TenantID:        7,
		ItemFingerprint: spatialItemFingerprint(11, "public", "farmland"),
		Locator:         tableLocator(11, "public", "farmland"),
		TileFormat:      "mvt",
		StorageRef:      "minio://manager/tile-cache/public/farmland/latest",
		Extent:          datatypes.JSON([]byte(`[120,30,122,32]`)),
		ExtentSRID:      &extentSRID,
		MinZoom:         &minZoom,
		MaxZoom:         &maxZoom,
		Status:          models.TileCacheStatusReady,
		CreatedAt:       latestUpdatedAt,
		UpdatedAt:       latestUpdatedAt,
	}
	if err := repo.CreateTileCache(context.Background(), latestReady); err != nil {
		t.Fatalf("create latest ready tile cache: %v", err)
	}

	capability, err := buildQuickViewStatusForTest(t, svc, 7, 11, "public", "farmland")
	if err != nil {
		t.Fatalf("get quick view status: %v", err)
	}
	if capability.RenderSource != QuickViewRenderSourceCachedTile {
		t.Fatalf("render_source = %s, want tile cache", capability.RenderSource)
	}
	if capability.DefaultTileCacheID == nil || *capability.DefaultTileCacheID != latestReady.ID {
		t.Fatalf("default_tile_cache_id = %#v, want latest ready tile cache result %d", capability.DefaultTileCacheID, latestReady.ID)
	}
}

func createQuickViewTestTileCacheResult(t *testing.T, db *gorm.DB, status string) *models.TileCache {
	t.Helper()
	repo := repository.NewTileCacheRepository(db)
	extentSRID := 4326
	minZoom := 0
	maxZoom := 8
	now := time.Now()
	tileCacheResult := &models.TileCache{
		TenantID:        7,
		ItemFingerprint: spatialItemFingerprint(11, "public", "farmland"),
		Locator:         tableLocator(11, "public", "farmland"),
		TileFormat:      "mvt",
		StorageRef:      "minio://manager/tile-cache/public/farmland",
		Extent:          datatypes.JSON([]byte(`[120,30,121,31]`)),
		ExtentSRID:      &extentSRID,
		MinZoom:         &minZoom,
		MaxZoom:         &maxZoom,
		Status:          status,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := repo.CreateTileCache(context.Background(), tileCacheResult); err != nil {
		t.Fatalf("create tile cache result: %v", err)
	}
	return tileCacheResult
}

func TestQuickViewCapabilityIncludesOptimizationDiagnostic(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	fingerprint := spatialItemFingerprint(11, "public", "roads")
	execID := "execution-qvo"
	taskID := uint(3)
	extentSRID := 4326
	if err := db.Create(&models.QuickViewOptimization{
		TenantID:                  1,
		ItemFingerprint:           fingerprint,
		Locator:                   tableLocator(11, "public", "roads"),
		TaskID:                    &taskID,
		LastExecutionID:           &execID,
		SourceEngineID:            11,
		SourceSchema:              "public",
		SourceTable:               "roads",
		SourceGeometryColumn:      "shape",
		SourceSRID:                4326,
		TargetSRID:                3857,
		TargetKind:                models.QuickViewOptimizationTargetKindSourceSchemaMaterializedView,
		TargetSchema:              "public",
		TargetTable:               "addp_qvo_ready",
		TargetGeometryColumn:      models.QuickViewOptimizationTargetGeometryColumn,
		Status:                    models.QuickViewOptimizationStatusReady,
		RenderExtent:              datatypes.JSON([]byte(`[100.1,20.2,101.3,21.4]`)),
		RenderExtentSRID:          &extentSRID,
		SourceFingerprintSnapshot: commonModels.JSONMap{},
		Metadata:                  commonModels.JSONMap{"index_name": "idx_addp_qvo_ready_geom_3857_gist"},
	}).Error; err != nil {
		t.Fatalf("create quick view optimization result: %v", err)
	}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        1,
			ItemFingerprint: fingerprint,
			Locator:         tableLocator(11, "public", "roads"),
		},
		EngineID: 11,
		Schema:   "public",
		Table:    "roads",
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "shape",
			GeometryColumns: []string{"shape"},
			SRID:            4326,
			Extent:          []float64{100, 20, 101, 21},
			ExtentSRID:      4326,
			RecordCount:     100000,
		},
		CanTile: true,
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:                      "public",
			Table:                       "addp_qvo_ready",
			GeomColumn:                  "geom_3857",
			SRID:                        3857,
			QuickViewOptimizationTarget: true,
		},
	})
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}
	if capability.Optimization == nil || !capability.Optimization.Available {
		t.Fatalf("optimization diagnostic = %#v, want available", capability.Optimization)
	}
	if capability.Optimization.TargetTable != "addp_qvo_ready" {
		t.Fatalf("optimization target table = %s", capability.Optimization.TargetTable)
	}
	if capability.Optimization.RenderExtentSRID != 4326 ||
		!reflect.DeepEqual(capability.Optimization.RenderExtent, []float64{100.1, 20.2, 101.3, 21.4}) {
		t.Fatalf("optimization render extent = %#v srid=%d", capability.Optimization.RenderExtent, capability.Optimization.RenderExtentSRID)
	}
}

func TestQuickViewCapabilityOptimizationDiagnosticUsesSelectedGeometryColumn(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	fingerprint := spatialItemFingerprint(11, "public", "roads")
	if err := db.Create(&models.QuickViewOptimization{
		TenantID:                  1,
		ItemFingerprint:           fingerprint,
		Locator:                   tableLocator(11, "public", "roads"),
		SourceEngineID:            11,
		SourceSchema:              "public",
		SourceTable:               "roads",
		SourceGeometryColumn:      "shape_a",
		SourceSRID:                4326,
		TargetSRID:                3857,
		TargetKind:                models.QuickViewOptimizationTargetKindSourceSchemaMaterializedView,
		TargetSchema:              "public",
		TargetTable:               "addp_qvo_shape_a",
		TargetGeometryColumn:      models.QuickViewOptimizationTargetGeometryColumn,
		Status:                    models.QuickViewOptimizationStatusReady,
		SourceFingerprintSnapshot: commonModels.JSONMap{},
		Metadata:                  commonModels.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("create quick view optimization result: %v", err)
	}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        1,
			ItemFingerprint: fingerprint,
			Locator:         tableLocator(11, "public", "roads"),
		},
		EngineID: 11,
		Schema:   "public",
		Table:    "roads",
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "shape_b",
			GeometryColumns: []string{"shape_a", "shape_b"},
			SRID:            4326,
			Extent:          []float64{100, 20, 101, 21},
			ExtentSRID:      4326,
			RecordCount:     100000,
		},
		CanTile: true,
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:                      "public",
			Table:                       "roads",
			GeomColumn:                  "shape_b",
			SRID:                        4326,
			QuickViewOptimizationTarget: false,
		},
	})
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}
	if capability.Optimization == nil {
		t.Fatal("optimization diagnostic is nil")
	}
	if capability.Optimization.Available {
		t.Fatalf("optimization diagnostic = %#v, want unavailable for selected geometry column shape_b", capability.Optimization)
	}
}

func TestQuickViewCapabilityReportsExternal3857MaterializedViewOptimization(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        1,
			ItemFingerprint: spatialItemFingerprint(11, "public", "dltb"),
			Locator:         tableLocator(11, "public", "dltb"),
		},
		EngineID: 11,
		Schema:   "public",
		Table:    "dltb",
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "SmGeometry",
			GeometryColumns: []string{"SmGeometry"},
			SRID:            2360,
			Extent:          []float64{100, 20, 101, 21},
			ExtentSRID:      2360,
			RecordCount:     10_000_000,
		},
		CanTile: true,
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:                      "public",
			Table:                       "dltb_3857",
			GeomColumn:                  "geom_3857",
			SRID:                        3857,
			QuickViewOptimizationTarget: true,
			PerformanceMode:             RealtimeTilePerformanceReady3857Target,
		},
	})
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}
	if capability.Optimization == nil || !capability.Optimization.Available {
		t.Fatalf("optimization diagnostic = %#v, want external target available", capability.Optimization)
	}
	if capability.Optimization.ResultID != nil {
		t.Fatalf("result_id = %#v, want nil for external readonly target", capability.Optimization.ResultID)
	}
	if capability.Optimization.TargetKind != QuickViewOptimizationTargetKindExternal3857MaterializedView {
		t.Fatalf("target_kind = %s, want %s", capability.Optimization.TargetKind, QuickViewOptimizationTargetKindExternal3857MaterializedView)
	}
	if capability.Optimization.TargetTable != "dltb_3857" ||
		capability.Optimization.TargetGeometryColumn != "geom_3857" ||
		capability.Optimization.TargetSRID != 3857 {
		t.Fatalf("optimization diagnostic = %#v, want dltb_3857.geom_3857 SRID 3857", capability.Optimization)
	}
	if capability.RealtimeTile == nil ||
		capability.RealtimeTile.PerformanceMode != RealtimeTilePerformanceReady3857Target ||
		capability.RealtimeTile.TimeoutRecommendation != RealtimeTileRecommendationTileCacheGeneration {
		t.Fatalf("realtime_tile = %#v, want ready 3857 target recommending tile cache", capability.RealtimeTile)
	}
}
