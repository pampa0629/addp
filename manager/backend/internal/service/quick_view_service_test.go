package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
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

func TestQuickViewCapabilityUsesDirectFlatGeobufForSmallSpatialTable(t *testing.T) {
	for _, recordCount := range []int64{100, 127} {
		t.Run(fmt.Sprintf("record_count_%d", recordCount), func(t *testing.T) {
			db := newTileCacheTaskServiceTestDB(t)
			svc := NewQuickViewService(db, nil)
			svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})
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
			if capability.RenderSource != QuickViewRenderSourceDirectFlatGeobuf {
				t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceDirectFlatGeobuf)
			}
			if capability.DefaultTileCacheID != nil {
				t.Fatalf("default_vector_tile_cache_id = %#v, want nil for direct FlatGeobuf", capability.DefaultTileCacheID)
			}
			if !strings.Contains(capability.QuickView.FlatGeobufURL, fmt.Sprintf("page_size=%d", recordCount)) {
				t.Fatalf("flatgeobuf_url = %s, want page_size=%d", capability.QuickView.FlatGeobufURL, recordCount)
			}
			if !strings.Contains(capability.QuickView.FlatGeobufURL, "geometry_column=shape") {
				t.Fatalf("flatgeobuf_url = %s, want geometry_column=shape", capability.QuickView.FlatGeobufURL)
			}

			var preferenceCount int64
			if err := db.Model(&models.PreviewState{}).Count(&preferenceCount).Error; err != nil {
				t.Fatalf("count preview states: %v", err)
			}
			if preferenceCount != 0 {
				t.Fatalf("preview state count = %d, want 0 because capability is dynamic", preferenceCount)
			}
		})
	}
}

func TestCADPreviewSourceAndCapabilityRequireManagedArtifact(t *testing.T) {
	source := CADPreviewSourceFromAttributes(map[string]interface{}{
		"item":    map[string]interface{}{"data_type": "cad", "format": "dwg", "layout": "single"},
		"storage": map[string]interface{}{"total_size": int64(4096)},
	})
	if source == nil || source.Format != "dwg" || source.SourceSizeBytes != 4096 {
		t.Fatalf("CAD source = %#v", source)
	}
	db := newTileCacheTaskServiceTestDB(t)
	capability, err := NewQuickViewService(db, nil).BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{TenantID: 7, ItemFingerprint: "cad-fingerprint", Locator: "addp://engine/26/path/cad/site.dwg?type=file"},
		EngineID: 26,
		CAD:      source,
	})
	if err != nil {
		t.Fatalf("BuildCapabilityFromSource() error = %v", err)
	}
	if capability.SourceKind != QuickViewSourceKindCAD || capability.UnavailableReason != "requires_cad_preview_generation" {
		t.Fatalf("capability = %#v", capability)
	}
	if len(capability.AvailableActions) != 1 || capability.AvailableActions[0] != QuickViewActionGenerateCADPreview {
		t.Fatalf("actions = %#v", capability.AvailableActions)
	}
	if capability.ActiveMode != models.PreviewModeBasicPreview {
		t.Fatalf("active mode = %q", capability.ActiveMode)
	}
}

func TestCADPreviewSourceAcceptsDXF(t *testing.T) {
	source := CADPreviewSourceFromAttributes(map[string]interface{}{
		"item":    map[string]interface{}{"data_type": "cad", "format": "dxf", "layout": "single"},
		"storage": map[string]interface{}{"total_size": int64(2048)},
	})
	if source == nil || source.Format != "dxf" || source.SourceSizeBytes != 2048 {
		t.Fatalf("CAD source = %#v", source)
	}
}

func TestCADPreviewCapabilityUsesBasicRendererWhenArtifactReady(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	capability, err := NewQuickViewService(db, nil).BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{TenantID: 7, ItemFingerprint: "cad-ready", Locator: "addp://engine/26/path/cad/site.dwg?type=file"},
		EngineID: 26,
		CAD: &CADPreviewSource{
			Format: "dwg", PreviewURL: "/api/v1/manager/cad-previews/8/manifest",
		},
	})
	if err != nil {
		t.Fatalf("BuildCapabilityFromSource() error = %v", err)
	}
	if capability.CanUseQuickView || capability.UnavailableReason != "source_format_direct_preview" {
		t.Fatalf("capability = %#v", capability)
	}
	if len(capability.AvailableActions) != 0 {
		t.Fatalf("actions = %#v, want none", capability.AvailableActions)
	}
}

func TestQuickViewCapabilityUsesLocatorDirectFlatGeobufForSmallSpatialItem(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})
	locator := "addp://engine/26/path/shp/farmland.shp?type=file&item_id=99"
	itemFingerprint := commonModels.GenerateItemFingerprint(26, "shp/farmland.shp")

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: itemFingerprint,
			Locator:         locator,
		},
		EngineID:         26,
		DirectFlatGeobuf: true,
		FlatGeobufURL:    "/api/v1/manager/quick-view/flatgeobuf?locator=addp%3A%2F%2Fengine%2F26%2Fpath%2Fshp%2Ffarmland.shp%3Ftype%3Dfile%26item_id%3D99&page=1&page_size=127&geometry_column=geometry",
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
		t.Fatal("can_generate_vector_tile_cache = true, want false for non-tile locator direct FlatGeobuf source")
	}
	if capability.RenderSource != QuickViewRenderSourceDirectFlatGeobuf {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceDirectFlatGeobuf)
	}
	if capability.Locator != locator {
		t.Fatalf("locator = %s, want %s", capability.Locator, locator)
	}
	if !strings.Contains(capability.QuickView.FlatGeobufURL, "/manager/quick-view/flatgeobuf?") {
		t.Fatalf("flatgeobuf_url = %s, want manager quick-view flatgeobuf URL", capability.QuickView.FlatGeobufURL)
	}
	if !strings.Contains(capability.QuickView.FlatGeobufURL, "page_size=127") {
		t.Fatalf("flatgeobuf_url = %s, want full page_size=127", capability.QuickView.FlatGeobufURL)
	}
	if capability.QuickView.SourceCRS != "EPSG:4326" || capability.QuickView.TransformStatus != "not_transformed" || capability.QuickView.PreviewHint != "direct_renderable" {
		t.Fatalf("quick_view CRS contract = %q/%q/%q, want EPSG:4326/not_transformed/direct_renderable", capability.QuickView.SourceCRS, capability.QuickView.TransformStatus, capability.QuickView.PreviewHint)
	}
}

func TestQuickViewCapabilityUsesDirectTIFFForSmallRasterItem(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{
		DirectTIFFMaxBytes:  50 * 1024 * 1024,
		DirectTIFFMaxPixels: 64 * 1000 * 1000,
	})
	locator := "addp://engine/26/path/rasters/small.tif?type=file&item_id=99"
	itemFingerprint := commonModels.GenerateItemFingerprint(26, "rasters/small.tif")

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: itemFingerprint,
			Locator:         locator,
		},
		EngineID: 26,
		Raster: &RasterQuickViewSource{
			Format:              "tiff",
			Profile:             "geotiff",
			SizeBytes:           8 * 1024 * 1024,
			Width:               2048,
			Height:              2048,
			SourceSRID:          4326,
			ExtentSRID:          4326,
			Extent:              []float64{110, 20, 120, 30},
			PreviewURL:          "/api/v1/manager/storage-stream?engine_id=26&storage_ref=rasters%2Fsmall.tif",
			ClientReadMode:      "full_file",
			ClientRenderLibrary: "geotiff.js",
		},
	})
	if err != nil {
		t.Fatalf("build raster quick view capability: %v", err)
	}
	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want true; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceDirectTIFF {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceDirectTIFF)
	}
	if capability.PreferredMode != models.PreviewModeMapQuickView || capability.ActiveMode != models.PreviewModeMapQuickView {
		t.Fatalf("preview mode = preferred:%s active:%s, want map_quick_view default for raster item", capability.PreferredMode, capability.ActiveMode)
	}
	if capability.QuickView.PreviewURL == "" {
		t.Fatalf("preview_url missing in quick_view: %#v", capability.QuickView)
	}
	if capability.CanGenerateTileCache || capability.TileCacheGeneration.Available {
		t.Fatalf("tile generation = can:%v available:%v, want false for raster phase 1", capability.CanGenerateTileCache, capability.TileCacheGeneration.Available)
	}
	if capability.Raster == nil || capability.Raster.Profile != "geotiff" || capability.Raster.ClientRenderLibrary != "geotiff.js" {
		t.Fatalf("raster info = %#v, want geotiff geotiff.js", capability.Raster)
	}
	if capability.Raster.RecommendedAction != "create_cog" {
		t.Fatalf("raster recommended_action = %q, want create_cog for optional optimization", capability.Raster.RecommendedAction)
	}
}

func TestQuickViewCapabilityInfersWGS84CRSFromGeographicRasterExtent(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{
		DirectTIFFMaxBytes:  50 * 1024 * 1024,
		DirectTIFFMaxPixels: 64 * 1000 * 1000,
	})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: commonModels.GenerateItemFingerprint(26, "rasters/no-crs.tif"),
			Locator:         "addp://engine/26/path/rasters/no-crs.tif?type=file&item_id=99",
		},
		EngineID: 26,
		Raster: &RasterQuickViewSource{
			Format:              "tiff",
			Profile:             "geotiff",
			SizeBytes:           8 * 1024 * 1024,
			Width:               2048,
			Height:              2048,
			Extent:              []float64{110, 20, 120, 30},
			PreviewURL:          "/api/v1/manager/storage-stream?engine_id=26&storage_ref=rasters%2Fno-crs.tif",
			ClientReadMode:      "full_file",
			ClientRenderLibrary: "geotiff.js",
		},
	})
	if err != nil {
		t.Fatalf("build raster quick view capability: %v", err)
	}
	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want true with inferred WGS84 CRS; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceDirectTIFF {
		t.Fatalf("render_source = %q, want %q", capability.RenderSource, QuickViewRenderSourceDirectTIFF)
	}
	if capability.QuickView.SourceSRID != 4326 || capability.QuickView.ExtentSRID != 4326 {
		t.Fatalf("quick_view srid = source:%d extent:%d, want inferred 4326", capability.QuickView.SourceSRID, capability.QuickView.ExtentSRID)
	}
	if capability.Raster == nil || capability.Raster.RecommendedAction != "create_cog" {
		t.Fatalf("raster recommended_action = %#v, want create_cog for non-COG raster with inferred CRS", capability.Raster)
	}
	if !capability.Raster.CRSInferred || capability.Raster.CRSInference != RasterCRSInferenceGeographicExtent {
		t.Fatalf("raster CRS inference = %#v/%q, want geographic extent inference", capability.Raster.CRSInferred, capability.Raster.CRSInference)
	}
}

func TestQuickViewCapabilityRecommendsCOGWhenRasterCannotMapLocate(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{
		DirectTIFFMaxBytes:  50 * 1024 * 1024,
		DirectTIFFMaxPixels: 64 * 1000 * 1000,
	})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: commonModels.GenerateItemFingerprint(26, "rasters/no-crs-project.tif"),
			Locator:         "addp://engine/26/path/rasters/no-crs-project.tif?type=file&item_id=99",
		},
		EngineID: 26,
		Raster: &RasterQuickViewSource{
			Format:              "tiff",
			Profile:             "geotiff",
			SizeBytes:           8 * 1024 * 1024,
			Width:               2048,
			Height:              2048,
			Extent:              []float64{500000, 3000000, 510000, 3010000},
			PreviewURL:          "/api/v1/manager/storage-stream?engine_id=26&storage_ref=rasters%2Fno-crs-project.tif",
			ClientReadMode:      "full_file",
			ClientRenderLibrary: "geotiff.js",
		},
	})
	if err != nil {
		t.Fatalf("build raster quick view capability: %v", err)
	}
	if capability.CanUseQuickView {
		t.Fatal("can_use_quick_view = true, want false for missing CRS with non-geographic extent")
	}
	if capability.UnavailableReason != RasterUnavailableReasonMissingCRS {
		t.Fatalf("unavailable_reason = %q, want %q", capability.UnavailableReason, RasterUnavailableReasonMissingCRS)
	}
	if capability.Raster == nil || capability.Raster.RecommendedAction != "create_cog" {
		t.Fatalf("raster recommended_action = %#v, want create_cog for non-COG raster without CRS", capability.Raster)
	}
	if capability.Raster.CRSInferred {
		t.Fatal("raster CRS inferred = true, want false for non-geographic extent")
	}
}

