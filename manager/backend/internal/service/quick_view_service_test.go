package service

import (
	"context"
	"fmt"
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

func TestQuickViewCapabilityUsesRealtimeTileForLargeSpatialTableWithoutTileCacheResult(t *testing.T) {
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
			RecordCount:     73090,
		}, nil
	})

	capability, err := buildQuickViewStatusForTest(t, svc, 7, 11, "public", "farmland")
	if err != nil {
		t.Fatalf("get quick view status: %v", err)
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
	if capability.DefaultTileCacheID != nil {
		t.Fatalf("default_tile_cache_id = %#v, want nil for realtime tile", capability.DefaultTileCacheID)
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
			ConfigHash:      status,
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
		ConfigHash:      "older-ready",
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
		ConfigHash:      "latest-ready",
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
