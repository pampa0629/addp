package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commonPMTiles "github.com/addp/common/format/pmtiles"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/mvt"
)

type staticPostGISPMTilesGenerator struct {
	config mvt.QuickViewConfig
	calls  int
}

func (g *staticPostGISPMTilesGenerator) Generate(_ context.Context, cfg mvt.QuickViewConfig, _ mvt.ProgressSink) (*mvt.GeneratedPMTilesArchive, error) {
	g.calls++
	g.config = cfg
	path := filepath.Join(os.TempDir(), "addp-vector-tile-set-test-"+cfg.Table+".pmtiles")
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	writer, err := commonPMTiles.NewWriter(commonPMTiles.WriterOptions{Bounds: [4]float64{110, 20, 120, 30}, MinZoom: 0, MaxZoom: 0})
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, _ = zw.Write([]byte{0x1a, 0x02, 0x08, 0x01})
	_ = zw.Close()
	if err := writer.AddTile(0, 0, 0, compressed.Bytes()); err != nil {
		_ = writer.Close()
		_ = file.Close()
		return nil, err
	}
	if _, err := writer.WriteTo(file); err != nil {
		_ = writer.Close()
		_ = file.Close()
		return nil, err
	}
	_ = writer.Close()
	_ = file.Close()
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	headerData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	hash, err := commonPMTiles.HeaderHash(headerData)
	if err != nil {
		return nil, err
	}
	return &mvt.GeneratedPMTilesArchive{
		Path: path, HeaderHash: hash, Size: info.Size(),
		Result: &mvt.GenerateResult{TotalTiles: 1, CachedTiles: 1, GeneratedTiles: 1, ActualMaxZoom: 0, StopReason: "postgis_st_asmvt_pmtiles"},
	}, nil
}

func TestVectorTileSetExecutorUsesPostGISGeneratorWithoutWorkflow(t *testing.T) {
	targetRoot := t.TempDir()
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/engines/26":
			_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Business NFS","engine_type":"nfs","connection_info":{"mount_path":"` + targetRoot + `"},"lifecycle_state":"active","connection_status":"online"}`))
		case "/api/v1/system/engines/11":
			_, _ = w.Write([]byte(`{"id":11,"tenant_id":7,"name":"Business PostGIS","engine_type":"postgresql","connection_info":{},"lifecycle_state":"active","connection_status":"online"}`))
		default:
			t.Fatalf("unexpected system path: %s", r.URL.Path)
		}
	}))
	defer systemServer.Close()

	workflowCalls := 0
	executor := NewManagerVectorTileSetExecutor(
		newTestSystemClient(systemServer.URL),
		recordingWorkflowLister{onList: func() { workflowCalls++ }}, nil, "http://manager:8081", 0,
	)
	generator := &staticPostGISPMTilesGenerator{}
	executor.SetPostGISGenerator(generator, 3)
	result, err := executor.GenerateVectorTileSet(context.Background(), VectorTileSetExecutionRequest{
		Task: &models.VectorTileSetTask{TenantID: 7}, ExecutionID: "set-exec-1",
		Config: VectorTileSetExecutionConfig{
			Source:         tileCacheTaskTargetIdentity{EngineID: 11, SourceKind: "table", Schema: "public", Table: "roads", FullName: "public.roads"},
			TargetEngineID: 26, TargetLocator: "addp://engine/26/path/vector-tiles?type=directory&node_id=4", TargetName: "roads.pmtiles",
			ProfileHash: "profile", Tile: commonModels.JSONMap{"min_zoom": 0, "max_zoom": 0, "source_srid": 4326, "extent": []float64{110, 20, 120, 30}, "extent_srid": 4326},
			Options: commonModels.JSONMap{"geometry_column": "shape", "primary_key": "id", "layer_name": "roads"},
		},
	})
	if err != nil {
		t.Fatalf("GenerateVectorTileSet() error = %v", err)
	}
	if workflowCalls != 0 {
		t.Fatalf("PostGIS vector tile set listed workflow engines %d times", workflowCalls)
	}
	if generator.calls != 1 || generator.config.Concurrency != 3 || generator.config.Schema != "public" {
		t.Fatalf("native generator calls=%d config=%#v", generator.calls, generator.config)
	}
	if result.EngineCatalogPath != "vector-tiles/roads.pmtiles" {
		t.Fatalf("catalog path = %q", result.EngineCatalogPath)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "vector-tiles", "roads.pmtiles")); err != nil {
		t.Fatalf("business PMTiles missing: %v", err)
	}
	generatorMetadata, _ := asJSONMap(result.Metadata["generator"])
	if generatorMetadata["kind"] != "postgis_native" {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
}