func TestQuickViewCapabilityUsesRasterMosaicTileForMosaicItem(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	locator := "addp://engine/26/path/mosaics/srtm?type=directory&item_id=99"
	itemFingerprint := commonModels.GenerateItemFingerprint(26, "mosaics/srtm")

	source := RasterMosaicQuickViewSourceFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"layout":    "whole",
			"data_type": "media",
			"format":    "raster_mosaic",
		},
		"format_info": map[string]interface{}{
			"raster_mosaic": map[string]interface{}{
				"manifest_ref":    "mosaic.addp.json",
				"index_ref":       "index/source-index.json",
				"overview_ref":    "overviews/overview.cog.tif",
				"leaf_count":      int64(2360),
				"source_count":    int64(2400),
				"overview_width":  int64(4096),
				"overview_height": int64(2048),
			},
		},
		"capabilities": map[string]interface{}{
			"spatial": map[string]interface{}{
				"srid":        4326,
				"crs_ref":     "EPSG:4326",
				"extent":      []interface{}{110.0, 20.0, 120.0, 30.0},
				"extent_srid": 4326,
			},
		},
	})
	if source == nil {
		t.Fatal("RasterMosaicQuickViewSourceFromAttributes() = nil")
	}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: itemFingerprint,
			Locator:         locator,
		},
		EngineID:         26,
		RasterMosaic:     source,
		DirectFlatGeobuf: false,
		CanTile:          false,
	})
	if err != nil {
		t.Fatalf("build raster mosaic quick view capability: %v", err)
	}
	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want true; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceRasterMosaic {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceRasterMosaic)
	}
	if capability.PreferredMode != models.PreviewModeMapQuickView || capability.ActiveMode != models.PreviewModeMapQuickView {
		t.Fatalf("preview mode = preferred:%s active:%s, want map_quick_view default for raster mosaic", capability.PreferredMode, capability.ActiveMode)
	}
	if capability.CanGenerateTileCache || capability.TileCacheGeneration.Available {
		t.Fatalf("tile generation = can:%v available:%v, want false for raster mosaic", capability.CanGenerateTileCache, capability.TileCacheGeneration.Available)
	}
	if capability.QuickView.TileFormat != RasterMosaicTileFormatPNG || capability.QuickView.RenderSource != QuickViewRenderSourceRasterMosaic {
		t.Fatalf("quick_view = %#v, want raster mosaic png tile source", capability.QuickView)
	}
	if capability.RasterMosaic == nil || capability.RasterMosaic.OverviewRef != "overviews/overview.cog.tif" || capability.RasterMosaic.LeafCount != 2360 {
		t.Fatalf("raster_mosaic = %#v, want overview ref and leaf count", capability.RasterMosaic)
	}
	if !reflect.DeepEqual(capability.QuickView.Extent, []float64{110, 20, 120, 30}) || capability.QuickView.ExtentSRID != 4326 {
		t.Fatalf("quick_view extent = %#v/%d, want WGS84 extent", capability.QuickView.Extent, capability.QuickView.ExtentSRID)
	}
}

func TestQuickViewCapabilityRespectsExistingBasicPreviewPreferenceForRasterItem(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{
		DirectTIFFMaxBytes:  50 * 1024 * 1024,
		DirectTIFFMaxPixels: 64 * 1000 * 1000,
	})
	locator := "addp://engine/26/path/rasters/small.tif?type=file&item_id=99"
	itemFingerprint := commonModels.GenerateItemFingerprint(26, "rasters/small.tif")
	if err := svc.repo.UpdatePreferredMode(7, itemFingerprint, locator, models.PreviewModeBasicPreview); err != nil {
		t.Fatalf("save existing preference: %v", err)
	}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: itemFingerprint,
			Locator:         locator,
		},
		EngineID: 26,
		Raster: &RasterQuickViewSource{
			Format:              "tiff",
			Profile:             "geotiff",
			SizeBytes:           8 * 1024 * 1024,
			Width:               2048,
			Height:              2048,
			SourceSRID:          4326,
			ExtentSRID:          4326,
			Extent:              []float64{110, 20, 120, 30},
			PreviewURL:          "/api/v1/manager/storage-stream?engine_id=26&storage_ref=rasters%2Fsmall.tif",
			ClientReadMode:      "full_file",
			ClientRenderLibrary: "geotiff.js",
		},
	})
	if err != nil {
		t.Fatalf("build raster quick view capability: %v", err)
	}
	if !capability.CanUseQuickView || capability.RenderSource != QuickViewRenderSourceDirectTIFF {
		t.Fatalf("raster quick view capability = can:%v source:%s", capability.CanUseQuickView, capability.RenderSource)
	}
	if capability.PreferredMode != models.PreviewModeBasicPreview || capability.ActiveMode != models.PreviewModeBasicPreview {
		t.Fatalf("preview mode = preferred:%s active:%s, want existing basic_preview preference", capability.PreferredMode, capability.ActiveMode)
	}
}

func TestQuickViewCapabilityRequiresCOGForLargeRasterItem(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{
		DirectTIFFMaxBytes:  50 * 1024 * 1024,
		DirectTIFFMaxPixels: 64 * 1000 * 1000,
	})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: commonModels.GenerateItemFingerprint(26, "rasters/large.tif"),
			Locator:         "addp://engine/26/path/rasters/large.tif?type=file&item_id=100",
		},
		EngineID: 26,
		Raster: &RasterQuickViewSource{
			Format:     "tiff",
			Profile:    "geotiff",
			SizeBytes:  900 * 1024 * 1024,
			Width:      120000,
			Height:     80000,
			SourceSRID: 4326,
			ExtentSRID: 4326,
			Extent:     []float64{110, 20, 120, 30},
			PreviewURL: "/api/v1/manager/storage-stream?engine_id=26&storage_ref=rasters%2Flarge.tif",
		},
	})
	if err != nil {
		t.Fatalf("build raster quick view capability: %v", err)
	}
	if capability.CanUseQuickView {
		t.Fatal("can_use_quick_view = true, want unavailable for large raster without managed COG")
	}
	if capability.UnavailableReason != RasterUnavailableReasonRequiresCOGGeneration {
		t.Fatalf("unavailable_reason = %q, want %q", capability.UnavailableReason, RasterUnavailableReasonRequiresCOGGeneration)
	}
	if capability.Raster == nil || capability.Raster.RecommendedAction != "create_cog" {
		t.Fatalf("raster info = %#v, want create_cog recommendation", capability.Raster)
	}
	if capability.CanGenerateTileCache || capability.TileCacheGeneration.Available {
		t.Fatalf("tile generation = can:%v available:%v, want false for raster phase 1", capability.CanGenerateTileCache, capability.TileCacheGeneration.Available)
	}
}

func TestQuickViewCapabilityUsesReadyRasterCOGForLargeRasterItem(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{
		DirectTIFFMaxBytes:  50 * 1024 * 1024,
		DirectTIFFMaxPixels: 64 * 1000 * 1000,
	})
	locator := "addp://engine/26/path/rasters/large.tif?type=file&item_id=100"
	fingerprint := commonModels.GenerateItemFingerprint(26, "rasters/large.tif")
	extentSRID := 4326
	result := &models.RasterCOG{
		TenantID:        7,
		ItemFingerprint: fingerprint,
		Locator:         locator,
		SourceEngineID:  26,
		SourceProfile:   "geotiff",
		SourceSizeBytes: 900 * 1024 * 1024,
		TargetKind:      models.RasterCOGTargetKindMinIO,
		StorageRef:      `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/cog/large.tif"}`,
		FileName:        "large.cog.tif",
		SizeBytes:       480 * 1024 * 1024,
		Width:           120000,
		Height:          80000,
		SourceSRID:      4326,
		Extent:          datatypes.JSON([]byte(`[110,20,120,30]`)),
		ExtentSRID:      &extentSRID,
		Status:          models.RasterCOGStatusReady,
		Metadata:        commonModels.JSONMap{},
	}
	if err := repository.NewRasterCOGRepository(db).Create(context.Background(), result); err != nil {
		t.Fatalf("create raster COG result: %v", err)
	}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: fingerprint,
			Locator:         locator,
		},
		EngineID: 26,
		Raster: &RasterQuickViewSource{
			Format:     "tiff",
			Profile:    "geotiff",
			SizeBytes:  900 * 1024 * 1024,
			Width:      120000,
			Height:     80000,
			SourceSRID: 4326,
			ExtentSRID: 4326,
			Extent:     []float64{110, 20, 120, 30},
			PreviewURL: "/api/v1/manager/storage-stream?engine_id=26&storage_ref=rasters%2Flarge.tif",
		},
	})
	if err != nil {
		t.Fatalf("build raster quick view capability: %v", err)
	}
	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want ready raster COG; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceClientCOG {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceClientCOG)
	}
	if capability.QuickView.PreviewURL != fmt.Sprintf("/api/v1/manager/raster_cog/%d/content", result.ID) {
		t.Fatalf("preview_url = %q, want raster COG content URL", capability.QuickView.PreviewURL)
	}
	if capability.Raster == nil || capability.Raster.Profile != "cog" || capability.Raster.ClientReadMode != "range" {
		t.Fatalf("raster info = %#v, want COG range render", capability.Raster)
	}
}

func TestQuickViewCapabilityUsesReadyModel3DGLBForSupportedSourceFormats(t *testing.T) {
	for _, tc := range []struct {
		format   string
		fullName string
		fileName string
	}{
		{format: "osgb", fullName: "3d/single-osgb/Tile_4_L20_00010t3.osgb", fileName: "Tile_4_L20_00010t3.glb"},
		{format: "gltf", fullName: "3d/gltf/scene/scene.gltf", fileName: "scene.glb"},
		{format: "fbx", fullName: "3d/fbx/gunsfbx/gunsfbx.fbx", fileName: "gunsfbx.glb"},
		{format: "obj", fullName: "3d/obj/AssaultRifle/AssaultRifle_01.obj", fileName: "AssaultRifle_01.glb"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			db := newTileCacheTaskServiceTestDB(t)
			createModel3DGLBTableForTest(t, db)
			svc := NewQuickViewService(db, nil)
			locator := fmt.Sprintf("addp://engine/26/path/%s?type=file&item_id=10282", tc.fullName)
			fingerprint := commonModels.GenerateItemFingerprint(26, tc.fullName)
			result := &models.Model3DGLB{
				TenantID:        7,
				ItemFingerprint: fingerprint,
				Locator:         locator,
				SourceEngineID:  26,
				SourceFormat:    tc.format,
				SourceSizeBytes: 1024 * 1024,
				StorageRef:      fmt.Sprintf(`{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/model3d_glb/%s"}`, tc.fileName),
				FileName:        tc.fileName,
				SizeBytes:       612396,
				Status:          models.Model3DGLBStatusReady,
				Metadata:        commonModels.JSONMap{},
			}
			if err := repository.NewModel3DGLBRepository(db).Create(context.Background(), result); err != nil {
				t.Fatalf("create model3d GLB preview artifact result: %v", err)
			}

			capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
				Identity: QuickViewIdentity{
					TenantID:        7,
					ItemFingerprint: fingerprint,
					Locator:         locator,
				},
				EngineID: 26,
				Model3D: &Model3DGLBSource{
					Format:          tc.format,
					Layout:          "single",
					SourceSizeBytes: 1024 * 1024,
				},
			})
			if err != nil {
				t.Fatalf("build model3d GLB preview capability: %v", err)
			}
			if !capability.CanUseQuickView {
				t.Fatalf("can_use_quick_view = false, want ready GLB; reason=%s", capability.UnavailableReason)
			}
			if capability.RenderSource != QuickViewRenderSourceModel3DGLB {
				t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceModel3DGLB)
			}
			if capability.QuickView.PreviewURL != fmt.Sprintf("/api/v1/manager/model_3d_glb/%d/content", result.ID) {
				t.Fatalf("preview_url = %q, want model3d GLB preview artifact content URL", capability.QuickView.PreviewURL)
			}
			if capability.Model3D == nil || capability.Model3D.Format != tc.format || capability.Model3D.FileName != tc.fileName {
				t.Fatalf("model3d info = %#v, want ready GLB facts", capability.Model3D)
			}
			if capability.CanGenerateTileCache || capability.TileCacheGeneration.Available {
				t.Fatalf("tile generation = can:%v available:%v, want false for model3d GLB preview artifact", capability.CanGenerateTileCache, capability.TileCacheGeneration.Available)
			}
			if capability.SourceKind != QuickViewSourceKindModel3D {
				t.Fatalf("source_kind = %q, want %q", capability.SourceKind, QuickViewSourceKindModel3D)
			}
			if !containsString(capability.AvailableActions, QuickViewActionBackToBasicPreview) {
				t.Fatalf("available_actions = %#v, want back to basic preview action", capability.AvailableActions)
			}
		})
	}
}

