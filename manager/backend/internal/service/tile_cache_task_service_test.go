package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/tilecache"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTileCacheTaskNormalizesPMTilesProfileAndReusesSemanticIdentity(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewTileCacheRepository(db)
	svc := NewTileCacheTaskService(repo, nil)

	first := newTileCacheTaskDefinition()
	if err := svc.Create(context.Background(), first); err != nil {
		t.Fatalf("create first task: %v", err)
	}
	profileHash := stringFromConfig(first.Config["profile_hash"])
	if len(profileHash) != 64 {
		t.Fatalf("profile_hash = %q, want sha256", profileHash)
	}
	tile, _ := asJSONMap(first.Config["tile"])
	if tile["archive_format"] != "pmtiles" || tile["tile_type"] != "mvt" || intFromTileCacheConfig(tile["target_srid"], 0) != 3857 {
		t.Fatalf("tile config = %#v, want canonical PMTiles/MVT/WebMercator", tile)
	}
	storage, _ := asJSONMap(first.Config["storage"])
	bucket, objectName, err := tilecache.ObjectLocation(stringFromConfig(storage["storage_ref"]), "manager")
	if err != nil {
		t.Fatalf("parse storage_ref: %v", err)
	}
	if bucket != "manager" || !strings.HasSuffix(objectName, "/"+profileHash+".pmtiles") {
		t.Fatalf("storage_ref bucket=%q object=%q", bucket, objectName)
	}

	duplicate := newTileCacheTaskDefinition()
	duplicate.Name = "更新名称"
	if err := svc.Create(context.Background(), duplicate); err != nil {
		t.Fatalf("reuse task: %v", err)
	}
	if duplicate.ID != first.ID || duplicate.Name != "更新名称" {
		t.Fatalf("duplicate = %#v, want reused task %d", duplicate, first.ID)
	}

	differentProfile := newTileCacheTaskDefinition()
	differentTile, _ := asJSONMap(differentProfile.Config["tile"])
	differentTile["max_zoom"] = 2
	if err := svc.Create(context.Background(), differentProfile); err != nil {
		t.Fatalf("create different profile: %v", err)
	}
	if differentProfile.ID == first.ID {
		t.Fatal("different profile reused the same task")
	}
}

func TestTileCacheTaskRejectsRemovedDirectoryTileFormat(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewTileCacheTaskService(repository.NewTileCacheRepository(db), nil)
	task := newTileCacheTaskDefinition()
	tile, _ := asJSONMap(task.Config["tile"])
	tile["format"] = "mvt"
	err := svc.Create(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "tile.format has been removed") {
		t.Fatalf("create error = %v, want removed format rejection", err)
	}
}

func TestTileCacheTaskRejectsScheduleAndLegacyPreparation(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	svc := NewTileCacheTaskService(repository.NewTileCacheRepository(db), nil)

	scheduled := newTileCacheTaskDefinition()
	scheduled.Schedule = "* * * * *"
	if err := svc.Create(context.Background(), scheduled); err == nil || !strings.Contains(err.Error(), "does not support schedule") {
		t.Fatalf("schedule error = %v", err)
	}
	legacy := newTileCacheTaskDefinition()
	legacy.Config["preparation"] = commonModels.JSONMap{"mode": "auto"}
	if err := svc.Create(context.Background(), legacy); err == nil || !strings.Contains(err.Error(), "config.preparation has been removed") {
		t.Fatalf("preparation error = %v", err)
	}
}

