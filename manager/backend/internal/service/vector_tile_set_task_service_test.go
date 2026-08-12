package service

import (
	"context"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

func TestVectorTileSetRecordProgressEventUpdatesExecution(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	startedAt := time.Now().Add(-time.Second)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: "vector-tile-set-progress-1", Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeVectorTileSetGeneration, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusRunning, TriggerType: commonExecution.TriggerTypeManual,
		Progress: 5, StartedAt: &startedAt,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	svc := NewVectorTileSetTaskService(repository.NewVectorTileSetRepository(db), taskExecRepo)
	overallProgress := 42.4
	if err := svc.RecordProgressEvent(context.Background(), 7, "vector-tile-set-progress-1", TileCacheProgressEvent{
		Phase: "publish", Event: "progress", Message: "生成矢量瓦片缓存", CurrentZoom: 10, MaxZoom: 12,
		TilesProcessed: 18, TilesTotalEstimate: 40, OverallProgress: &overallProgress,
	}); err != nil {
		t.Fatalf("record progress event: %v", err)
	}
	exec, err := taskExecRepo.GetByExecutionID(context.Background(), "vector-tile-set-progress-1", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if exec.Progress != 42 {
		t.Fatalf("progress = %d, want 42", exec.Progress)
	}
	if exec.CurrentStep == nil || *exec.CurrentStep != "生成业务矢量瓦片集 z10/12：18/40" {
		t.Fatalf("current_step = %#v", exec.CurrentStep)
	}
}

func TestVectorTileSetMetaScanOptionsUsesDeepScan(t *testing.T) {
	opts := vectorTileSetMetaScanOptions(VectorTileSetExecutionConfig{TargetEngineID: 9}, "addp/vector-tiles/roads.pmtiles")
	if opts.EngineID != 9 || opts.ScanDepth != "deep" || len(opts.CatalogPaths) != 1 || opts.CatalogPaths[0] != "addp/vector-tiles/roads.pmtiles" {
		t.Fatalf("scan options = %#v", opts)
	}
	var _ commonClient.MetaScanOptions = opts
}

func TestVectorTileSetTaskNormalizesBusinessPMTilesConfig(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewVectorTileSetTaskService(repository.NewVectorTileSetRepository(db), commonExecution.NewTaskExecutionRepository(db))
	task := &models.VectorTileSetTask{TenantID: 7, Name: "roads tiles", Enabled: true, Config: commonModels.JSONMap{
		"source":  commonModels.JSONMap{"source_engine_id": 11, "locator": "addp://engine/11/path/public/roads?type=table&item_id=99", "item_id": 99},
		"target":  commonModels.JSONMap{"engine_id": 26, "storage_locator": "addp://engine/26/path/vector-tiles?type=directory&node_id=4", "name": "roads"},
		"tile":    commonModels.JSONMap{"archive_format": "pmtiles", "tile_type": "mvt", "min_zoom": 0, "max_zoom": 12, "source_srid": 4326, "target_srid": 3857, "extent": []float64{110, 20, 120, 30}, "extent_srid": 4326},
		"options": commonModels.JSONMap{"geometry_column": "shape", "layer_name": "roads"},
	}}
	if err := svc.Create(context.Background(), task); err != nil {
		t.Fatalf("create vector tile set task: %v", err)
	}
	if len(stringFromConfig(task.Config["profile_hash"])) != 64 {
		t.Fatalf("profile_hash = %v", task.Config["profile_hash"])
	}
	if len(stringFromConfig(task.Config["semantic_hash"])) != 64 {
		t.Fatalf("semantic_hash = %v", task.Config["semantic_hash"])
	}
	target, _ := asJSONMap(task.Config["target"])
	if target["name"] != "roads.pmtiles" {
		t.Fatalf("target = %#v", target)
	}
	source, _ := asJSONMap(task.Config["source"])
	if stringFromConfig(source["item_fingerprint"]) == "" {
		t.Fatalf("source = %#v", source)
	}
}

func TestVectorTileSetTaskAllowsExecutionTimeExtentForDatabaseSource(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewVectorTileSetTaskService(repository.NewVectorTileSetRepository(db), commonExecution.NewTaskExecutionRepository(db))
	task := &models.VectorTileSetTask{TenantID: 7, Name: "Oracle locations", Enabled: true, Config: commonModels.JSONMap{
		"source":  commonModels.JSONMap{"source_engine_id": 22, "locator": "addp://engine/22/path/BUSINESS/CUSTOMER_LOCATIONS?type=table&item_id=52408", "item_id": 52408},
		"target":  commonModels.JSONMap{"engine_id": 26, "storage_locator": "addp://engine/26/path/vector-tiles?type=directory&node_id=4", "name": "oracle-locations.pmtiles"},
		"tile":    commonModels.JSONMap{"archive_format": "pmtiles", "tile_type": "mvt", "min_zoom": 0, "max_zoom": 12, "source_srid": 4326, "target_srid": 3857},
		"options": commonModels.JSONMap{"geometry_column": "SHAPE", "layer_name": "oracle_locations"},
	}}
	if err := svc.Create(context.Background(), task); err != nil {
		t.Fatalf("create vector tile set task without metadata extent: %v", err)
	}
	tile, _ := asJSONMap(task.Config["tile"])
	if _, exists := tile["extent"]; exists {
		t.Fatalf("task normalization fabricated extent: %#v", tile)
	}
}

func TestVectorTileSetTaskCreateReusesSameBusinessTarget(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewVectorTileSetTaskService(repository.NewVectorTileSetRepository(db), commonExecution.NewTaskExecutionRepository(db))

	first := &models.VectorTileSetTask{TenantID: 7, Name: "roads tiles", Enabled: true, Config: vectorTileSetTaskConfigForTest("roads")}
	if err := svc.Create(context.Background(), first); err != nil {
		t.Fatalf("create first vector tile set task: %v", err)
	}
	second := &models.VectorTileSetTask{TenantID: 7, Name: "renamed roads tiles", Enabled: true, Config: vectorTileSetTaskConfigForTest("roads.pmtiles")}
	if err := svc.Create(context.Background(), second); err != nil {
		t.Fatalf("reuse vector tile set task: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("reused task ID = %d, want %d", second.ID, first.ID)
	}
	items, total, err := svc.List(context.Background(), 7, 1, 20)
	if err != nil {
		t.Fatalf("list vector tile set tasks: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "renamed roads tiles" {
		t.Fatalf("tasks = %#v total=%d, want one reused task", items, total)
	}

	differentTarget := &models.VectorTileSetTask{TenantID: 7, Name: "buildings tiles", Enabled: true, Config: vectorTileSetTaskConfigForTest("buildings")}
	if err := svc.Create(context.Background(), differentTarget); err != nil {
		t.Fatalf("create different target task: %v", err)
	}
	if differentTarget.ID == first.ID {
		t.Fatalf("different business target reused task %d", first.ID)
	}
}

func vectorTileSetTaskConfigForTest(targetName string) commonModels.JSONMap {
	return commonModels.JSONMap{
		"source":  commonModels.JSONMap{"source_engine_id": 11, "locator": "addp://engine/11/path/public/roads?type=table&item_id=99", "item_id": 99},
		"target":  commonModels.JSONMap{"engine_id": 26, "storage_locator": "addp://engine/26/path/vector-tiles?type=directory&node_id=4", "name": targetName},
		"tile":    commonModels.JSONMap{"archive_format": "pmtiles", "tile_type": "mvt", "min_zoom": 0, "max_zoom": 12, "source_srid": 4326, "target_srid": 3857, "extent": []float64{110, 20, 120, 30}, "extent_srid": 4326},
		"options": commonModels.JSONMap{"geometry_column": "shape", "layer_name": "roads"},
	}
}
