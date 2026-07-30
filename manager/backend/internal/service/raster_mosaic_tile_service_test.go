package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
)

type fakeRasterMosaicMetaClient struct {
	item     *commonModels.MetaItem
	tenantID *uint
}

func (f *fakeRasterMosaicMetaClient) GetItemByIDForTenant(tenantID, itemID uint) (*commonModels.MetaItem, error) {
	f.tenantID = &tenantID
	if f.item == nil || f.item.ID != itemID {
		return nil, nil
	}
	return f.item, nil
}

type fakeRasterMosaicSystemClient struct {
	engine *commonModels.Engine
}

func (f fakeRasterMosaicSystemClient) GetEngine(engineID uint) (*commonModels.Engine, error) {
	if f.engine == nil || f.engine.ID != engineID {
		return nil, nil
	}
	return f.engine, nil
}

type fakeRasterMosaicRuntime struct {
	request RasterMosaicRuntimeRenderRequest
}

func (f *fakeRasterMosaicRuntime) RenderTile(_ context.Context, req RasterMosaicRuntimeRenderRequest) (*RasterMosaicRuntimeTile, error) {
	f.request = req
	return &RasterMosaicRuntimeTile{Data: []byte("tile"), ContentType: "image/png", Source: "leaf"}, nil
}

func TestRasterMosaicTileServiceRenderTileBuildsRuntimeRequest(t *testing.T) {
	tenantID := uint(7)
	runtime := &fakeRasterMosaicRuntime{}
	svc := NewRasterMosaicTileService(
		fakeRasterMosaicSystemClient{engine: &commonModels.Engine{
			ID:             26,
			TenantID:       &tenantID,
			EngineType:     "nfs",
			LifecycleState: "active",
			ConnectionInfo: commonModels.ConnectionInfo{
				"mount_path": "/data",
			},
		}},
		&fakeRasterMosaicMetaClient{item: &commonModels.MetaItem{
			ID:       99,
			TenantID: 7,
			EngineID: 26,
			FullName: "mosaics/srtm",
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"layout":    "whole",
					"data_type": "media",
					"format":    "raster_mosaic",
				},
				"format_info": map[string]interface{}{
					"raster_mosaic": map[string]interface{}{
						"manifest_ref": "mosaic.addp.json",
						"index_ref":    "index/source-index.json",
						"overview_ref": "overviews/overview.cog.tif",
					},
				},
			},
		}},
		runtime,
		256,
	)
	displayMin := 10.0
	displayMax := 4200.0

	tile, err := svc.RenderTile(context.Background(), RasterMosaicTileRequest{
		Locator:    "addp://engine/26/path/mosaics/srtm?type=directory&item_id=99",
		TenantID:   &tenantID,
		Z:          3,
		X:          4,
		Y:          5,
		Format:     "png",
		Gamma:      0.7,
		DisplayMin: &displayMin,
		DisplayMax: &displayMax,
		Invert:     true,
	})
	if err != nil {
		t.Fatalf("RenderTile() error = %v", err)
	}
	if string(tile.Data) != "tile" || tile.ContentType != "image/png" || tile.Source != "leaf" {
		t.Fatalf("tile = %#v, want fake png tile", tile)
	}
	if runtime.request.Dataset.DatasetRootURI != "/data/mosaics/srtm" {
		t.Fatalf("dataset_root_uri = %q, want /data/mosaics/srtm", runtime.request.Dataset.DatasetRootURI)
	}
	if runtime.request.Dataset.OverviewRef != "overviews/overview.cog.tif" ||
		runtime.request.Dataset.ManifestRef != "mosaic.addp.json" ||
		runtime.request.Dataset.IndexRef != "index/source-index.json" {
		t.Fatalf("dataset refs = %#v", runtime.request.Dataset)
	}
	if len(runtime.request.Dataset.GDALEnv) != 0 {
		t.Fatalf("gdal_env = %#v, want empty for mounted nfs path", runtime.request.Dataset.GDALEnv)
	}
	if runtime.request.Tile.Z != 3 || runtime.request.Tile.X != 4 || runtime.request.Tile.Y != 5 || runtime.request.Tile.TileSize != 256 {
		t.Fatalf("tile request = %#v", runtime.request.Tile)
	}
	if strings.ToLower(runtime.request.Render.Format) != "png" {
		t.Fatalf("render format = %q, want png", runtime.request.Render.Format)
	}
	if runtime.request.Render.Gamma != 0.7 {
		t.Fatalf("render gamma = %v, want 0.7", runtime.request.Render.Gamma)
	}
	if runtime.request.Render.DisplayMin == nil || *runtime.request.Render.DisplayMin != displayMin ||
		runtime.request.Render.DisplayMax == nil || *runtime.request.Render.DisplayMax != displayMax ||
		!runtime.request.Render.Invert {
		t.Fatalf("render style = %#v, want display range and invert", runtime.request.Render)
	}
}