func TestModel3DGLBSourceFromAttributesSupportsOBJSingleItem(t *testing.T) {
	source := Model3DGLBSourceFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"data_type": "model_3d",
			"format":    "obj",
			"layout":    "single",
		},
		"storage": map[string]interface{}{
			"total_size": int64(8192),
		},
	})
	if source == nil {
		t.Fatal("source is nil, want OBJ single item to be a model3d GLB quick view source")
	}
	if source.Format != "obj" || source.Layout != "single" || source.SourceSizeBytes != 8192 {
		t.Fatalf("source = %#v, want OBJ single item facts", source)
	}
}

func TestModel3DGLBSourceFromAttributesSupportsSTLSingleItem(t *testing.T) {
	source := Model3DGLBSourceFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"data_type": "model_3d",
			"format":    "stl",
			"layout":    "single",
		},
		"storage": map[string]interface{}{
			"total_size": int64(4096),
		},
	})
	if source == nil {
		t.Fatal("source is nil, want STL single item to be a model3d GLB quick view source")
	}
	if source.Format != "stl" || source.Layout != "single" || source.SourceSizeBytes != 4096 {
		t.Fatalf("source = %#v, want STL single item facts", source)
	}
}

func TestModel3DGLBSourceFromAttributesSupportsIFCSingleItem(t *testing.T) {
	source := Model3DGLBSourceFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"data_type": "model_3d",
			"format":    "ifc",
			"layout":    "single",
		},
		"storage": map[string]interface{}{
			"total_size": int64(16384),
		},
	})
	if source == nil {
		t.Fatal("source is nil, want IFC single item to be a model3d GLB quick view source")
	}
	if source.Format != "ifc" || source.Layout != "single" || source.SourceSizeBytes != 16384 {
		t.Fatalf("source = %#v, want IFC single item facts", source)
	}
}

func TestModel3DDirectPreviewSourceFromAttributes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format string
		layout string
	}{
		{name: "glb", format: "glb", layout: "single"},
		{name: "ply", format: "ply", layout: "single"},
		{name: "3dtiles", format: "3dtiles", layout: "whole"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := Model3DGLBSourceFromAttributes(map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "model_3d",
					"format":    tc.format,
					"layout":    tc.layout,
				},
				"storage": map[string]interface{}{
					"total_size": int64(8192),
				},
			})
			if source == nil {
				t.Fatalf("source is nil, want %s direct preview model source", tc.format)
			}
			if source.Format != tc.format || source.Layout != tc.layout || source.SourceSizeBytes != 8192 {
				t.Fatalf("source = %#v, want %s/%s facts", source, tc.format, tc.layout)
			}
		})
	}
}

func TestModel3DGLBSourceFromAttributesSupportsOSGBSceneWholeItem(t *testing.T) {
	source := Model3DGLBSourceFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"data_type": "model_3d",
			"format":    "osgb_scene",
			"layout":    "whole",
		},
		"storage": map[string]interface{}{
			"total_size": int64(1749426479),
		},
	})
	if source == nil {
		t.Fatal("source is nil, want OSGB Scene whole item to be a model3d tiles quick view source")
	}
	if source.Format != "osgb_scene" || source.Layout != "whole" || source.SourceSizeBytes != 1749426479 {
		t.Fatalf("source = %#v, want OSGB Scene whole item facts", source)
	}
}

func TestModel3DDirectPreviewCapabilityDoesNotRecommendGLBGeneration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		format     string
		layout     string
		previewURL string
	}{
		{name: "glb", format: "glb", layout: "single", previewURL: "/api/v1/manager/storage-stream?engine_id=26&storage_ref=3d%2Fglb%2Fscene.glb"},
		{name: "ply", format: "ply", layout: "single", previewURL: "/api/v1/manager/storage-stream?engine_id=26&storage_ref=3d%2Fply%2Fmesh.ply"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTileCacheTaskServiceTestDB(t)
			createModel3DGLBTableForTest(t, db)
			svc := NewQuickViewService(db, nil)
			capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
				Identity: QuickViewIdentity{
					TenantID:        7,
					ItemFingerprint: "fp-direct-" + tc.format,
					Locator:         fmt.Sprintf("addp://engine/26/path/3d/%s/scene.%s?type=file&item_id=100", tc.format, tc.format),
				},
				EngineID: 26,
				Model3D: &Model3DGLBSource{
					Format:          tc.format,
					Layout:          tc.layout,
					SourceSizeBytes: 4096,
					PreviewURL:      tc.previewURL,
				},
			})
			if err != nil {
				t.Fatalf("build direct model3d capability: %v", err)
			}
			if capability.SourceKind != QuickViewSourceKindModel3D {
				t.Fatalf("source_kind = %q, want %q", capability.SourceKind, QuickViewSourceKindModel3D)
			}
			if capability.CanUseQuickView || capability.UnavailableReason != "source_format_direct_preview" {
				t.Fatalf("capability quick view = can:%v reason:%q, want direct preview reason", capability.CanUseQuickView, capability.UnavailableReason)
			}
			if capability.ActiveMode != models.PreviewModeBasicPreview || capability.PreferredMode != models.PreviewModeBasicPreview {
				t.Fatalf("preview mode = active:%s preferred:%s, want basic preview for direct model source", capability.ActiveMode, capability.PreferredMode)
			}
			if containsString(capability.AvailableActions, QuickViewActionGenerateModel3DGLB) {
				t.Fatalf("available_actions = %#v, want no GLB generation action for direct preview source", capability.AvailableActions)
			}
			if capability.Model3D == nil || capability.Model3D.PreviewURL != tc.previewURL {
				t.Fatalf("model3d info = %#v, want direct preview URL", capability.Model3D)
			}
		})
	}
}

func TestModel3DGLBCapabilityRecommendsGLBGenerationWhenMissing(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createModel3DGLBTableForTest(t, db)
	svc := NewQuickViewService(db, nil)
	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: "fp-ifc-missing",
			Locator:         "addp://engine/26/path/3d/ifc/building.ifc?type=file&item_id=10988",
		},
		EngineID: 26,
		Model3D: &Model3DGLBSource{
			Format:          "ifc",
			Layout:          "single",
			SourceSizeBytes: 4096,
		},
	})
	if err != nil {
		t.Fatalf("build model3d GLB capability: %v", err)
	}
	if capability.SourceKind != QuickViewSourceKindModel3D {
		t.Fatalf("source_kind = %q, want %q", capability.SourceKind, QuickViewSourceKindModel3D)
	}
	if capability.CanUseQuickView || capability.UnavailableReason != "requires_glb_generation" {
		t.Fatalf("capability quick view = can:%v reason:%q, want GLB generation requirement", capability.CanUseQuickView, capability.UnavailableReason)
	}
	if !containsString(capability.AvailableActions, QuickViewActionGenerateModel3DGLB) {
		t.Fatalf("available_actions = %#v, want GLB generation action", capability.AvailableActions)
	}
}

func TestQuickViewCapabilityAvailableActions(t *testing.T) {
	t.Run("switch quick view when quick view is ready but inactive", func(t *testing.T) {
		db := newTileCacheTaskServiceTestDB(t)
		svc := NewQuickViewService(db, nil)
		svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})
		fingerprint := commonModels.GenerateItemFingerprint(26, "shp/farmland.shp")
		locator := "addp://engine/26/path/shp/farmland.shp?type=file"
		if err := svc.repo.UpdatePreferredMode(7, fingerprint, locator, models.PreviewModeBasicPreview); err != nil {
			t.Fatalf("save preview preference: %v", err)
		}

		capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
			Identity: QuickViewIdentity{
				TenantID:        7,
				ItemFingerprint: fingerprint,
				Locator:         locator,
			},
			EngineID:         26,
			DirectFlatGeobuf: true,
			FlatGeobufURL:    "/api/v1/manager/quick-view/flatgeobuf",
			SpatialMeta: &SpatialMetadataResult{
				GeomColumn:  "geometry",
				SRID:        4326,
				ExtentSRID:  4326,
				Extent:      []float64{110, 20, 120, 30},
				RecordCount: 127,
			},
		})
		if err != nil {
			t.Fatalf("build quick view capability: %v", err)
		}
		assertActions(t, capability.AvailableActions, QuickViewActionSwitchQuickView)
		assertNoActions(t, capability.AvailableActions, QuickViewActionBackToBasicPreview)
	})

	t.Run("back to basic preview when quick view is active", func(t *testing.T) {
		db := newTileCacheTaskServiceTestDB(t)
		svc := NewQuickViewService(db, nil)
		svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})
		fingerprint := commonModels.GenerateItemFingerprint(26, "shp/farmland.shp")
		locator := "addp://engine/26/path/shp/farmland.shp?type=file"
		if err := svc.repo.UpdatePreferredMode(7, fingerprint, locator, models.PreviewModeMapQuickView); err != nil {
			t.Fatalf("save preview preference: %v", err)
		}

		capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
			Identity: QuickViewIdentity{
				TenantID:        7,
				ItemFingerprint: fingerprint,
				Locator:         locator,
			},
			EngineID:         26,
			DirectFlatGeobuf: true,
			FlatGeobufURL:    "/api/v1/manager/quick-view/flatgeobuf",
			SpatialMeta: &SpatialMetadataResult{
				GeomColumn:  "geometry",
				SRID:        4326,
				ExtentSRID:  4326,
				Extent:      []float64{110, 20, 120, 30},
				RecordCount: 127,
			},
		})
		if err != nil {
			t.Fatalf("build quick view capability: %v", err)
		}
		assertActions(t, capability.AvailableActions, QuickViewActionBackToBasicPreview)
		assertNoActions(t, capability.AvailableActions, QuickViewActionSwitchQuickView)
	})

	t.Run("vector tile generation when spatial metadata is tile ready", func(t *testing.T) {
		db := newTileCacheTaskServiceTestDB(t)
		svc := NewQuickViewService(db, nil)

		capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
			Identity: QuickViewIdentity{
				TenantID:        7,
				ItemFingerprint: commonModels.GenerateItemFingerprint(26, "shp/large.shp"),
				Locator:         "addp://engine/26/path/shp/large.shp?type=file",
			},
			EngineID: 26,
			CanTile:  true,
			SpatialMeta: &SpatialMetadataResult{
				GeomColumn:  "geometry",
				SRID:        4326,
				ExtentSRID:  4326,
				Extent:      []float64{110, 20, 120, 30},
				RecordCount: 100000,
			},
		})
		if err != nil {
			t.Fatalf("build quick view capability: %v", err)
		}
		assertActions(t, capability.AvailableActions, QuickViewActionGenerateTileCache)
	})

	t.Run("raster COG generation when large raster requires COG", func(t *testing.T) {
		db := newTileCacheTaskServiceTestDB(t)
		svc := NewQuickViewService(db, nil)

		capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
			Identity: QuickViewIdentity{
				TenantID:        7,
				ItemFingerprint: commonModels.GenerateItemFingerprint(26, "rasters/large.tif"),
				Locator:         "addp://engine/26/path/rasters/large.tif?type=file",
			},
			EngineID: 26,
			Raster: &RasterQuickViewSource{
				Format:     "tiff",
				Profile:    "geotiff",
				SizeBytes:  900 * 1024 * 1024,
				Width:      120000,
				Height:     80000,
				SourceSRID: 4326,
				ExtentSRID: 4326,
				Extent:     []float64{110, 20, 120, 30},
				PreviewURL: "/api/v1/manager/storage-stream?engine_id=26&storage_ref=rasters%2Flarge.tif",
			},
		})
		if err != nil {
			t.Fatalf("build quick view capability: %v", err)
		}
		assertActions(t, capability.AvailableActions, QuickViewActionGenerateRasterCOG)
		assertNoActions(t, capability.AvailableActions, QuickViewActionGenerateTileCache)
	})
}

