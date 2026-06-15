package mvt

import (
	"context"
	"errors"
	"testing"

	commonModels "github.com/addp/common/models"
)

func TestResolveTileRangeExtentUsesConfiguredWGS84Extent(t *testing.T) {
	cfg := QuickViewConfig{
		Extent:     []float64{120, 30, 121, 31},
		ExtentSRID: 4326,
	}

	got, err := resolveTileRangeExtentWGS84(context.Background(), cfg, func(context.Context, uint, uint, string, string, string) ([]float64, error) {
		t.Fatal("loader must not be called for WGS84 extent")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("resolve extent: %v", err)
	}
	if len(got) != 4 || got[0] != 120 || got[3] != 31 {
		t.Fatalf("extent = %#v, want configured WGS84 extent", got)
	}
}

func TestResolveTileRangeExtentLoadsWGS84ExtentForProjectedSource(t *testing.T) {
	cfg := QuickViewConfig{
		EngineID:   8,
		TenantID:   1,
		Schema:     "public",
		Table:      "test",
		GeomColumn: "SmGeometry",
		Extent:     []float64{570841.0277, 3404864.0397, 598936.5143, 3434951.8803},
		ExtentSRID: 4549,
	}
	want := []float64{110.4, 30.7, 110.7, 31.0}

	got, err := resolveTileRangeExtentWGS84(context.Background(), cfg, func(_ context.Context, engineID, tenantID uint, schema, table, geomColumn string) ([]float64, error) {
		if engineID != 8 || tenantID != 1 || schema != "public" || table != "test" || geomColumn != "SmGeometry" {
			t.Fatalf("loader args = %d/%d/%s/%s/%s", engineID, tenantID, schema, table, geomColumn)
		}
		return want, nil
	})
	if err != nil {
		t.Fatalf("resolve extent: %v", err)
	}
	if got[0] != want[0] || got[3] != want[3] {
		t.Fatalf("extent = %#v, want %#v", got, want)
	}

	_, minY, _, _ := calculateTileBounds(got, 12)
	if minY == 0 {
		t.Fatalf("tile y = %d, want non-zero row for China WGS84 extent", minY)
	}
}

func TestResolveTileRangeExtentRequiresKnownSRID(t *testing.T) {
	_, err := resolveTileRangeExtentWGS84(context.Background(), QuickViewConfig{
		Extent: []float64{570841.0277, 3404864.0397, 598936.5143, 3434951.8803},
	}, nil)
	if err == nil {
		t.Fatal("resolve extent succeeded, want extent_srid error")
	}
}

func TestResolveTileRangeExtentReturnsLoaderError(t *testing.T) {
	wantErr := errors.New("postgis unavailable")
	_, err := resolveTileRangeExtentWGS84(context.Background(), QuickViewConfig{
		Extent:     []float64{570841.0277, 3404864.0397, 598936.5143, 3434951.8803},
		ExtentSRID: 4549,
	}, func(context.Context, uint, uint, string, string, string) ([]float64, error) {
		return nil, wantErr
	})
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("resolve error = %v, want %v", err, wantErr)
	}
}

func TestTileGenerationSourceFromQuickViewConfigBuildsTileParams(t *testing.T) {
	optimization := commonModels.DefaultOptimizationConfig()
	cfg := QuickViewConfig{
		EngineID:           8,
		TenantID:           1,
		Schema:             "public",
		Table:              "yanshi",
		GeomColumn:         "SmGeometry",
		SRID:               4549,
		PrimaryKey:         "SmID",
		MaxZoom:            12,
		OptimizationConfig: &optimization,
	}

	params := TileGenerationSourceFromQuickViewConfig(cfg).Params(TileCoord{Z: 10, X: 855, Y: 419})

	if params.EngineID != cfg.EngineID || params.TenantID != cfg.TenantID {
		t.Fatalf("params engine/tenant = %d/%d, want %d/%d", params.EngineID, params.TenantID, cfg.EngineID, cfg.TenantID)
	}
	if params.Schema != cfg.Schema || params.Table != cfg.Table || params.GeomColumn != cfg.GeomColumn {
		t.Fatalf("params source = %s.%s/%s, want %s.%s/%s",
			params.Schema, params.Table, params.GeomColumn, cfg.Schema, cfg.Table, cfg.GeomColumn)
	}
	if params.SRID != cfg.SRID || params.PrimaryKey != cfg.PrimaryKey || params.MaxZoom != cfg.MaxZoom {
		t.Fatalf("params srid/pk/max_zoom = %d/%s/%d, want %d/%s/%d",
			params.SRID, params.PrimaryKey, params.MaxZoom, cfg.SRID, cfg.PrimaryKey, cfg.MaxZoom)
	}
	if params.Z != 10 || params.X != 855 || params.Y != 419 {
		t.Fatalf("params tile = %d/%d/%d, want 10/855/419", params.Z, params.X, params.Y)
	}
	if params.OptimizationConfig != &optimization {
		t.Fatal("optimization config was not preserved")
	}
}