func TestVectorTileSetExecutorRoutesDatabaseFlatGeobufEnginesToWorkflow(t *testing.T) {
	for _, engineType := range []string{"mysql", "oracle"} {
		t.Run(engineType, func(t *testing.T) {
			targetRoot := t.TempDir()
			systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/system/engines/26":
					_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Business NFS","engine_type":"nfs","connection_info":{"mount_path":"` + targetRoot + `"},"lifecycle_state":"active","connection_status":"online"}`))
				case "/api/v1/system/engines/11":
					_, _ = w.Write([]byte(`{"id":11,"tenant_id":7,"name":"Database","engine_type":"` + engineType + `","connection_info":{},"lifecycle_state":"active","connection_status":"online"}`))
				default:
					t.Fatalf("unexpected system path: %s", r.URL.Path)
				}
			}))
			defer systemServer.Close()

			executor := NewManagerVectorTileSetExecutor(
				newTestSystemClient(systemServer.URL),
				recordingWorkflowLister{}, nil, "http://manager:8081", 0,
			)
			_, err := executor.GenerateVectorTileSet(context.Background(), VectorTileSetExecutionRequest{
				Task: &models.VectorTileSetTask{TenantID: 7}, ExecutionID: "set-exec-1",
				Config: VectorTileSetExecutionConfig{
					Source:         tileCacheTaskTargetIdentity{EngineID: 11, SourceKind: "table", Schema: "BUSINESS", Table: "ROADS", FullName: "BUSINESS.ROADS"},
					TargetEngineID: 26, TargetLocator: "addp://engine/26/path/vector-tiles?type=directory&node_id=4", TargetName: "roads.pmtiles",
					ProfileHash: "profile", Tile: commonModels.JSONMap{"min_zoom": 0, "max_zoom": 0, "source_srid": 4326, "extent": []float64{110, 20, 120, 30}, "extent_srid": 4326},
					Options: commonModels.JSONMap{"geometry_column": "SHAPE", "primary_key": "ID", "layer_name": "roads"},
				},
			})
			if err == nil || !strings.Contains(err.Error(), "workflow runtime and infra object store are required for database vector tile set generation") {
				t.Fatalf("GenerateVectorTileSet() error = %v, want database workflow unavailable", err)
			}
		})
	}
}

func TestApplyDatabaseFlatGeobufExtentUsesExecutionFactsWhenTaskHasNoExtent(t *testing.T) {
	tile := commonModels.JSONMap{"source_srid": 4326, "min_zoom": 0, "max_zoom": 12}
	sourceFacts := commonModels.JSONMap{"extent": []float64{116.4, 31.2, 121.5, 39.9}, "extent_srid": 4326}
	if err := applyDatabaseFlatGeobufExtent(tile, sourceFacts); err != nil {
		t.Fatalf("applyDatabaseFlatGeobufExtent() error = %v", err)
	}
	extent, ok := floatSliceFromConfig(tile["extent"])
	if !ok || extent[0] != 116.4 || extent[1] != 31.2 || extent[2] != 121.5 || extent[3] != 39.9 {
		t.Fatalf("tile extent = %#v", tile["extent"])
	}
	if got := intFromTileCacheConfig(tile["extent_srid"], 0); got != 4326 {
		t.Fatalf("tile extent_srid = %d", got)
	}
}

func TestApplyDatabaseFlatGeobufExtentPreservesConfirmedTaskExtent(t *testing.T) {
	tile := commonModels.JSONMap{"extent": []float64{110, 20, 120, 30}, "extent_srid": 4326}
	sourceFacts := commonModels.JSONMap{"extent": []float64{116.4, 31.2, 121.5, 39.9}, "extent_srid": 4326}
	if err := applyDatabaseFlatGeobufExtent(tile, sourceFacts); err != nil {
		t.Fatalf("applyDatabaseFlatGeobufExtent() error = %v", err)
	}
	extent, _ := floatSliceFromConfig(tile["extent"])
	if extent[0] != 110 || extent[1] != 20 || extent[2] != 120 || extent[3] != 30 {
		t.Fatalf("confirmed task extent was replaced: %#v", extent)
	}
}