func createModel3DGLBTableForTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`CREATE TABLE manager.model_3d_glb (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		item_id INTEGER,
		locator TEXT,
		task_id INTEGER,
		last_execution_id TEXT,
		source_engine_id INTEGER NOT NULL,
		source_format TEXT NOT NULL,
		source_size_bytes INTEGER,
		storage_ref TEXT NOT NULL,
		file_name TEXT,
		size_bytes INTEGER,
		content_url TEXT,
		status TEXT NOT NULL,
		metadata JSON,
		error_message TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model_3d_glb table: %v", err)
	}
}

func TestGaussianSplatKSplatSourceFromAttributes(t *testing.T) {
	for _, formatName := range []string{"ply", "splat", "ksplat"} {
		formatName := formatName
		t.Run(formatName, func(t *testing.T) {
			source := GaussianSplatKSplatSourceFromAttributes(map[string]interface{}{
				"item": map[string]interface{}{
					"data_type": "gaussian_splat",
					"format":    formatName,
					"layout":    "single",
				},
				"type_info": map[string]interface{}{
					"gaussian_splat": map[string]interface{}{
						"representation":              "3d_gaussian_splatting",
						"splat_count":                 int64(128),
						"has_opacity":                 true,
						"has_scale":                   true,
						"has_rotation":                true,
						"has_spherical_harmonics":     true,
						"sh_degree":                   int64(3),
						"sampled_bounds_3d":           map[string]interface{}{"min_x": 1.0, "min_y": 2.0, "min_z": 3.0, "max_x": 4.0, "max_y": 5.0, "max_z": 6.0},
						"sampled_bounds_sample_count": int64(2048),
					},
				},
				"format_info": map[string]interface{}{
					formatName: map[string]interface{}{
						"scene_center": []interface{}{1.5, 2.5, 3.5},
					},
				},
				"storage": map[string]interface{}{
					"total_size": int64(4096),
				},
			})
			if source == nil {
				t.Fatal("source is nil, want gaussian splat KSplat source")
			}
			if source.Format != formatName || source.Layout != "single" || source.Representation != "3d_gaussian_splatting" || source.SplatCount != 128 || source.SourceSizeBytes != 4096 {
				t.Fatalf("source = %#v, want gaussian splat facts", source)
			}
			if source.HasOpacity == nil || !*source.HasOpacity || source.SHDegree == nil || *source.SHDegree != 3 {
				t.Fatalf("source optional facts = %#v, want opacity and sh degree", source)
			}
			if len(source.SceneCenter) != 3 || source.SceneCenter[0] != 1.5 || source.SceneCenter[2] != 3.5 {
				t.Fatalf("source scene center = %#v, want format_info scene center", source.SceneCenter)
			}
			if source.SampledBounds3D == nil || source.SampledBounds3D.MinX == nil || *source.SampledBounds3D.MinX != 1.0 {
				t.Fatalf("source sampled bounds = %#v, want sampled bounds from attributes", source.SampledBounds3D)
			}
			if source.SampledBoundsSampleCount == nil || *source.SampledBoundsSampleCount != 2048 {
				t.Fatalf("source sampled bounds sample count = %#v, want 2048", source.SampledBoundsSampleCount)
			}
		})
	}
	if model3D := Model3DGLBSourceFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"data_type": "gaussian_splat",
			"format":    "ply",
			"layout":    "single",
		},
	}); model3D != nil {
		t.Fatalf("model3d source = %#v, want nil for gaussian splat", model3D)
	}
}

func TestQuickViewCapabilityRecommendsKSplatGenerationForConvertibleGaussianSources(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createGaussianSplatKSplatTable(t, db)
	svc := NewQuickViewService(db, nil)

	for _, sourceFormat := range []string{"ply", "splat"} {
		t.Run(sourceFormat, func(t *testing.T) {
			capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
				Identity: QuickViewIdentity{
					TenantID:        7,
					ItemFingerprint: "fp-gaussian-" + sourceFormat,
					Locator:         "addp://engine/26/path/3d/splat/model." + sourceFormat + "?type=file&item_id=201",
				},
				EngineID: 26,
				GaussianSplat: &GaussianSplatKSplatSource{
					Format:          sourceFormat,
					Layout:          "single",
					Representation:  "3d_gaussian_splatting",
					SplatCount:      128,
					SourceSizeBytes: 4096,
				},
			})
			if err != nil {
				t.Fatalf("BuildCapabilityFromSource() error = %v", err)
			}
			if capability.CanUseQuickView || capability.RenderSource != "" || capability.UnavailableReason != "requires_ksplat_generation" {
				t.Fatalf("capability quick view = can:%v render:%q reason:%q, want managed KSplat generation requirement", capability.CanUseQuickView, capability.RenderSource, capability.UnavailableReason)
			}
			if capability.Model3D != nil {
				t.Fatalf("model_3d capability = %#v, want nil for gaussian splat", capability.Model3D)
			}
			if capability.GaussianSplat == nil || capability.GaussianSplat.Format != sourceFormat || capability.GaussianSplat.SplatCount != 128 {
				t.Fatalf("gaussian_splat capability = %#v, want gaussian splat facts", capability.GaussianSplat)
			}
			if capability.GaussianSplat.RecommendedAction != commonExecution.TaskTypeGaussianSplatKSplatGeneration {
				t.Fatalf("recommended_action = %q, want KSplat generation recommendation", capability.GaussianSplat.RecommendedAction)
			}
			if capability.CanGenerateTileCache || capability.TileCacheGeneration.Available {
				t.Fatalf("tile generation = can:%v available:%v, want false for gaussian splat", capability.CanGenerateTileCache, capability.TileCacheGeneration.Available)
			}
		})
	}
}

func TestQuickViewCapabilityTreatsSourceKSplatAsBasicPreview(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createGaussianSplatKSplatTable(t, db)
	svc := NewQuickViewService(db, nil)

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: "fp-gaussian-ksplat",
			Locator:         "addp://engine/26/path/3d/splat/model.ksplat?type=file&item_id=201",
		},
		EngineID: 26,
		GaussianSplat: &GaussianSplatKSplatSource{
			Format:          "ksplat",
			Layout:          "single",
			Representation:  "3d_gaussian_splatting",
			SceneCenter:     []float64{-53.4, -0.7, 1522.4},
			SplatCount:      128,
			SourceSizeBytes: 4096,
			PreviewURL:      "/api/v1/manager/storage-stream?engine_id=26&storage_ref=3d%2Fsplat%2Fmodel.ksplat",
		},
	})
	if err != nil {
		t.Fatalf("BuildCapabilityFromSource() error = %v", err)
	}
	if capability.CanUseQuickView || capability.RenderSource != "" || capability.UnavailableReason != "source_format_direct_preview" {
		t.Fatalf("capability quick view = can:%v render:%q reason:%q, want source KSplat basic preview only", capability.CanUseQuickView, capability.RenderSource, capability.UnavailableReason)
	}
	if capability.QuickView.PreviewURL != "" {
		t.Fatalf("quick view preview_url = %q, want empty for source KSplat", capability.QuickView.PreviewURL)
	}
	if capability.PreferredMode != models.PreviewModeBasicPreview || capability.ActiveMode != models.PreviewModeBasicPreview {
		t.Fatalf("preview mode = preferred:%q active:%q, want basic preview", capability.PreferredMode, capability.ActiveMode)
	}
	if capability.GaussianSplat == nil || capability.GaussianSplat.RecommendedAction != "" || capability.GaussianSplat.UnavailableReason != "source_format_direct_preview" {
		t.Fatalf("gaussian_splat = %#v, want source KSplat direct preview facts", capability.GaussianSplat)
	}
	if len(capability.GaussianSplat.SceneCenter) != 3 || capability.GaussianSplat.SceneCenter[2] != 1522.4 {
		t.Fatalf("scene_center = %#v, want source scene center", capability.GaussianSplat.SceneCenter)
	}
	if capability.CanGenerateTileCache || capability.TileCacheGeneration.Available {
		t.Fatalf("tile generation = can:%v available:%v, want false for gaussian splat", capability.CanGenerateTileCache, capability.TileCacheGeneration.Available)
	}
}

func TestQuickViewCapabilityUsesReadyGaussianSplatKSplat(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createGaussianSplatKSplatTable(t, db)
	svc := NewQuickViewService(db, nil)
	locator := "addp://engine/26/path/3d/gaussian/model.ply?type=file&item_id=201"
	result := &models.GaussianSplatKSplat{
		TenantID:        7,
		ItemFingerprint: "fp-gaussian",
		Locator:         locator,
		SourceEngineID:  26,
		SourceFormat:    "ply",
		SourceSizeBytes: 4096,
		StorageRef:      `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/gaussian-splat-ksplat/fp-gaussian/model.ksplat"}`,
		FileName:        "model.ksplat",
		SizeBytes:       4096,
		Status:          models.GaussianSplatKSplatStatusReady,
		Metadata:        commonModels.JSONMap{},
	}
	if err := repository.NewGaussianSplatKSplatRepository(db).Create(context.Background(), result); err != nil {
		t.Fatalf("create gaussian splat KSplat result: %v", err)
	}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: "fp-gaussian",
			Locator:         locator,
		},
		EngineID: 26,
		GaussianSplat: &GaussianSplatKSplatSource{
			Format:                   "ply",
			Layout:                   "single",
			Representation:           "3d_gaussian_splatting",
			SplatCount:               128,
			SourceSizeBytes:          4096,
			SampledBounds3D:          &datatype.Bounds3D{MinX: float64Ptr(1), MinY: float64Ptr(2), MinZ: float64Ptr(3), MaxX: float64Ptr(4), MaxY: float64Ptr(5), MaxZ: float64Ptr(6)},
			SampledBoundsSampleCount: int64Ptr(2048),
		},
	})
	if err != nil {
		t.Fatalf("BuildCapabilityFromSource() error = %v", err)
	}
	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want ready KSplat; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceGaussianSplatKSplat {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceGaussianSplatKSplat)
	}
	if capability.QuickView.PreviewURL != fmt.Sprintf("/api/v1/manager/gaussian_splat_ksplat/%d/content", result.ID) {
		t.Fatalf("preview_url = %q, want gaussian splat KSplat content URL", capability.QuickView.PreviewURL)
	}
	if capability.GaussianSplat == nil || capability.GaussianSplat.FileName != "model.ksplat" || capability.GaussianSplat.RecommendedAction != "" {
		t.Fatalf("gaussian_splat capability = %#v, want ready KSplat facts", capability.GaussianSplat)
	}
	if capability.GaussianSplat.SampledBounds3D == nil || capability.GaussianSplat.SampledBounds3D.MinX == nil || *capability.GaussianSplat.SampledBounds3D.MinX != 1 {
		t.Fatalf("gaussian_splat sampled bounds = %#v, want source sampled bounds", capability.GaussianSplat.SampledBounds3D)
	}
	if capability.GaussianSplat.SampledBoundsSampleCount == nil || *capability.GaussianSplat.SampledBoundsSampleCount != 2048 {
		t.Fatalf("gaussian_splat sampled bounds sample count = %#v, want 2048", capability.GaussianSplat.SampledBoundsSampleCount)
	}
	if capability.Model3D != nil {
		t.Fatalf("model_3d capability = %#v, want nil for gaussian splat", capability.Model3D)
	}
}

func createGaussianSplatKSplatTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`CREATE TABLE manager.gaussian_splat_ksplat (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		item_id INTEGER,
		locator TEXT,
		task_id INTEGER,
		last_execution_id TEXT,
		source_engine_id INTEGER NOT NULL,
		source_format TEXT NOT NULL,
		source_size_bytes INTEGER,
		storage_ref TEXT NOT NULL,
		file_name TEXT,
		size_bytes INTEGER,
		content_url TEXT,
		status TEXT NOT NULL,
		metadata JSON,
		error_message TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create gaussian_splat_ksplat table: %v", err)
	}
}

func float64Ptr(value float64) *float64 {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func TestQuickViewCapabilityUsesReadyRasterCOGWithInferredExtentSRID(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{
		DirectTIFFMaxBytes:  50 * 1024 * 1024,
		DirectTIFFMaxPixels: 64 * 1000 * 1000,
	})
	locator := "addp://engine/26/path/rasters/no-crs.tif?type=file&item_id=100"
	fingerprint := commonModels.GenerateItemFingerprint(26, "rasters/no-crs.tif")
	result := &models.RasterCOG{
		TenantID:        7,
		ItemFingerprint: fingerprint,
		Locator:         locator,
		SourceEngineID:  26,
		SourceProfile:   "geotiff",
		SourceSizeBytes: 900 * 1024 * 1024,
		TargetKind:      models.RasterCOGTargetKindMinIO,
		StorageRef:      `{"type":"object","provider":"addp_object_storage","bucket":"manager","object":"tenant_7/cog/no-crs.tif"}`,
		FileName:        "no-crs.cog.tif",
		SizeBytes:       480 * 1024 * 1024,
		Width:           120000,
		Height:          80000,
		Extent:          datatypes.JSON([]byte(`[110,20,120,30]`)),
		Status:          models.RasterCOGStatusReady,
		Metadata:        commonModels.JSONMap{},
	}
	if err := repository.NewRasterCOGRepository(db).Create(context.Background(), result); err != nil {
		t.Fatalf("create raster COG result: %v", err)
	}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: fingerprint,
			Locator:         locator,
		},
		EngineID: 26,
		Raster: &RasterQuickViewSource{
			Format:     "tiff",
			Profile:    "geotiff",
			SizeBytes:  900 * 1024 * 1024,
			Width:      120000,
			Height:     80000,
			Extent:     []float64{110, 20, 120, 30},
			PreviewURL: "/api/v1/manager/storage-stream?engine_id=26&storage_ref=rasters%2Fno-crs.tif",
		},
	})
	if err != nil {
		t.Fatalf("build raster quick view capability: %v", err)
	}
	if !capability.CanUseQuickView || capability.RenderSource != QuickViewRenderSourceClientCOG {
		t.Fatalf("capability = can:%v source:%s, want ready COG quick view", capability.CanUseQuickView, capability.RenderSource)
	}
	if capability.QuickView.SourceSRID != 4326 || capability.QuickView.ExtentSRID != 4326 {
		t.Fatalf("quick_view srid = source:%d extent:%d, want inferred 4326", capability.QuickView.SourceSRID, capability.QuickView.ExtentSRID)
	}
	if !reflect.DeepEqual(capability.QuickView.Extent, []float64{110, 20, 120, 30}) {
		t.Fatalf("quick_view extent = %#v, want COG extent", capability.QuickView.Extent)
	}
	if capability.Raster == nil || capability.Raster.ExtentSRID != 4326 || !capability.Raster.CRSInferred {
		t.Fatalf("raster info = %#v, want inferred 4326 extent_srid", capability.Raster)
	}
}

func TestRasterQuickViewSourceFromAttributesUsesMetaFacts(t *testing.T) {
	attrs := map[string]interface{}{
		"item": map[string]interface{}{
			"data_type": "media",
			"format":    "tiff",
		},
		"storage": map[string]interface{}{
			"total_size":    int64(900 * 1024 * 1024),
			"physical_path": "rasters/large-cog.tif",
		},
		"type_info": map[string]interface{}{
			"media": map[string]interface{}{
				"width":  int64(120000),
				"height": int64(80000),
			},
		},
		"format_info": map[string]interface{}{
			"tiff": map[string]interface{}{
				"profile":              "cog",
				"is_tiled":             true,
				"has_overviews":        true,
				"is_cloud_optimized":   true,
				"cog_check_level":      "heuristic",
				"nodata":               -32768.0,
				"sample_min":           -49.0,
				"sample_max":           406.0,
				"display_min":          -49.0,
				"display_max":          406.0,
				"display_range_method": "metadata_statistics",
			},
		},
		"capabilities": map[string]interface{}{
			"spatial": map[string]interface{}{
				"srid":        4326,
				"extent":      []interface{}{110.0, 20.0, 120.0, 30.0},
				"extent_srid": 4326,
			},
		},
	}

	raster := RasterQuickViewSourceFromAttributes(attrs, "addp://engine/26/path/rasters/large-cog.tif?type=file&item_id=101", 26)
	if raster == nil {
		t.Fatal("raster source is nil")
	}
	if raster.Profile != "cog" || raster.SizeBytes != int64(900*1024*1024) || raster.Width != 120000 || raster.Height != 80000 {
		t.Fatalf("raster facts = %#v, want COG size and dimensions", raster)
	}
	if raster.PreviewURL == "" || !strings.Contains(raster.PreviewURL, "/api/v1/manager/storage-stream?") {
		t.Fatalf("preview_url = %q, want storage stream URL", raster.PreviewURL)
	}
	if raster.NoData == nil || *raster.NoData != -32768 || raster.DisplayMin == nil || *raster.DisplayMin != -49 || raster.DisplayMax == nil || *raster.DisplayMax != 406 {
		t.Fatalf("raster render stats = nodata:%#v display:%#v/%#v", raster.NoData, raster.DisplayMin, raster.DisplayMax)
	}
	if raster.DisplayRangeMethod != "metadata_statistics" {
		t.Fatalf("display_range_method = %q, want metadata_statistics", raster.DisplayRangeMethod)
	}
	if !strings.Contains(raster.PreviewURL, "engine_id=26") || !strings.Contains(raster.PreviewURL, "storage_ref=rasters%2Flarge-cog.tif") {
		t.Fatalf("preview_url = %q, want file storage_ref", raster.PreviewURL)
	}
	if reason := rasterUnavailableReason(raster, 50*1024*1024, 64*1000*1000); reason != RasterUnavailableReasonRequiresManagedCOG {
		t.Fatalf("raster unavailable reason = %q, want %q", reason, RasterUnavailableReasonRequiresManagedCOG)
	}
	if action := rasterRecommendedActionForReason(RasterUnavailableReasonRequiresManagedCOG); action != "create_managed_cog" {
		t.Fatalf("recommended action = %q, want create_managed_cog", action)
	}
}

func TestRasterQuickViewSourceFromObjectLocatorUsesStorageStream(t *testing.T) {
	attrs := map[string]interface{}{
		"item": map[string]interface{}{
			"data_type": "media",
			"format":    "tiff",
		},
		"type_info": map[string]interface{}{
			"media": map[string]interface{}{
				"width":      int64(1024),
				"height":     int64(1024),
				"size_bytes": int64(10 * 1024 * 1024),
			},
		},
		"capabilities": map[string]interface{}{
			"spatial": map[string]interface{}{
				"srid":        4326,
				"extent":      []interface{}{110.0, 20.0, 120.0, 30.0},
				"extent_srid": 4326,
			},
		},
	}

	raster := RasterQuickViewSourceFromAttributes(attrs, "addp://engine/9/path/addp/rasters/small.tif?type=object&item_id=101", 0)
	if raster == nil {
		t.Fatal("raster source is nil")
	}
	if !strings.Contains(raster.PreviewURL, "/api/v1/manager/storage-stream?") {
		t.Fatalf("preview_url = %q, want storage stream URL", raster.PreviewURL)
	}
	if !strings.Contains(raster.PreviewURL, "engine_id=9") || !strings.Contains(raster.PreviewURL, "storage_ref=addp%2Frasters%2Fsmall.tif") {
		t.Fatalf("preview_url = %q, want object bucket/path storage_ref", raster.PreviewURL)
	}
}

func TestQuickViewCapabilityUsesDirectFlatGeobufWithCRSDefinitionWithoutNumericSRID(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})
	locator := "addp://engine/26/path/shp/custom-crs.shp?type=file&item_id=99"
	itemFingerprint := commonModels.GenerateItemFingerprint(26, "shp/custom-crs.shp")
	crsDefinition := &datatype.CRSDefinition{
		ID:                 "ADDP:CRS:custom",
		DefinitionEncoding: datatype.CRSDefinitionEncodingESRIWKT,
		Definition:         `PROJCS["Custom_CRS"]`,
		Source:             datatype.CRSDefinitionSourceSidecarPRJ,
	}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: itemFingerprint,
			Locator:         locator,
		},
		EngineID:         26,
		DirectFlatGeobuf: true,
		FlatGeobufURL:    "/api/v1/manager/quick-view/flatgeobuf?locator=custom&page=1&page_size=127&geometry_column=geometry",
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:          "geometry",
			GeometryColumns:     []string{"geometry"},
			SourceCRS:           crsDefinition.ID,
			SourceCRSDefinition: crsDefinition,
			Extent:              []float64{570841, 3404864, 598936, 3434951},
			RecordCount:         127,
		},
	})
	if err != nil {
		t.Fatalf("build locator quick view capability: %v", err)
	}
	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want CRS-definition direct FlatGeobuf; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceDirectFlatGeobuf {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceDirectFlatGeobuf)
	}
	if capability.CanGenerateTileCache || capability.TileCacheGeneration.Available {
		t.Fatalf("tile generation = can:%v available:%v, want unavailable without numeric SRID", capability.CanGenerateTileCache, capability.TileCacheGeneration.Available)
	}
	if capability.QuickView.SourceCRS != crsDefinition.ID || capability.QuickView.SourceCRSDefinition != crsDefinition {
		t.Fatalf("quick_view CRS = %q/%#v, want %q/%#v", capability.QuickView.SourceCRS, capability.QuickView.SourceCRSDefinition, crsDefinition.ID, crsDefinition)
	}
	if capability.QuickView.TransformStatus != "not_transformed" || capability.QuickView.PreviewHint != "frontend_transform_required" {
		t.Fatalf("quick_view transform contract = %q/%q, want not_transformed/frontend_transform_required", capability.QuickView.TransformStatus, capability.QuickView.PreviewHint)
	}
}

func TestQuickViewCapabilityReportsDirectFlatGeobufRowLimitBeforeTileSRIDRequirement(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})
	crsDefinition := &datatype.CRSDefinition{
		ID:                 "ADDP:CRS:custom",
		DefinitionEncoding: datatype.CRSDefinitionEncodingESRIWKT,
		Definition:         `PROJCS["Custom_CRS"]`,
		Source:             datatype.CRSDefinitionSourceSidecarPRJ,
	}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: commonModels.GenerateItemFingerprint(26, "shp/large-custom-crs.shp"),
			Locator:         "addp://engine/26/path/shp/large-custom-crs.shp?type=file&item_id=100",
		},
		EngineID:         26,
		DirectFlatGeobuf: true,
		FlatGeobufURL:    "/api/v1/manager/quick-view/flatgeobuf?locator=custom&page=1&page_size=73090&geometry_column=geometry",
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:          "geometry",
			GeometryColumns:     []string{"geometry"},
			SourceCRS:           crsDefinition.ID,
			SourceCRSDefinition: crsDefinition,
			Extent:              []float64{570841, 3404864, 598936, 3434951},
			RecordCount:         73090,
		},
	})
	if err != nil {
		t.Fatalf("build locator quick view capability: %v", err)
	}
	if capability.CanUseQuickView {
		t.Fatal("can_use_quick_view = true, want direct FlatGeobuf row limit unavailable")
	}
	if capability.UnavailableReason != "direct FlatGeobuf quick view exceeds row limit" {
		t.Fatalf("unavailable_reason = %q, want row limit reason", capability.UnavailableReason)
	}
	if capability.TileCacheGeneration.Reason != "tile generation requires numeric SRID" {
		t.Fatalf("tile reason = %q, want numeric SRID reason", capability.TileCacheGeneration.Reason)
	}
}

func TestQuickViewCapabilityKeepsTileGenerationAvailableWhenOnlyDirectFlatGeobufExceedsLimit(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "large_points"),
			Locator:         tableLocator(11, "public", "large_points"),
		},
		EngineID:         11,
		Schema:           "public",
		Table:            "large_points",
		DirectFlatGeobuf: true,
		CanTile:          true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "geom",
			GeometryColumns: []string{"geom"},
			SRID:            4326,
			ExtentSRID:      4326,
			Extent:          []float64{120, 30, 121, 31},
			RecordCount:     73090,
		},
	})
	if err != nil {
		t.Fatalf("build quick view capability: %v", err)
	}
	if capability.CanUseQuickView {
		t.Fatal("can_use_quick_view = true, want unavailable without realtime target")
	}
	if capability.UnavailableReason != "direct FlatGeobuf quick view exceeds row limit" {
		t.Fatalf("unavailable_reason = %q, want row limit reason", capability.UnavailableReason)
	}
	if !capability.TileCacheGeneration.Available || !capability.CanGenerateTileCache {
		t.Fatalf("tile generation = available:%v can:%v, want available for complete numeric spatial metadata", capability.TileCacheGeneration.Available, capability.CanGenerateTileCache)
	}
	if capability.TileCacheGeneration.Reason != "" {
		t.Fatalf("tile reason = %q, want empty", capability.TileCacheGeneration.Reason)
	}
}

func TestQuickViewCapabilityRecommendsZoomFromCGCS20003DegreeGaussKrugerExtent(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: commonModels.GenerateItemFingerprint(26, "shp/landuse.shp"),
			Locator:         "addp://engine/26/path/shp/landuse.shp?type=file&item_id=100",
		},
		EngineID:         26,
		DirectFlatGeobuf: true,
		CanTile:          true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "geometry",
			GeometryColumns: []string{"geometry"},
			SRID:            4549,
			ExtentSRID:      4549,
			Extent:          []float64{570841.0277000004, 3404864.0396999996, 598936.5142999999, 3434951.8803000003},
			RecordCount:     73090,
		},
	})
	if err != nil {
		t.Fatalf("build quick view capability: %v", err)
	}
	if !capability.CanGenerateTileCache || !capability.TileCacheGeneration.Available {
		t.Fatalf("tile generation = available:%v can:%v, want available", capability.TileCacheGeneration.Available, capability.CanGenerateTileCache)
	}
	if capability.RenderFacts == nil || capability.RenderFacts.ZoomRecommendation == nil {
		t.Fatalf("render_facts = %#v, want zoom recommendation", capability.RenderFacts)
	}
	if capability.RenderFacts.ZoomRecommendation.MinZoom != 9 || capability.RenderFacts.ZoomRecommendation.MaxZoom != 18 {
		t.Fatalf("zoom recommendation = %#v, want 9-18", capability.RenderFacts.ZoomRecommendation)
	}
	if capability.RenderFacts.ZoomRecommendation.Status != "estimated" {
		t.Fatalf("zoom status = %q, want estimated", capability.RenderFacts.ZoomRecommendation.Status)
	}
}

func TestQuickViewCapabilityTileCacheCreateURLCarriesSpatialContext(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	itemID := uint(54)
	locator := (&resourcetree.ResourceLocator{
		EngineID: 11,
		Path:     []string{"public", "dltb"},
		Type:     resourcetree.TypeTable,
		ItemID:   &itemID,
	}).ToURI()
	itemFingerprint := commonModels.GenerateItemFingerprint(11, "public.dltb")

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: itemFingerprint,
			Locator:         locator,
		},
		EngineID: 11,
		Schema:   "public",
		Table:    "dltb",
		CanTile:  true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:       "SmGeometry",
			GeometryColumns:  []string{"SmGeometry", "geom_3857"},
			SRID:             2360,
			Extent:           []float64{36139988.055131, 2312732.766837, 36911717.357651, 2923289.6009},
			ExtentSRID:       2360,
			RenderExtent:     []float64{104.39407266464883, 20.860819209527108, 112.12280883568947, 26.419289130393885},
			RenderExtentSRID: 4326,
			RecordCount:      10_000_000,
		},
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:          "public",
			Table:           "dltb_mv3857",
			GeomColumn:      "geom_3857",
			SRID:            3857,
			PerformanceMode: RealtimeTilePerformanceReady3857Target,
		},
	})
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}
	parsed, err := url.Parse(capability.TileCacheGeneration.CreateURL)
	if err != nil {
		t.Fatalf("parse create_url: %v", err)
	}
	query := parsed.Query()
	want := map[string]string{
		"tab":              "tasks",
		"create":           "1",
		"locator":          locator,
		"item_id":          "54",
		"item_fingerprint": itemFingerprint,
		"geom":             "SmGeometry",
		"geometry_columns": "SmGeometry,geom_3857",
		"source_srid":      "2360",
		"extent":           "104.39407266464883,20.860819209527108,112.12280883568947,26.419289130393885",
		"extent_srid":      "4326",
	}
	for key, expected := range want {
		if got := query.Get(key); got != expected {
			t.Fatalf("create_url query[%s] = %q, want %q; url=%s", key, got, expected, capability.TileCacheGeneration.CreateURL)
		}
	}
	for _, key := range []string{"engine_id", "schema", "table"} {
		if got := query.Get(key); got != "" {
			t.Fatalf("create_url query[%s] = %q, want empty locator-only resource identity; url=%s", key, got, capability.TileCacheGeneration.CreateURL)
		}
	}
	if capability.SourceEngineID != 11 || capability.SourceSchema != "public" || capability.SourceTable != "dltb" {
		t.Fatalf("source identity = %d/%s/%s, want 11/public/dltb", capability.SourceEngineID, capability.SourceSchema, capability.SourceTable)
	}
}

func TestQuickViewCapabilityUsesDirectFlatGeobufForSmallPGTableAndKeepsRealtimeAlternative(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "small_points"),
			Locator:         tableLocator(11, "public", "small_points"),
		},
		EngineID:         11,
		Schema:           "public",
		Table:            "small_points",
		DirectFlatGeobuf: true,
		FlatGeobufURL:    "/api/v1/manager/quick-view/flatgeobuf?locator=small_points&page=1&page_size=1000&geometry_column=geom",
		CanTile:          true,
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
			OptimizationRecommendation: RealtimeTileRecommendationVectorMaterializedView,
		},
	})
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}
	if capability.RenderSource != QuickViewRenderSourceDirectFlatGeobuf {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceDirectFlatGeobuf)
	}
	if capability.SourceEngineID != 11 || capability.SourceSchema != "public" || capability.SourceTable != "small_points" {
		t.Fatalf("source identity = %d/%s/%s, want 11/public/small_points", capability.SourceEngineID, capability.SourceSchema, capability.SourceTable)
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

func TestPreviewStateUsesStandardItemFingerprint(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	itemFingerprint := spatialItemFingerprint(11, "public", "roads")
	locator := "addp://engine/11/path/public/roads?type=table&item_id=99"

	err := svc.UpdatePreferredModeByIdentity(context.Background(), QuickViewIdentity{
		TenantID:        7,
		ItemFingerprint: itemFingerprint,
		Locator:         locator,
	}, models.PreviewModeBasicPreview, nil)
	if err != nil {
		t.Fatalf("update preferred mode by standard item fingerprint: %v", err)
	}

	repo := repository.NewPreviewStateRepository(db)
	state, err := repo.GetByIdentity(7, itemFingerprint, locator)
	if err != nil {
		t.Fatalf("load preview state: %v", err)
	}
	if state.ItemFingerprint != itemFingerprint {
		t.Fatalf("item_fingerprint = %s, want %s", state.ItemFingerprint, itemFingerprint)
	}
	if strings.HasPrefix(state.ItemFingerprint, "locator:") {
		t.Fatalf("item_fingerprint = %s, must not be derived from locator", state.ItemFingerprint)
	}
	if state.Locator != locator {
		t.Fatalf("locator = %s, want %s", state.Locator, locator)
	}
}

func TestPreviewStateRejectsLocatorWithoutItemFingerprint(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)

	err := svc.UpdatePreferredModeByIdentity(context.Background(), QuickViewIdentity{
		TenantID: 7,
		Locator:  "addp://engine/11/path/public/roads?type=table&item_id=99",
	}, models.PreviewModeBasicPreview, nil)
	if err == nil || !strings.Contains(err.Error(), "item identity is missing") {
		t.Fatalf("update preferred mode error = %v, want missing item identity", err)
	}
}

func TestQuickViewCapabilityPrefersReadyTileCacheResultOverDirectFlatGeobuf(t *testing.T) {
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
		t.Fatalf("default_vector_tile_cache_id = %#v, want %d", capability.DefaultTileCacheID, tileCacheResult.ID)
	}
	if capability.QuickView.TileFormat != "mvt" {
		t.Fatalf("tile_format = %s, want mvt", capability.QuickView.TileFormat)
	}
}

func TestQuickViewCapabilityUsesSourceTransformRealtimeTileForLargePGTableWithoutVectorMaterializedViewTarget(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "farmland"),
			Locator:         tableLocator(11, "public", "farmland"),
		},
		EngineID:         11,
		Schema:           "public",
		Table:            "farmland",
		DirectFlatGeobuf: true,
		CanTile:          true,
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
			OptimizationRecommendation: RealtimeTileRecommendationVectorMaterializedView,
		},
	})
	if err != nil {
		t.Fatalf("build quick view capability: %v", err)
	}

	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want source-transform realtime tile; reason=%s", capability.UnavailableReason)
	}
	if !capability.CanGenerateTileCache {
		t.Fatal("can_generate_vector_tile_cache = false, want true for PostGIS source")
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
		capability.RealtimeTile.TimeoutRecommendation != RealtimeTileRecommendationVectorMaterializedView ||
		capability.RealtimeTile.TimeoutRetryPolicy != RealtimeTileTimeoutRetrySuppressTile {
		t.Fatalf("realtime_tile = %#v, want optimization recommendation and suppress retry", capability.RealtimeTile)
	}
}

func TestQuickViewCapabilityUsesSource3857RealtimeTileWhenResolved(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{
		DirectFlatGeobufMaxRows: 2000,
		RealtimeTileTimeoutMS:   2500,
	})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "farmland"),
			Locator:         tableLocator(11, "public", "farmland"),
		},
		EngineID:         11,
		Schema:           "public",
		Table:            "farmland",
		DirectFlatGeobuf: true,
		CanTile:          true,
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
			TargetKind:      RealtimeTileTargetKindSourceTable,
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
		t.Fatal("can_generate_vector_tile_cache = false, want true for PostGIS source")
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
	if capability.Optimization == nil ||
		capability.Optimization.Available ||
		capability.Optimization.Status != VectorMaterializedViewStatusNotRequired {
		t.Fatalf("optimization = %#v, want not_required for indexed source 3857", capability.Optimization)
	}
}

func TestQuickViewCapabilitySource3857UnindexedDoesNotRecommendOptimizationTask(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "farmland"),
			Locator:         tableLocator(11, "public", "farmland"),
		},
		EngineID: 11,
		Schema:   "public",
		Table:    "farmland",
		CanTile:  true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "shape",
			GeometryColumns: []string{"shape"},
			SRID:            3857,
			ExtentSRID:      3857,
			Extent:          []float64{13469658, 3503549, 13490000, 3520000},
			RecordCount:     73090,
		},
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:          "public",
			Table:           "farmland",
			GeomColumn:      "shape",
			SRID:            3857,
			TargetKind:      RealtimeTileTargetKindSourceTable,
			PerformanceMode: RealtimeTilePerformanceSource3857,
		},
	})
	if err != nil {
		t.Fatalf("build quick view capability: %v", err)
	}
	if capability.RealtimeTile == nil {
		t.Fatal("realtime_tile is nil")
	}
	if capability.RealtimeTile.OptimizationRecommended ||
		capability.RealtimeTile.OptimizationRecommendation != "" ||
		capability.RealtimeTile.TimeoutRecommendation != RealtimeTileRecommendationTileCacheGeneration ||
		capability.RealtimeTile.TimeoutRetryPolicy != RealtimeTileTimeoutRetrySuppressTile {
		t.Fatalf("realtime_tile = %#v, want tile cache recommendation without vector materialized view task", capability.RealtimeTile)
	}
	if capability.Optimization == nil ||
		capability.Optimization.Available ||
		capability.Optimization.Status != VectorMaterializedViewStatusNotRequired {
		t.Fatalf("optimization = %#v, want not_required for source 3857", capability.Optimization)
	}
}

func TestQuickViewCapabilityUsesRealtimeTileForLargeSpatialTableWithVectorMaterializedViewTarget(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "farmland"),
			Locator:         tableLocator(11, "public", "farmland"),
		},
		EngineID:         11,
		Schema:           "public",
		Table:            "farmland",
		DirectFlatGeobuf: true,
		CanTile:          true,
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
			Schema:                       "public",
			Table:                        "farmland_mv3857",
			GeomColumn:                   "geom_3857",
			SRID:                         3857,
			VectorMaterializedViewTarget: true,
		},
	})
	if err != nil {
		t.Fatalf("build quick view capability: %v", err)
	}

	if !capability.CanUseQuickView {
		t.Fatalf("can_use_quick_view = false, want realtime tile quick view; reason=%s", capability.UnavailableReason)
	}
	if !capability.CanGenerateTileCache {
		t.Fatal("can_generate_vector_tile_cache = false, want true for realtime tile source")
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
		t.Fatalf("default_vector_tile_cache_id = %#v, want nil for realtime tile", capability.DefaultTileCacheID)
	}
}

func TestQuickViewCapabilityUsesRealtimeTileForLargePGTableWithoutVectorMaterializedViewTarget(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "a2"),
			Locator:         tableLocator(11, "public", "a2"),
		},
		EngineID:         11,
		Schema:           "public",
		Table:            "a2",
		DirectFlatGeobuf: true,
		CanTile:          true,
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
			OptimizationRecommendation: RealtimeTileRecommendationVectorMaterializedView,
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
		capability.RealtimeTile.TimeoutRecommendation != RealtimeTileRecommendationVectorMaterializedView ||
		capability.RealtimeTile.TimeoutRetryPolicy != RealtimeTileTimeoutRetrySuppressTile {
		t.Fatalf("realtime_tile = %#v, want optimization recommendation and suppress retry", capability.RealtimeTile)
	}
}

func TestQuickViewCapabilityReady3857RealtimeTimeoutRecommendsTileCache(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "a2"),
			Locator:         tableLocator(11, "public", "a2"),
		},
		EngineID:         11,
		Schema:           "public",
		Table:            "a2",
		DirectFlatGeobuf: true,
		CanTile:          true,
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
			Schema:                       "public",
			Table:                        "addp_vmv_a2",
			GeomColumn:                   "geom_3857",
			SRID:                         3857,
			VectorMaterializedViewTarget: true,
			PerformanceMode:              RealtimeTilePerformanceReady3857Target,
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
	svc.SetCapabilityOptions(QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: 2000})
	renderExtent := []float64{104.39407266464883, 20.860819209527108, 112.12280883568947, 26.419285005545643}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: spatialItemFingerprint(11, "public", "dltb"),
			Locator:         tableLocator(11, "public", "dltb"),
		},
		EngineID:         11,
		Schema:           "public",
		Table:            "dltb",
		DirectFlatGeobuf: true,
		CanTile:          true,
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
			Schema:                       "public",
			Table:                        "dltb_mv3857",
			GeomColumn:                   "geom_3857",
			SRID:                         3857,
			VectorMaterializedViewTarget: true,
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
		t.Fatalf("default_vector_tile_cache_id = %#v, want ready tile cache result %d", capability.DefaultTileCacheID, ready.ID)
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
		t.Fatalf("default_vector_tile_cache_id = %#v, want %d", capability.DefaultTileCacheID, tileCacheResult.ID)
	}
}

func TestQuickViewCapabilityReadyTileCacheDisablesGenerationActionForFileSource(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	locator := "addp://engine/26/path/shp/farmland.shp?type=file&item_id=99"
	itemFingerprint := commonModels.GenerateItemFingerprint(26, "shp/farmland.shp")
	repo := repository.NewTileCacheRepository(db)
	extentSRID := 4326
	minZoom := 3
	maxZoom := 12
	ready := &models.TileCache{
		TenantID:        7,
		ItemFingerprint: itemFingerprint,
		Locator:         locator,
		TileFormat:      "mvt",
		StorageRef:      "minio://manager/tile-cache/shp/farmland",
		Extent:          datatypes.JSON([]byte(`[110,20,120,30]`)),
		ExtentSRID:      &extentSRID,
		MinZoom:         &minZoom,
		MaxZoom:         &maxZoom,
		Status:          models.TileCacheStatusReady,
	}
	if err := repo.CreateTileCache(context.Background(), ready); err != nil {
		t.Fatalf("create ready tile cache: %v", err)
	}

	capability, err := svc.BuildCapabilityFromSource(context.Background(), QuickViewSource{
		Identity: QuickViewIdentity{
			TenantID:        7,
			ItemFingerprint: itemFingerprint,
			Locator:         locator,
		},
		EngineID:         26,
		DirectFlatGeobuf: true,
		CanTile:          true,
		SpatialMeta: &SpatialMetadataResult{
			GeomColumn:      "geometry",
			GeometryColumns: []string{"geometry"},
			SRID:            4326,
			ExtentSRID:      4326,
			Extent:          []float64{110, 20, 120, 30},
			RecordCount:     73090,
		},
	})
	if err != nil {
		t.Fatalf("build locator quick view capability: %v", err)
	}
	if capability.RenderSource != QuickViewRenderSourceCachedTile {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceCachedTile)
	}
	if capability.DefaultTileCacheID == nil || *capability.DefaultTileCacheID != ready.ID {
		t.Fatalf("default_vector_tile_cache_id = %#v, want %d", capability.DefaultTileCacheID, ready.ID)
	}
	if capability.CanGenerateTileCache || capability.TileCacheGeneration.Available {
		t.Fatalf("tile generation = can:%v available:%v, want false after ready tile cache", capability.CanGenerateTileCache, capability.TileCacheGeneration.Available)
	}
	assertNoActions(t, capability.AvailableActions, QuickViewActionGenerateTileCache)
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

func TestQuickViewCapabilityFallsBackToDirectFlatGeobufAfterDefaultTileCacheResultDeleted(t *testing.T) {
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
		t.Fatal("default_vector_tile_cache_id is nil before delete, want ready tile cache result")
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
		t.Fatalf("can_use_quick_view = false after delete, want direct FlatGeobuf fallback; reason=%s", capability.UnavailableReason)
	}
	if capability.RenderSource != QuickViewRenderSourceDirectFlatGeobuf {
		t.Fatalf("render_source = %s, want %s", capability.RenderSource, QuickViewRenderSourceDirectFlatGeobuf)
	}
	if capability.DefaultTileCacheID != nil {
		t.Fatalf("default_vector_tile_cache_id = %#v, want nil after deleted tile cache result fallback", capability.DefaultTileCacheID)
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
		t.Fatalf("default_vector_tile_cache_id = %#v, want latest ready tile cache result %d", capability.DefaultTileCacheID, latestReady.ID)
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
	if err := db.Create(&models.VectorMaterializedView{
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
		TargetKind:                models.VectorMaterializedViewTargetKindSourceSchemaMaterializedView,
		TargetSchema:              "public",
		TargetTable:               "addp_vmv_ready",
		TargetGeometryColumn:      models.VectorMaterializedViewTargetGeometryColumn,
		Status:                    models.VectorMaterializedViewStatusReady,
		RenderExtent:              datatypes.JSON([]byte(`[100.1,20.2,101.3,21.4]`)),
		RenderExtentSRID:          &extentSRID,
		SourceFingerprintSnapshot: commonModels.JSONMap{},
		Metadata:                  commonModels.JSONMap{"index_name": "idx_addp_vmv_ready_geom_3857_gist"},
	}).Error; err != nil {
		t.Fatalf("create vector materialized view result: %v", err)
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
			Schema:                       "public",
			Table:                        "addp_vmv_ready",
			GeomColumn:                   "geom_3857",
			SRID:                         3857,
			VectorMaterializedViewTarget: true,
		},
	})
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}
	if capability.Optimization == nil || !capability.Optimization.Available {
		t.Fatalf("optimization diagnostic = %#v, want available", capability.Optimization)
	}
	if capability.Optimization.TargetTable != "addp_vmv_ready" {
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
	if err := db.Create(&models.VectorMaterializedView{
		TenantID:                  1,
		ItemFingerprint:           fingerprint,
		Locator:                   tableLocator(11, "public", "roads"),
		SourceEngineID:            11,
		SourceSchema:              "public",
		SourceTable:               "roads",
		SourceGeometryColumn:      "shape_a",
		SourceSRID:                4326,
		TargetSRID:                3857,
		TargetKind:                models.VectorMaterializedViewTargetKindSourceSchemaMaterializedView,
		TargetSchema:              "public",
		TargetTable:               "addp_vmv_shape_a",
		TargetGeometryColumn:      models.VectorMaterializedViewTargetGeometryColumn,
		Status:                    models.VectorMaterializedViewStatusReady,
		SourceFingerprintSnapshot: commonModels.JSONMap{},
		Metadata:                  commonModels.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("create vector materialized view result: %v", err)
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
			Schema:                       "public",
			Table:                        "roads",
			GeomColumn:                   "shape_b",
			SRID:                         4326,
			VectorMaterializedViewTarget: false,
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

func TestQuickViewCapabilityReportsStaleOptimizationWhenSourceSRIDChanged(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	fingerprint := spatialItemFingerprint(11, "public", "roads")
	if err := db.Create(&models.VectorMaterializedView{
		TenantID:                  1,
		ItemFingerprint:           fingerprint,
		Locator:                   tableLocator(11, "public", "roads"),
		SourceEngineID:            11,
		SourceSchema:              "public",
		SourceTable:               "roads",
		SourceGeometryColumn:      "shape",
		SourceSRID:                4326,
		TargetSRID:                3857,
		TargetKind:                models.VectorMaterializedViewTargetKindSourceSchemaMaterializedView,
		TargetSchema:              "public",
		TargetTable:               "addp_vmv_roads",
		TargetGeometryColumn:      models.VectorMaterializedViewTargetGeometryColumn,
		Status:                    models.VectorMaterializedViewStatusReady,
		SourceFingerprintSnapshot: commonModels.JSONMap{},
		Metadata:                  commonModels.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("create vector materialized view result: %v", err)
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
			SRID:            4490,
			Extent:          []float64{100, 20, 101, 21},
			ExtentSRID:      4490,
			RecordCount:     100000,
		},
		CanTile: true,
		RealtimeTileTarget: &RealtimeTileTarget{
			Schema:                     "public",
			Table:                      "roads",
			GeomColumn:                 "shape",
			SRID:                       4490,
			PerformanceMode:            RealtimeTilePerformanceSourceTransform,
			OptimizationRecommended:    true,
			OptimizationRecommendation: RealtimeTileRecommendationVectorMaterializedView,
		},
	})
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}
	if capability.Optimization == nil {
		t.Fatal("optimization diagnostic is nil")
	}
	if capability.Optimization.Available ||
		capability.Optimization.Status != models.VectorMaterializedViewStatusStale ||
		capability.Optimization.Reason != models.VectorMaterializedViewStaleReasonSourceFactsChanged {
		t.Fatalf("optimization diagnostic = %#v, want stale source facts changed", capability.Optimization)
	}
	var stored models.VectorMaterializedView
	if err := db.First(&stored, "id = ?", *capability.Optimization.ResultID).Error; err != nil {
		t.Fatalf("load stored optimization result: %v", err)
	}
	if stored.Status != models.VectorMaterializedViewStatusStale ||
		stored.ErrorMessage != models.VectorMaterializedViewStaleReasonSourceFactsChanged {
		t.Fatalf("stored optimization status=%s error=%q, want stale source facts changed", stored.Status, stored.ErrorMessage)
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
			Schema:                       "public",
			Table:                        "dltb_3857",
			GeomColumn:                   "geom_3857",
			SRID:                         3857,
			VectorMaterializedViewTarget: true,
			PerformanceMode:              RealtimeTilePerformanceReady3857Target,
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
	if capability.Optimization.TargetKind != VectorMaterializedViewTargetKindExternal3857MaterializedView {
		t.Fatalf("target_kind = %s, want %s", capability.Optimization.TargetKind, VectorMaterializedViewTargetKindExternal3857MaterializedView)
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

func TestQuickViewServiceUpdatesPreviewState(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewQuickViewService(db, nil)
	identity := QuickViewIdentity{
		TenantID:        7,
		ItemFingerprint: spatialItemFingerprint(26, "public", "buildings"),
		Locator:         tableLocator(26, "public", "buildings"),
	}
	viewState := commonModels.JSONMap{
		"model_3d": map[string]interface{}{
			"camera": "legacy-flat",
		},
		"quick_view": map[string]interface{}{
			"map": map[string]interface{}{
				"center": []interface{}{120.1, 30.2},
				"zoom":   12,
			},
			"scene_3d": map[string]interface{}{
				"camera": map[string]interface{}{
					"position": []interface{}{1, 2, 3},
					"target":   []interface{}{0, 0, 0},
				},
			},
			"gaussian_splat": map[string]interface{}{
				"camera": "legacy-nested",
			},
		},
	}

	if err := svc.UpdateViewStateByIdentity(context.Background(), identity, viewState); err != nil {
		t.Fatalf("update view state: %v", err)
	}

	stored, err := repository.NewPreviewStateRepository(db).GetByIdentity(identity.TenantID, identity.ItemFingerprint, identity.Locator)
	if err != nil {
		t.Fatalf("get preview state: %v", err)
	}
	if stored.PreferredMode != models.PreviewModeBasicPreview {
		t.Fatalf("preferred_mode = %q, want basic_preview", stored.PreferredMode)
	}
	quickViewState, ok := stored.ViewState["quick_view"].(map[string]interface{})
	if !ok {
		t.Fatalf("view_state.quick_view = %#v, want object", stored.ViewState["quick_view"])
	}
	gotMap, ok := quickViewState["map"].(map[string]interface{})
	if !ok {
		t.Fatalf("view_state.quick_view.map = %#v, want object", quickViewState["map"])
	}
	if gotMap["zoom"] != float64(12) {
		t.Fatalf("view_state.quick_view.map.zoom = %#v, want 12", gotMap["zoom"])
	}
	gotScene, ok := quickViewState["scene_3d"].(map[string]interface{})
	if !ok {
		t.Fatalf("view_state.quick_view.scene_3d = %#v, want object", quickViewState["scene_3d"])
	}
	gotCamera, ok := gotScene["camera"].(map[string]interface{})
	if !ok {
		t.Fatalf("view_state.quick_view.scene_3d.camera = %#v, want object", gotScene["camera"])
	}
	if gotCamera["position"] == nil || gotCamera["target"] == nil {
		t.Fatalf("view_state.quick_view.scene_3d.camera = %#v, want position and target", gotCamera)
	}
	if _, ok := stored.ViewState["model_3d"]; ok {
		t.Fatalf("view_state.model_3d should not be persisted: %#v", stored.ViewState)
	}
	if _, ok := quickViewState["gaussian_splat"]; ok {
		t.Fatalf("view_state.quick_view.gaussian_splat should not be persisted: %#v", quickViewState)
	}
}

func TestGaussianSplatProgressiveOrder(t *testing.T) {
	centerFirst := gaussianSplatProgressiveOrder(commonModels.JSONMap{
		"ksplat_facts": map[string]interface{}{
			"converter":           "create_ksplat.mjs",
			"scene_center_source": "sampled_bounds_3d",
		},
	})
	if centerFirst != "center_first" {
		t.Fatalf("progressive order = %q, want center_first", centerFirst)
	}

	sourceOrder := gaussianSplatProgressiveOrder(commonModels.JSONMap{
		"ksplat_facts": map[string]interface{}{
			"converter": "copy",
		},
	})
	if sourceOrder != "source_order" {
		t.Fatalf("progressive order = %q, want source_order", sourceOrder)
	}
}

func TestModel3DTilesCapabilityChecksOperatorsPerTargetFormat(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createModel3DTilesResultTableForTest(t, db)
	server := newWorkflowOperatorServerForTest(t, "osgb_scene_to_3dtiles")

	svc := NewQuickViewService(db, nil)
	svc.SetWorkflowEngineLister(staticWorkflowEngineLister{engines: []commonModels.Engine{workflowEngineForTest(t, server.URL)}})
	capability, err := svc.BuildCapabilityFromSource(context.Background(), model3DTilesQuickViewSource("fp-current", "addp://engine/26/path/site?type=directory"))
	if err != nil {
		t.Fatalf("BuildCapabilityFromSource() error = %v", err)
	}
	formats := model3DTilesFormatsByName(t, capability)
	if !formats[models.Model3DTilesTargetFormat3DTiles].CanGenerate {
		t.Fatalf("3d_tiles capability = %#v, want can_generate=true", formats[models.Model3DTilesTargetFormat3DTiles])
	}
	if got := formats[models.Model3DTilesTargetFormatS3M]; got.CanGenerate || got.UnavailableReason != "operator_unavailable" {
		t.Fatalf("s3m capability = %#v, want operator_unavailable", got)
	}
	assertActions(t, capability.AvailableActions, QuickViewActionGenerateModel3D3DTiles)
	assertNoActions(t, capability.AvailableActions, QuickViewActionGenerateModel3DS3M)
}

func TestModel3DTilesCapabilityReturnsIndependentReadyResultsWhenEngineDiscoveryFails(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createModel3DTilesResultTableForTest(t, db)
	locator := "addp://engine/26/path/site?type=directory"
	execution3DTiles := "exec-3d-tiles"
	executionS3M := "exec-s3m"
	results := []*models.Model3DTiles{
		{TenantID: 7, ItemFingerprint: "fp-ready", Locator: locator, SourceEngineID: 26, SourceFormat: "osgb_scene", TargetFormat: models.Model3DTilesTargetFormat3DTiles, StorageRef: "3d-ref", ManifestRef: "tileset.json", Status: models.Model3DTilesStatusReady, LastExecutionID: &execution3DTiles, Metadata: commonModels.JSONMap{}},
		{TenantID: 7, ItemFingerprint: "fp-ready", Locator: locator, SourceEngineID: 26, SourceFormat: "osgb_scene", TargetFormat: models.Model3DTilesTargetFormatS3M, StorageRef: "s3m-ref", ManifestRef: "config/scene.scp", Status: models.Model3DTilesStatusReady, LastExecutionID: &executionS3M, Metadata: commonModels.JSONMap{
			"tiles_facts": commonModels.JSONMap{
				"manifest_encoding": "json", "tile_extension": ".s3mb", "texture_compression": "dxt",
				"geometry_compression": "draco", "s3m_version": "3.01",
			},
		}},
	}
	for _, result := range results {
		if err := db.Create(result).Error; err != nil {
			t.Fatalf("create model3d tiles result: %v", err)
		}
	}

	svc := NewQuickViewService(db, nil)
	svc.SetWorkflowEngineLister(staticWorkflowEngineLister{err: fmt.Errorf("system unavailable")})
	capability, err := svc.BuildCapabilityFromSource(context.Background(), model3DTilesQuickViewSource("fp-ready", locator))
	if err != nil {
		t.Fatalf("BuildCapabilityFromSource() error = %v", err)
	}
	formats := model3DTilesFormatsByName(t, capability)
	for _, targetFormat := range []string{models.Model3DTilesTargetFormat3DTiles, models.Model3DTilesTargetFormatS3M} {
		formatInfo := formats[targetFormat]
		if formatInfo.Status != models.Model3DTilesStatusReady || formatInfo.PreviewURL == "" || formatInfo.UnavailableReason != "result_ready" {
			t.Fatalf("%s capability = %#v, want independent ready preview", targetFormat, formatInfo)
		}
	}
	if !capability.CanUseQuickView || capability.RenderSource != "model3d_3d_tiles" {
		t.Fatalf("capability = %#v, want 3D Tiles as deterministic default ready renderer", capability)
	}
	if !strings.Contains(formats[models.Model3DTilesTargetFormatS3M].PreviewURL, "/config/scene.scp") {
		t.Fatalf("s3m preview_url = %q, want scene.scp", formats[models.Model3DTilesTargetFormatS3M].PreviewURL)
	}
	if !strings.Contains(formats[models.Model3DTilesTargetFormatS3M].PreviewURL, "version=exec-s3m") {
		t.Fatalf("s3m preview_url = %q, want execution version", formats[models.Model3DTilesTargetFormatS3M].PreviewURL)
	}
	s3m := formats[models.Model3DTilesTargetFormatS3M]
	if s3m.ManifestEncoding != "json" || s3m.TileExtension != ".s3mb" || s3m.TextureCompression != "dxt" ||
		s3m.GeometryCompression != "draco" || s3m.S3MVersion != "3.01" {
		t.Fatalf("s3m artifact facts = %#v, want json/.s3mb/dxt/draco/3.01", s3m)
	}
}

func TestModel3DTilesCapabilityKeepsReadyFormatAvailableWhileOtherFormatBuilds(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createModel3DTilesResultTableForTest(t, db)
	locator := "addp://engine/26/path/site?type=directory"
	for _, result := range []*models.Model3DTiles{
		{TenantID: 7, ItemFingerprint: "fp-mixed", Locator: locator, SourceEngineID: 26, SourceFormat: "osgb_scene", TargetFormat: models.Model3DTilesTargetFormat3DTiles, StorageRef: "3d-ref", ManifestRef: "tileset.json", Status: models.Model3DTilesStatusReady, Metadata: commonModels.JSONMap{}},
		{TenantID: 7, ItemFingerprint: "fp-mixed", Locator: locator, SourceEngineID: 26, SourceFormat: "osgb_scene", TargetFormat: models.Model3DTilesTargetFormatS3M, StorageRef: "s3m-ref", ManifestRef: "config/scene.scp", Status: models.Model3DTilesStatusBuilding, Metadata: commonModels.JSONMap{}},
	} {
		if err := db.Create(result).Error; err != nil {
			t.Fatalf("create model3d tiles result: %v", err)
		}
	}

	capability, err := NewQuickViewService(db, nil).BuildCapabilityFromSource(context.Background(), model3DTilesQuickViewSource("fp-mixed", locator))
	if err != nil {
		t.Fatalf("BuildCapabilityFromSource() error = %v", err)
	}
	formats := model3DTilesFormatsByName(t, capability)
	if !capability.CanUseQuickView || formats[models.Model3DTilesTargetFormat3DTiles].PreviewURL == "" {
		t.Fatalf("capability = %#v, want ready 3D Tiles preview", capability)
	}
	if got := formats[models.Model3DTilesTargetFormatS3M]; got.Status != models.Model3DTilesStatusBuilding || got.UnavailableReason != "generation_running" {
		t.Fatalf("s3m capability = %#v, want generation_running", got)
	}
}

func TestModel3DTilesCapabilityMarksPreviousFingerprintResultsStale(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	createModel3DTilesResultTableForTest(t, db)
	locator := "addp://engine/26/path/site?type=directory"
	oldResult := &models.Model3DTiles{TenantID: 7, ItemFingerprint: "fp-old", Locator: locator, SourceEngineID: 26, SourceFormat: "osgb_scene", TargetFormat: models.Model3DTilesTargetFormat3DTiles, StorageRef: "old-ref", ManifestRef: "tileset.json", Status: models.Model3DTilesStatusReady, Metadata: commonModels.JSONMap{}}
	if err := db.Create(oldResult).Error; err != nil {
		t.Fatalf("create old model3d tiles result: %v", err)
	}

	if _, err := NewQuickViewService(db, nil).BuildCapabilityFromSource(context.Background(), model3DTilesQuickViewSource("fp-new", locator)); err != nil {
		t.Fatalf("BuildCapabilityFromSource() error = %v", err)
	}
	var stored models.Model3DTiles
	if err := db.First(&stored, oldResult.ID).Error; err != nil {
		t.Fatalf("reload old model3d tiles result: %v", err)
	}
	if stored.Status != models.Model3DTilesStatusStale {
		t.Fatalf("old result status = %q, want stale", stored.Status)
	}
}

type staticWorkflowEngineLister struct {
	engines []commonModels.Engine
	err     error
}

func (l staticWorkflowEngineLister) ListWorkflowEngines(uint) ([]commonModels.Engine, error) {
	return l.engines, l.err
}

func model3DTilesQuickViewSource(fingerprint, locator string) QuickViewSource {
	return QuickViewSource{
		Identity: QuickViewIdentity{TenantID: 7, ItemFingerprint: fingerprint, Locator: locator},
		EngineID: 26,
		Model3D:  &Model3DGLBSource{Format: "osgb_scene", Layout: "whole", SourceSizeBytes: 1024},
	}
}

func createModel3DTilesResultTableForTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`CREATE TABLE manager.model3d_tiles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		item_id INTEGER,
		locator TEXT,
		task_id INTEGER,
		last_execution_id TEXT,
		source_engine_id INTEGER NOT NULL,
		source_format TEXT NOT NULL,
		source_size_bytes INTEGER,
		target_format TEXT NOT NULL,
		storage_ref TEXT NOT NULL,
		manifest_ref TEXT NOT NULL,
		file_count INTEGER,
		size_bytes INTEGER,
		status TEXT NOT NULL,
		metadata JSON,
		error_message TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model3d_tiles table: %v", err)
	}
}