func TestRasterMosaicTileServiceUsesDefaultGamma(t *testing.T) {
	tenantID := uint(7)
	runtime := &fakeRasterMosaicRuntime{}
	svc := NewRasterMosaicTileService(
		fakeRasterMosaicSystemClient{engine: &commonModels.Engine{
			ID:             26,
			TenantID:       &tenantID,
			EngineType:     "nfs",
			LifecycleState: "active",
			ConnectionInfo: commonModels.ConnectionInfo{
				"mount_path": "/data",
			},
		}},
		&fakeRasterMosaicMetaClient{item: &commonModels.MetaItem{
			ID:       99,
			TenantID: 7,
			EngineID: 26,
			FullName: "mosaics/srtm",
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"layout":    "whole",
					"data_type": "media",
					"format":    "raster_mosaic",
				},
				"format_info": map[string]interface{}{
					"raster_mosaic": map[string]interface{}{
						"manifest_ref": "mosaic.addp.json",
						"index_ref":    "index/source-index.json",
						"overview_ref": "overviews/overview.cog.tif",
					},
				},
			},
		}},
		runtime,
		256,
	)

	_, err := svc.RenderTile(context.Background(), RasterMosaicTileRequest{
		Locator:  "addp://engine/26/path/mosaics/srtm?type=directory&item_id=99",
		TenantID: &tenantID,
		Z:        3,
		X:        4,
		Y:        5,
		Format:   "png",
	})
	if err != nil {
		t.Fatalf("RenderTile() error = %v", err)
	}
	if runtime.request.Render.Gamma != DefaultRasterMosaicGamma {
		t.Fatalf("render gamma = %v, want default %v", runtime.request.Render.Gamma, DefaultRasterMosaicGamma)
	}
}

func TestHTTPRasterMosaicRuntimeClientReadsTileSourceHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/raster-mosaic/render-tile" {
			t.Fatalf("runtime path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/webp")
		w.Header().Set("X-ADDP-Mosaic-Tile-Source", "leaf")
		_, _ = w.Write([]byte("tile"))
	}))
	defer server.Close()

	client := NewHTTPRasterMosaicRuntimeClient(server.URL, "", 0)
	tile, err := client.RenderTile(context.Background(), RasterMosaicRuntimeRenderRequest{
		Dataset: RasterMosaicRuntimeDataset{DatasetRootURI: "/data/mosaic", OverviewRef: "overviews/overview.cog.tif"},
		Tile:    RasterMosaicRuntimeTileReq{Z: 1, X: 0, Y: 0, TileSize: 256},
		Render:  RasterMosaicRuntimeOptions{Format: "webp"},
	})
	if err != nil {
		t.Fatalf("RenderTile() error = %v", err)
	}
	if string(tile.Data) != "tile" || tile.ContentType != "image/webp" || tile.Source != "leaf" {
		t.Fatalf("tile = %#v, want webp leaf tile", tile)
	}
}