func TestPostGISTileCacheGenerationUsesNativePMTilesAndPersistsIdentity(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewTileCacheRepository(db)
	execRepo := commonExecution.NewTaskExecutionRepository(db)
	svc := NewTileCacheTaskService(repo, execRepo)
	generator := &fakeTileCacheGenerator{
		result: &mvt.GenerateResult{
			TotalTiles: 4, CachedTiles: 2, TilesTotalEstimate: 4, TilesProcessed: 4,
			GeneratedTiles: 2, EmptyTiles: 2, TotalSizeBytes: 1024, ActualMaxZoom: 1,
			StopReason: "postgis_st_asmvt_pmtiles", GenerationSec: 0.5,
			ExtentWGS84:       []float64{110, 20, 120, 30},
			ArchiveHeaderHash: strings.Repeat("a", 64), ArchiveSizeBytes: 2048,
		},
	}
	workflow := &fakeWorkflowTileCacheGenerator{}
	svc.SetTileGenerator(generator, 3)
	svc.SetWorkflowTileGenerator(workflow)
	svc.SetSourceVersionResolver(func(context.Context, uint, tileCacheTaskTargetIdentity) (string, error) {
		return strings.Repeat("b", 64), nil
	})

	task := newTileCacheTaskDefinition()
	if err := svc.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	executionID, err := svc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, execRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %s, error=%#v", exec.Status, exec.ErrorDetails)
	}
	if generator.calls != 1 || generator.lastConfig.Schema != "public" || generator.lastConfig.Table != "roads" || generator.lastProgress == nil {
		t.Fatalf("native generator calls=%d config=%#v", generator.calls, generator.lastConfig)
	}
	if generator.lastConfig.Concurrency != 3 || generator.lastConfig.LayerName != "roads" {
		t.Fatalf("native config = %#v", generator.lastConfig)
	}
	if workflow.calls != 0 {
		t.Fatalf("PostGIS table unexpectedly invoked workflow %d times", workflow.calls)
	}
	profileHash := stringFromConfig(task.Config["profile_hash"])
	artifact, err := repo.GetTileCacheByFingerprintAndProfile(context.Background(), task.TenantID, spatialItemFingerprint(11, "public", "roads"), profileHash)
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}
	if artifact == nil || artifact.Status != models.TileCacheStatusReady || artifact.SourceVersion != strings.Repeat("b", 64) || artifact.ProfileHash != profileHash {
		t.Fatalf("artifact = %#v", artifact)
	}
	if _, _, err := tilecache.ObjectLocation(artifact.StorageRef, "manager"); err != nil {
		t.Fatalf("artifact storage_ref: %v", err)
	}
}

func TestObjectTileCacheGenerationUsesWorkflow(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewTileCacheRepository(db)
	execRepo := commonExecution.NewTaskExecutionRepository(db)
	svc := NewTileCacheTaskService(repo, execRepo)
	native := &fakeTileCacheGenerator{}
	workflow := &fakeWorkflowTileCacheGenerator{result: &mvt.GenerateResult{
		TotalTiles: 1, CachedTiles: 1, TilesTotalEstimate: 1, TilesProcessed: 1,
		GeneratedTiles: 1, TotalSizeBytes: 128, ActualMaxZoom: 0,
		StopReason: "workflow_ogr2ogr_pmtiles", ExtentWGS84: []float64{110, 20, 120, 30},
	}}
	svc.SetTileGenerator(native, 4)
	svc.SetWorkflowTileGenerator(workflow)
	svc.SetSourceVersionResolver(func(context.Context, uint, tileCacheTaskTargetIdentity) (string, error) {
		return strings.Repeat("c", 64), nil
	})
	task := newTileCacheTaskDefinition()
	target, _ := asJSONMap(task.Config["target"])
	delete(target, "schema")
	delete(target, "table")
	target["source_engine_id"] = float64(22)
	target["locator"] = "addp://engine/22/path/gis/roads.fgb?type=object&item_id=199"
	target["item_id"] = float64(199)
	if err := svc.Create(context.Background(), task); err != nil {
		t.Fatalf("create object task: %v", err)
	}
	executionID, err := svc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute object task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, execRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %s, error=%#v", exec.Status, exec.ErrorDetails)
	}
	if native.calls != 0 {
		t.Fatalf("object source unexpectedly invoked native generator %d times", native.calls)
	}
	if workflow.calls != 1 || workflow.lastReq.Identity.SourceKind != "object" || workflow.lastReq.Identity.FullName != "gis/roads.fgb" {
		t.Fatalf("workflow calls=%d request=%#v", workflow.calls, workflow.lastReq)
	}
}