func newWorkflowOperatorServerForTest(t *testing.T, operatorNames ...string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/operators":
			operators := make([]map[string]interface{}, 0, len(operatorNames))
			for _, name := range operatorNames {
				operators = append(operators, map[string]interface{}{
					"id": name, "name": name, "display_name": name, "engine_type": "quick_view_test_workflow",
					"type": "model_3d", "category": "Model 3D", "category_path": []string{"Model 3D"},
					"description": "Model 3D test operator", "parameters": []map[string]interface{}{},
					"output_ports":    []map[string]interface{}{{"name": "default", "type": "object", "is_default": true}},
					"execution_modes": []string{"workflow", "direct"},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"operators": operators})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func workflowEngineForTest(t *testing.T, rawURL string) commonModels.Engine {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse workflow server URL: %v", err)
	}
	capabilities := commonModels.JSONString(`{"schema_version":"engine.capabilities/v1","engine_type":"quick_view_test_workflow","engine_family":"workflow","compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1","dynamic_operators":true}}}`)
	return commonModels.Engine{
		ID: 1, Name: "Quick View Test Workflow", EngineType: "quick_view_test_workflow", EngineOrigin: "extension", IsActive: true,
		ConnectionInfo: commonModels.ConnectionInfo{"host": parsed.Hostname(), "port": parsed.Port(), "protocol": parsed.Scheme},
		Capabilities:   &capabilities,
	}
}

func model3DTilesFormatsByName(t *testing.T, capability *QuickViewCapability) map[string]QuickViewModel3DTilesFormatInfo {
	t.Helper()
	if capability == nil || capability.Model3DTiles == nil || len(capability.Model3DTiles.Formats) != 2 {
		t.Fatalf("model3d_tiles capability = %#v, want two target formats", capability)
	}
	formats := make(map[string]QuickViewModel3DTilesFormatInfo, len(capability.Model3DTiles.Formats))
	for _, formatInfo := range capability.Model3DTiles.Formats {
		formats[formatInfo.TargetFormat] = formatInfo
	}
	return formats
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func assertActions(t *testing.T, actions []string, expected ...string) {
	t.Helper()
	for _, action := range expected {
		if !containsString(actions, action) {
			t.Fatalf("available_actions = %#v, want %q", actions, action)
		}
	}
}

func assertNoActions(t *testing.T, actions []string, unexpected ...string) {
	t.Helper()
	for _, action := range unexpected {
		if containsString(actions, action) {
			t.Fatalf("available_actions = %#v, did not want %q", actions, action)
		}
	}
}
