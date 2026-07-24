package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	commonPMTiles "github.com/addp/common/pmtiles"
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
		if r.URL.Path != "/api/v1/internal/engines/26" {
			t.Fatalf("unexpected system path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":26,"tenant_id":7,"name":"Business NFS","engine_type":"nfs","connection_info":{"mount_path":"` + targetRoot + `"},"is_active":true}`))
	}))
	defer systemServer.Close()

	workflowCalls := 0
	executor := NewManagerVectorTileSetExecutor(
		commonClient.NewSystemClientWithInternalKey(systemServer.URL, "internal-key"),
		recordingWorkflowLister{onList: func() { workflowCalls++ }}, nil, "http://manager:8081", "internal-key", 0,
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
	if result.CatalogPath != "vector-tiles/roads.pmtiles" {
		t.Fatalf("catalog path = %q", result.CatalogPath)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "vector-tiles", "roads.pmtiles")); err != nil {
		t.Fatalf("business PMTiles missing: %v", err)
	}
	generatorMetadata, _ := asJSONMap(result.Metadata["generator"])
	if generatorMetadata["kind"] != "postgis_native" {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
}