type fakeTileCacheGenerator struct {
	result       *mvt.GenerateResult
	err          error
	calls        int
	lastConfig   mvt.QuickViewConfig
	lastProgress mvt.ProgressSink
}

func (g *fakeTileCacheGenerator) GenerateMixed(_ context.Context, cfg mvt.QuickViewConfig, progress mvt.ProgressSink) (*mvt.GenerateResult, error) {
	g.calls++
	g.lastConfig = cfg
	g.lastProgress = progress
	if g.err != nil {
		return nil, g.err
	}
	if g.result == nil {
		return nil, errors.New("fake native PMTiles result is required")
	}
	return g.result, nil
}

func TestTileCacheRecordProgressEventUpdatesExecution(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	execRepo := commonExecution.NewTaskExecutionRepository(db)
	svc := NewTileCacheTaskService(repository.NewTileCacheRepository(db), execRepo)
	startedAt := time.Now().Add(-time.Second)
	if err := execRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: "progress-1", Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeVectorTileCacheGeneration, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusRunning, Progress: 10, TriggerType: commonExecution.TriggerTypeManual,
		StartedAt: &startedAt, CreatedAt: startedAt, UpdatedAt: startedAt,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	overall := 55.0
	if err := svc.RecordProgressEvent(context.Background(), 7, "progress-1", TileCacheProgressEvent{
		Phase: "publish", Event: "progress", Message: "写入 PMTiles", CurrentZoom: 1, MaxZoom: 2,
		TilesProcessed: 55, TilesTotalEstimate: 100, OverallProgress: &overall,
	}); err != nil {
		t.Fatalf("record progress: %v", err)
	}
	exec, err := execRepo.GetByExecutionID(context.Background(), "progress-1", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if exec.Progress != 55 || exec.CurrentStep == nil || *exec.CurrentStep != "写入 PMTiles" {
		t.Fatalf("execution progress=%d step=%v", exec.Progress, exec.CurrentStep)
	}
}

type fakeWorkflowTileCacheGenerator struct {
	result   *mvt.GenerateResult
	metadata commonModels.JSONMap
	err      error
	lastReq  WorkflowTileCacheRequest
	calls    int
}

func (g *fakeWorkflowTileCacheGenerator) GenerateVectorTileCache(_ context.Context, req WorkflowTileCacheRequest) (*mvt.GenerateResult, commonModels.JSONMap, error) {
	g.calls++
	g.lastReq = req
	if g.err != nil {
		return nil, nil, g.err
	}
	if g.result == nil {
		return nil, nil, errors.New("fake PMTiles result is required")
	}
	return g.result, g.metadata, nil
}

func newTileCacheTaskDefinition() *models.TileCacheTask {
	return &models.TileCacheTask{
		TenantID: 7, Name: "瓦片缓存生成", Enabled: true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"source_engine_id": float64(11), "schema": "public", "table": "roads",
				"locator": "addp://engine/11/path/public/roads?type=table&item_id=99", "item_id": float64(99),
			},
			"tile": commonModels.JSONMap{
				"archive_format": "pmtiles", "tile_type": "mvt", "min_zoom": float64(0), "max_zoom": float64(1),
				"source_srid": float64(4326), "target_srid": float64(3857),
				"extent": []interface{}{110.0, 20.0, 120.0, 30.0}, "extent_srid": float64(4326),
			},
			"options": commonModels.JSONMap{"geometry_column": "shape", "primary_key": "id"},
		},
	}
}

func newTileCacheTaskServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	for _, schema := range []string{"common", "manager"} {
		if err := db.Exec("ATTACH DATABASE ':memory:' AS " + schema).Error; err != nil {
			t.Fatalf("attach %s: %v", schema, err)
		}
	}
	statements := []string{
		`CREATE TABLE common.task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, execution_id TEXT NOT NULL,
			module TEXT NOT NULL, task_type TEXT NOT NULL, source TEXT NOT NULL DEFAULT '', source_task_id TEXT,
			source_task_name TEXT, parent_execution_id TEXT, status TEXT NOT NULL, progress INTEGER, current_step TEXT,
			trigger_type TEXT NOT NULL, triggered_by INTEGER, execution_config JSON, error_details JSON, metadata JSON,
			execution_time_ms INTEGER, rows_affected INTEGER, records_read INTEGER, records_written INTEGER,
			bytes_read INTEGER, bytes_written INTEGER, started_at DATETIME, completed_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE manager.vector_tile_cache_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT,
			enabled BOOLEAN, last_execution_id TEXT, last_execution_status TEXT, last_run_at DATETIME, next_run_at DATETIME,
			schedule TEXT, created_by INTEGER, config JSON, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE manager.vector_tile_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL, item_id INTEGER,
			locator TEXT, task_id INTEGER, last_execution_id TEXT, tile_format TEXT NOT NULL, storage_ref TEXT,
			source_version TEXT NOT NULL, profile_hash TEXT NOT NULL, extent JSON, extent_srid INTEGER, min_zoom INTEGER,
			max_zoom INTEGER, status TEXT NOT NULL, error_message TEXT, created_by INTEGER, created_at DATETIME,
			updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE manager.vector_tile_set_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT,
			enabled BOOLEAN, last_execution_id TEXT, last_execution_status TEXT, last_run_at DATETIME, next_run_at DATETIME,
			schedule TEXT, created_by INTEGER, config JSON, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE manager.preview_state (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
			locator TEXT, preferred_mode TEXT NOT NULL DEFAULT 'basic_preview', view_state JSON NOT NULL DEFAULT '{}',
			created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE manager.vector_materialized_view_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT,
			enabled BOOLEAN, last_execution_id TEXT, last_execution_status TEXT, last_run_at DATETIME, next_run_at DATETIME,
			schedule TEXT, created_by INTEGER, config JSON, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE manager.vector_materialized_view (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL, item_id INTEGER,
			locator TEXT, task_id INTEGER, last_execution_id TEXT, source_engine_id INTEGER NOT NULL, source_schema TEXT NOT NULL,
			source_table TEXT NOT NULL, source_geometry_column TEXT NOT NULL, source_srid INTEGER NOT NULL, target_srid INTEGER NOT NULL,
			target_kind TEXT NOT NULL, target_schema TEXT NOT NULL, target_table TEXT NOT NULL, target_geometry_column TEXT NOT NULL,
			status TEXT NOT NULL, render_extent JSON, render_extent_srid INTEGER, row_count_estimate INTEGER,
			source_fingerprint_snapshot JSON, metadata JSON, error_message TEXT, created_by INTEGER,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE manager.raster_cog (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL, item_id INTEGER,
			locator TEXT, task_id INTEGER, last_execution_id TEXT, source_engine_id INTEGER NOT NULL, source_profile TEXT,
			source_size_bytes INTEGER, target_kind TEXT NOT NULL, storage_ref TEXT NOT NULL, file_name TEXT, size_bytes INTEGER,
			width INTEGER, height INTEGER, band_count INTEGER, source_srid INTEGER, source_crs TEXT, extent JSON, extent_srid INTEGER,
			status TEXT NOT NULL, metadata JSON, error_message TEXT, created_by INTEGER, created_at DATETIME,
			updated_at DATETIME, deleted_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	addTaskExecutionAuthorizationColumns(t, db)
	return db
}

func waitForTileCacheTaskExecution(t *testing.T, repo *commonExecution.TaskExecutionRepository, executionID string, tenantID int) *commonExecution.TaskExecution {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		exec, err := repo.GetByExecutionID(context.Background(), executionID, tenantID)
		if err == nil && exec != nil && exec.IsCompleted() {
			return exec
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for tile cache execution")
	return nil
}
