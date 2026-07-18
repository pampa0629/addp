package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTileCacheTaskCreateRejectsSchedule(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, nil)

	scheduled := newTileCacheTaskDefinition()
	scheduled.Schedule = "* * * * *"
	if err := taskSvc.Create(context.Background(), scheduled); err == nil || !strings.Contains(err.Error(), "does not support schedule") {
		t.Fatalf("create scheduled tile cache task error = %v, want schedule rejection", err)
	}

	nextRun := newTileCacheTaskDefinition()
	nextRunAt := time.Now().Add(time.Hour)
	nextRun.NextRunAt = &nextRunAt
	if err := taskSvc.Create(context.Background(), nextRun); err == nil || !strings.Contains(err.Error(), "does not support schedule") {
		t.Fatalf("create tile cache task with next_run_at error = %v, want schedule rejection", err)
	}
}

func TestTileCacheTaskCreateNormalizesTargetIdentity(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, nil)

	task := newTileCacheTaskDefinition()
	target, _ := asJSONMap(task.Config["target"])
	target["source_ref"] = commonModels.JSONMap{"ignored": true}
	task.Config["target"] = target

	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	normalized, _ := asJSONMap(task.Config["target"])
	expectedFingerprint := spatialItemFingerprint(11, "public", "roads")
	if normalized["item_fingerprint"] != expectedFingerprint {
		t.Fatalf("item_fingerprint = %v, want %s", normalized["item_fingerprint"], expectedFingerprint)
	}
	if normalized["locator"] != tableLocator(11, "public", "roads") {
		t.Fatalf("locator = %v, want standard table locator", normalized["locator"])
	}
	if _, ok := normalized["source_ref"]; ok {
		t.Fatalf("target.source_ref is still present: %#v", normalized)
	}
	if _, ok := normalized["engine_id"]; ok {
		t.Fatalf("target.engine_id is still present: %#v", normalized)
	}
}

func TestTileCacheTaskCreateReusesSemanticIdentity(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, nil)

	first := newTileCacheTaskDefinition()
	if err := taskSvc.Create(context.Background(), first); err != nil {
		t.Fatalf("create first tile cache task: %v", err)
	}

	duplicate := newTileCacheTaskDefinition()
	duplicate.Name = "重复瓦片缓存生成"
	duplicate.Description = "更新后的配置"
	if err := taskSvc.Create(context.Background(), duplicate); err != nil {
		t.Fatalf("reuse tile cache task: %v", err)
	}
	if duplicate.ID != first.ID {
		t.Fatalf("reused task id = %d, want %d", duplicate.ID, first.ID)
	}
	if duplicate.Name != "重复瓦片缓存生成" || duplicate.Description != "更新后的配置" {
		t.Fatalf("reused task = %#v, want updated mutable fields", duplicate)
	}
	items, total, err := tileCacheRepo.ListTasks(context.Background(), first.TenantID, 1, 20)
	if err != nil {
		t.Fatalf("list tile cache tasks: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("task total=%d len=%d, want one semantic task", total, len(items))
	}

	if err := tileCacheRepo.DeleteTask(context.Background(), first.ID, first.TenantID); err != nil {
		t.Fatalf("delete first tile cache task: %v", err)
	}
	recreated := newTileCacheTaskDefinition()
	if err := taskSvc.Create(context.Background(), recreated); err != nil {
		t.Fatalf("create tile cache task after deleting duplicate: %v", err)
	}
}

func TestTileCacheTaskCreateNormalizesFileTargetIdentity(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, nil)

	itemID := uint(100)
	locator := "addp://engine/26/path/shp/farmland.shp?type=file&item_id=100"
	task := newTileCacheTaskDefinition()
	task.Config["target"] = commonModels.JSONMap{
		"source_engine_id": float64(26),
		"locator":          locator,
		"item_id":          float64(itemID),
	}

	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create file tile cache task: %v", err)
	}

	normalized, _ := asJSONMap(task.Config["target"])
	expectedFingerprint := commonModels.GenerateItemFingerprint(26, "shp/farmland.shp")
	if normalized["item_fingerprint"] != expectedFingerprint {
		t.Fatalf("item_fingerprint = %v, want %s", normalized["item_fingerprint"], expectedFingerprint)
	}
	if normalized["locator"] != locator {
		t.Fatalf("locator = %v, want %s", normalized["locator"], locator)
	}
	if normalized["source_kind"] != "file" {
		t.Fatalf("source_kind = %v, want file", normalized["source_kind"])
	}
	if normalized["full_name"] != "shp/farmland.shp" {
		t.Fatalf("full_name = %v, want shp/farmland.shp", normalized["full_name"])
	}
	if _, ok := normalized["schema"]; ok {
		t.Fatalf("schema is present for file target: %#v", normalized)
	}
	if _, ok := normalized["table"]; ok {
		t.Fatalf("table is present for file target: %#v", normalized)
	}
}

func TestTileCacheTaskCreateRejectsMismatchedFingerprint(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, nil)

	task := newTileCacheTaskDefinition()
	target, _ := asJSONMap(task.Config["target"])
	target["item_fingerprint"] = "bad-fingerprint"
	task.Config["target"] = target

	err := taskSvc.Create(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "item_fingerprint does not match") {
		t.Fatalf("create error = %v, want item_fingerprint mismatch", err)
	}
}

func TestTileCacheTaskCreateRejectsLegacyTargetFields(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, nil)

	task := newTileCacheTaskDefinition()
	task.Config["target"] = commonModels.JSONMap{
		"engine_id":  float64(11),
		"table_name": "roads",
	}
	task.Config["options"] = commonModels.JSONMap{
		"schema":          "public",
		"geometry_column": "shape",
		"primary_key":     "id",
	}

	err := taskSvc.Create(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "source_engine_id and locator") {
		t.Fatalf("create error = %v, want missing standard target fields", err)
	}
}

func TestTileCacheTaskCreateRejectsLegacyPreparationConfig(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, nil)

	task := newTileCacheTaskDefinition()
	task.Config["preparation"] = commonModels.JSONMap{
		"mode":                    "auto",
		"allow_materialized_view": true,
		"allow_index":             true,
	}

	err := taskSvc.Create(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "config.preparation has been removed") {
		t.Fatalf("create error = %v, want legacy preparation rejection", err)
	}
}

func TestTileCacheTaskCreatePreservesDisabledFlag(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, nil)

	task := newTileCacheTaskDefinition()
	task.Enabled = false
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create disabled tile cache task: %v", err)
	}

	refreshed, err := tileCacheRepo.GetTask(context.Background(), task.ID, task.TenantID)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if refreshed.Enabled {
		t.Fatal("enabled = true, want false")
	}
}

func TestTileCacheExecutionRejectsUnnormalizedTarget(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, taskExecRepo)

	task := newTileCacheTaskDefinition()
	target, _ := asJSONMap(task.Config["target"])
	delete(target, "item_fingerprint")
	task.Config["target"] = target
	if err := tileCacheRepo.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("create unnormalized tile cache task: %v", err)
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute tile cache task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("execution status = %s, want failed", exec.Status)
	}
	if !strings.Contains(stringFromConfig(exec.ErrorDetails["message"]), "item_fingerprint is required") {
		t.Fatalf("execution error_details = %#v, want item_fingerprint required", exec.ErrorDetails)
	}
}

func TestTileCacheExecutionRejectsFileTargetWithoutWorkflowGenerator(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, taskExecRepo)

	task := newTileCacheTaskDefinition()
	task.Config["target"] = commonModels.JSONMap{
		"source_engine_id": float64(26),
		"locator":          "addp://engine/26/path/shp/farmland.shp?type=file&item_id=100",
	}
	tile, _ := asJSONMap(task.Config["tile"])
	tile["source_srid"] = float64(4326)
	tile["extent"] = []interface{}{110.0, 20.0, 120.0, 30.0}
	tile["extent_srid"] = float64(4326)
	task.Config["tile"] = tile
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create file tile cache task: %v", err)
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute tile cache task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("execution status = %s, want failed", exec.Status)
	}
	if !strings.Contains(stringFromConfig(exec.ErrorDetails["message"]), "workflow tile cache generation executor is not connected") {
		t.Fatalf("execution error_details = %#v, want workflow generator boundary", exec.ErrorDetails)
	}
}

func TestTileCacheGenerationUsesWorkflowGeneratorForFileTarget(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, taskExecRepo)
	workflowGenerator := &fakeWorkflowTileCacheGenerator{
		result: &mvt.GenerateResult{
			TotalTiles:         4,
			CachedTiles:        2,
			TilesTotalEstimate: 4,
			TilesProcessed:     4,
			GeneratedTiles:     2,
			EmptyTiles:         2,
			TotalSizeBytes:     128,
			MaxTileSizeBytes:   80,
			MinTileSizeBytes:   48,
			ActualMaxZoom:      1,
			StopReason:         "workflow_ogr2ogr_mvt",
			GenerationSec:      0.5,
			ExtentWGS84:        []float64{110, 20, 120, 30},
		},
		metadata: commonModels.JSONMap{
			"operator": "vector_to_mvt_tiles",
			"mode":     "direct",
		},
	}
	taskSvc.SetWorkflowTileGenerator(workflowGenerator)
	quickViewSvc := NewQuickViewService(db, nil)
	quickViewSvc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return nil, errors.New("file target must not load table spatial metadata")
	})
	taskSvc.SetQuickViewService(quickViewSvc)

	task := newTileCacheTaskDefinition()
	task.Config["target"] = commonModels.JSONMap{
		"source_engine_id": float64(26),
		"locator":          "addp://engine/26/path/shp/farmland.shp?type=file&item_id=100",
	}
	tile, _ := asJSONMap(task.Config["tile"])
	tile["source_srid"] = float64(4326)
	tile["extent"] = []interface{}{110.0, 20.0, 120.0, 30.0}
	tile["extent_srid"] = float64(4326)
	task.Config["tile"] = tile
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create file tile cache task: %v", err)
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute tile cache task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %s, want success; error=%#v", exec.Status, exec.ErrorDetails)
	}
	if workflowGenerator.lastReq.Identity.SourceKind != "file" || workflowGenerator.lastReq.Identity.FullName != "shp/farmland.shp" {
		t.Fatalf("workflow identity = %#v, want file farmland", workflowGenerator.lastReq.Identity)
	}
	if workflowGenerator.lastReq.ProgressSink == nil {
		t.Fatal("workflow progress sink is nil")
	}
	artifact, err := tileCacheRepo.GetTileCacheByFingerprintAndFormat(context.Background(), task.TenantID, commonModels.GenerateItemFingerprint(26, "shp/farmland.shp"), "mvt")
	if err != nil {
		t.Fatalf("load tile cache artifact: %v", err)
	}
	if artifact == nil || artifact.Status != models.TileCacheStatusReady {
		t.Fatalf("artifact = %#v, want ready", artifact)
	}
	if exec.Metadata["workflow_runtime"] == nil {
		t.Fatalf("execution metadata = %#v, want workflow_runtime", exec.Metadata)
	}
}

func TestTileCacheRecordProgressEventUpdatesExecution(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(repository.NewTileCacheRepository(db), taskExecRepo)
	startedAt := time.Now().Add(-30 * time.Second)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "tile-cache-progress-1",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeVectorTileCacheGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		Progress:    10,
		TriggerType: commonExecution.TriggerTypeManual,
		StartedAt:   &startedAt,
		Metadata: commonModels.JSONMap{
			"keep": "value",
		},
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	overallProgress := 36.7
	progressPercent := 36.7
	elapsedSeconds := 12.5
	remainingSeconds := 31.2
	if err := taskSvc.RecordProgressEvent(context.Background(), 7, "tile-cache-progress-1", TileCacheProgressEvent{
		Phase:              "generate",
		Event:              "progress",
		Message:            "生成矢量瓦片缓存",
		CurrentZoom:        10,
		MaxZoom:            18,
		TilesProcessed:     367,
		TilesTotalEstimate: 1000,
		OverallProgress:    &overallProgress,
		ProgressPercent:    &progressPercent,
		ElapsedSeconds:     &elapsedSeconds,
		RemainingSeconds:   &remainingSeconds,
		Metadata: commonModels.JSONMap{
			"worker": "python-workflow",
		},
	}); err != nil {
		t.Fatalf("RecordProgressEvent: %v", err)
	}

	got, err := taskExecRepo.GetByExecutionID(context.Background(), "tile-cache-progress-1", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Progress != 37 {
		t.Fatalf("progress = %d, want 37", got.Progress)
	}
	if got.CurrentStep == nil || *got.CurrentStep != "生成矢量瓦片缓存" {
		t.Fatalf("current_step = %#v, want vector tile message", got.CurrentStep)
	}
	if got.Metadata["keep"] != "value" {
		t.Fatalf("metadata.keep = %#v, want value", got.Metadata["keep"])
	}
	progressEvent, ok := asJSONMap(got.Metadata["progress_event"])
	if !ok {
		t.Fatalf("metadata.progress_event = %#v, want object", got.Metadata["progress_event"])
	}
	if progressEvent["phase"] != "generate" || progressEvent["event"] != "progress" {
		t.Fatalf("progress_event = %#v, want generate/progress", progressEvent)
	}
	if progressEvent["overall_progress"] != 36.7 || progressEvent["progress_percent"] != 36.7 {
		t.Fatalf("progress_event percentages = %#v", progressEvent)
	}
	if intFromTileCacheConfig(got.Metadata["tiles_processed"], 0) != 367 ||
		intFromTileCacheConfig(got.Metadata["tiles_total_estimate"], 0) != 1000 ||
		intFromTileCacheConfig(got.Metadata["current_zoom"], 0) != 10 {
		t.Fatalf("metadata = %#v, want tile progress facts", got.Metadata)
	}
}

func TestTileCacheRecordProgressEventDoesNotMoveProgressBackwards(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(repository.NewTileCacheRepository(db), taskExecRepo)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "tile-cache-progress-2",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeVectorTileCacheGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		Progress:    45,
		TriggerType: commonExecution.TriggerTypeManual,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}

	overallProgress := 12.2
	if err := taskSvc.RecordProgressEvent(context.Background(), 7, "tile-cache-progress-2", TileCacheProgressEvent{
		Phase:              "generate",
		Event:              "progress",
		TilesProcessed:     122,
		TilesTotalEstimate: 1000,
		OverallProgress:    &overallProgress,
	}); err != nil {
		t.Fatalf("RecordProgressEvent: %v", err)
	}
	got, err := taskExecRepo.GetByExecutionID(context.Background(), "tile-cache-progress-2", 7)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Progress != 45 {
		t.Fatalf("progress = %d, want 45", got.Progress)
	}
}

func TestTileCacheRecordProgressEventRejectsWrongExecution(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(repository.NewTileCacheRepository(db), taskExecRepo)
	if err := taskExecRepo.Create(context.Background(), &commonExecution.TaskExecution{
		TenantID:    7,
		ExecutionID: "tile-cache-progress-wrong",
		Module:      commonExecution.ModuleManager,
		TaskType:    commonExecution.TaskTypeRasterCOGGeneration,
		Source:      commonExecution.ModuleManager,
		Status:      commonExecution.ExecutionStatusRunning,
		TriggerType: commonExecution.TriggerTypeManual,
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	err := taskSvc.RecordProgressEvent(context.Background(), 7, "tile-cache-progress-wrong", TileCacheProgressEvent{
		Phase: "generate",
		Event: "progress",
	})
	if !errors.Is(err, ErrTileCacheProgressTargetMismatch) {
		t.Fatalf("RecordProgressEvent error = %v, want ErrTileCacheProgressTargetMismatch", err)
	}
}

func TestTileCacheGenerationSuccessMarksArtifactReadyAndQuickViewAvailable(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, taskExecRepo)
	quickViewSvc := NewQuickViewService(db, nil)
	quickViewSvc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:      "shape",
			GeometryColumns: []string{"shape"},
			SRID:            4326,
			ExtentSRID:      4326,
			Extent:          []float64{120, 30, 121, 31},
			PrimaryKey:      "id",
			RecordCount:     100,
		}, nil
	})
	taskSvc.SetQuickViewService(quickViewSvc)
	setVectorMaterializedViewTargetResolver(taskSvc, "public", "roads")
	cleaner := &fakeTileCacheCleaner{}
	invalidator := &fakeTileCacheRuntimeInvalidator{}
	taskSvc.SetTileCacheCleaner(cleaner)
	taskSvc.SetTileCacheRuntimeCacheInvalidator(invalidator)
	taskSvc.SetTileGenerator(&fakeTileCacheGenerator{
		result: &mvt.GenerateResult{
			ActualMaxZoom:      1,
			TotalTiles:         4,
			CachedTiles:        3,
			TilesTotalEstimate: 4,
			TilesProcessed:     4,
			GeneratedTiles:     3,
			EmptyTiles:         0,
			SkippedTiles:       1,
			OversizedTiles:     1,
			FailedTiles:        0,
			TotalSizeBytes:     4096,
			MaxTileSizeBytes:   2048,
			MinTileSizeBytes:   512,
			ZoomLevels: map[string]mvt.ZoomLevelStats{
				"1": {
					Zoom:           1,
					TotalTiles:     4,
					GeneratedTiles: 3,
					SkippedTiles:   1,
					OversizedTiles: 1,
					TotalSizeBytes: 4096,
					MaxSizeBytes:   2048,
					MinSizeBytes:   512,
				},
			},
			GenerationSec: 0.5,
			StopReason:    "test_complete",
			ExtentWGS84:   []float64{120, 30, 121, 31},
		},
	}, 1)

	task := newTileCacheTaskDefinition()
	task.Config["tile"] = commonModels.JSONMap{
		"format":      "mvt",
		"min_zoom":    float64(0),
		"max_zoom":    float64(1),
		"source_srid": float64(4326),
		"target_srid": float64(3857),
		"extent_srid": float64(4326),
		"extent":      []interface{}{float64(120), float64(30), float64(121), float64(31)},
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute tile cache task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %s, want success; error = %#v", exec.Status, exec.ErrorDetails)
	}
	if exec.Progress != 100 {
		t.Fatalf("execution progress = %d, want 100", exec.Progress)
	}
	if exec.Metadata["total_tiles"] != float64(4) {
		t.Fatalf("metadata total_tiles = %#v, want 4", exec.Metadata["total_tiles"])
	}
	if exec.Metadata["tiles_total_estimate"] != float64(4) {
		t.Fatalf("metadata tiles_total_estimate = %#v, want 4", exec.Metadata["tiles_total_estimate"])
	}
	if exec.Metadata["tiles_processed"] != float64(4) {
		t.Fatalf("metadata tiles_processed = %#v, want 4", exec.Metadata["tiles_processed"])
	}
	if exec.Metadata["generated_tiles"] != float64(3) {
		t.Fatalf("metadata generated_tiles = %#v, want 3", exec.Metadata["generated_tiles"])
	}
	if exec.Metadata["skipped_tiles"] != float64(1) {
		t.Fatalf("metadata skipped_tiles = %#v, want 1", exec.Metadata["skipped_tiles"])
	}
	if exec.Metadata["oversized_skipped_tiles"] != float64(1) {
		t.Fatalf("metadata oversized_skipped_tiles = %#v, want 1", exec.Metadata["oversized_skipped_tiles"])
	}
	if exec.Metadata["total_size_bytes"] != float64(4096) {
		t.Fatalf("metadata total_size_bytes = %#v, want 4096", exec.Metadata["total_size_bytes"])
	}
	if _, ok := exec.Metadata["zoom_levels"]; !ok {
		t.Fatalf("metadata = %#v, want zoom_levels", exec.Metadata)
	}
	if exec.Metadata["source_srid"] != float64(4326) {
		t.Fatalf("metadata source_srid = %#v, want 4326", exec.Metadata["source_srid"])
	}
	if exec.Metadata["target_srid"] != float64(3857) {
		t.Fatalf("metadata target_srid = %#v, want 3857", exec.Metadata["target_srid"])
	}
	if exec.Metadata["geometry_column"] != "geom_3857" {
		t.Fatalf("metadata geometry_column = %#v, want geom_3857", exec.Metadata["geometry_column"])
	}
	if _, ok := exec.Metadata["tile_range_extent_wgs84"]; !ok {
		t.Fatalf("metadata = %#v, want tile_range_extent_wgs84", exec.Metadata)
	}
	if _, ok := exec.Metadata["refresh_actions"]; !ok {
		t.Fatalf("metadata = %#v, want refresh_actions", exec.Metadata)
	}
	if _, ok := exec.Metadata["tile_generation_target"]; !ok {
		t.Fatalf("metadata = %#v, want tile_generation_target", exec.Metadata)
	}
	if _, ok := exec.Metadata["optimization"]; !ok {
		t.Fatalf("metadata = %#v, want optimization", exec.Metadata)
	}

	artifacts, _, err := tileCacheRepo.ListTileCache(context.Background(), repository.TileCacheFilter{
		TenantID: task.TenantID,
		TaskID:   task.ID,
	})
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifact count = %d, want 1", len(artifacts))
	}
	artifact := artifacts[0]
	if artifact.Status != models.TileCacheStatusReady {
		t.Fatalf("artifact status = %s, want ready", artifact.Status)
	}
	if artifact.StorageRef == "" {
		t.Fatal("artifact storage_ref is empty, want object prefix storage ref")
	}
	if artifact.ItemFingerprint == "" {
		t.Fatal("artifact item_fingerprint is empty, want standard item fingerprint")
	}
	var storageRef map[string]interface{}
	if err := json.Unmarshal([]byte(artifact.StorageRef), &storageRef); err != nil {
		t.Fatalf("artifact storage_ref = %s, want valid json: %v", artifact.StorageRef, err)
	}
	expectedObjectPrefix := fmt.Sprintf("tenant_%d/mvt-tiles/%s/", task.TenantID, artifact.ItemFingerprint)
	if storageRef["object_prefix"] != expectedObjectPrefix {
		t.Fatalf("artifact object_prefix = %v, want %s", storageRef["object_prefix"], expectedObjectPrefix)
	}
	expectedManifest := expectedObjectPrefix + "metadata.json"
	if storageRef["manifest"] != expectedManifest {
		t.Fatalf("artifact manifest = %v, want %s", storageRef["manifest"], expectedManifest)
	}
	if artifact.ExtentSRID == nil || *artifact.ExtentSRID != 4326 {
		t.Fatalf("artifact extent_srid = %#v, want 4326", artifact.ExtentSRID)
	}

	capability, err := quickViewSvc.BuildCapability(context.Background(), QuickViewIdentity{
		TenantID: task.TenantID,
		Locator:  "addp://engine/11/path/public/roads?type=table",
	}, 11, "public", "roads")
	if err != nil {
		t.Fatalf("build quick view capability: %v", err)
	}
	if !capability.CanUseQuickView || capability.Status != QuickViewStatusAvailable {
		t.Fatalf("quick view capability = can_use:%v status:%s, want available", capability.CanUseQuickView, capability.Status)
	}
	if capability.DefaultTileCacheID == nil || *capability.DefaultTileCacheID != artifact.ID {
		t.Fatalf("default_vector_tile_cache_id = %#v, want %d", capability.DefaultTileCacheID, artifact.ID)
	}

	target, _ := asJSONMap(task.Config["target"])
	target["item_id"] = float64(99)
	target["locator"] = "addp://engine/11/path/public/roads?type=table&item_id=99"
	task.Config["target"] = target
	if err := tileCacheRepo.UpdateTask(context.Background(), task); err != nil {
		t.Fatalf("update tile cache task target with ui-only identity fields: %v", err)
	}

	secondExecutionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, true)
	if err != nil {
		t.Fatalf("execute tile cache task second time: %v", err)
	}
	secondExec := waitForTileCacheTaskExecution(t, taskExecRepo, secondExecutionID, int(task.TenantID))
	if secondExec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("second execution status = %s, want success; error = %#v", secondExec.Status, secondExec.ErrorDetails)
	}
	artifacts, _, err = tileCacheRepo.ListTileCache(context.Background(), repository.TileCacheFilter{
		TenantID: task.TenantID,
		TaskID:   task.ID,
	})
	if err != nil {
		t.Fatalf("list artifacts after second execution: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifact count after second execution = %d, want 1 for idempotent result", len(artifacts))
	}
	if artifacts[0].ID != artifact.ID {
		t.Fatalf("artifact id after second execution = %d, want existing artifact %d", artifacts[0].ID, artifact.ID)
	}
	if len(cleaner.deletedRefs) != 1 || cleaner.deletedRefs[0] != artifact.StorageRef {
		t.Fatalf("deleted storage refs after second execution = %#v, want previous artifact storage ref", cleaner.deletedRefs)
	}
	if len(invalidator.calls) != 1 || invalidator.calls[0] != (tileCacheRuntimeInvalidationCall{tenantID: task.TenantID, tileCacheID: artifact.ID}) {
		t.Fatalf("runtime cache invalidation after second execution = %#v, want current artifact", invalidator.calls)
	}

	tileSettings, _ := asJSONMap(task.Config["tile"])
	tileSettings["max_zoom"] = float64(2)
	task.Config["tile"] = tileSettings
	if err := tileCacheRepo.UpdateTask(context.Background(), task); err != nil {
		t.Fatalf("update tile cache task tile config: %v", err)
	}
	thirdExecutionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, true)
	if err != nil {
		t.Fatalf("execute tile cache task third time: %v", err)
	}
	thirdExec := waitForTileCacheTaskExecution(t, taskExecRepo, thirdExecutionID, int(task.TenantID))
	if thirdExec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("third execution status = %s, want success; error = %#v", thirdExec.Status, thirdExec.ErrorDetails)
	}
	artifacts, _, err = tileCacheRepo.ListTileCache(context.Background(), repository.TileCacheFilter{
		TenantID: task.TenantID,
		TaskID:   task.ID,
	})
	if err != nil {
		t.Fatalf("list artifacts after tile config change: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifact count after tile config change = %d, want 1 current result", len(artifacts))
	}
	if artifacts[0].ID != artifact.ID {
		t.Fatalf("artifact id after tile config change = %d, want current result %d", artifacts[0].ID, artifact.ID)
	}
	if len(cleaner.deletedRefs) != 2 || cleaner.deletedRefs[1] != artifact.StorageRef {
		t.Fatalf("deleted storage refs after third execution = %#v, want previous artifact storage ref again", cleaner.deletedRefs)
	}
	if len(invalidator.calls) != 2 || invalidator.calls[1] != (tileCacheRuntimeInvalidationCall{tenantID: task.TenantID, tileCacheID: artifact.ID}) {
		t.Fatalf("runtime cache invalidation after third execution = %#v, want current artifact again", invalidator.calls)
	}
}

func TestTileCacheGenerationWithNoNonEmptyTilesMarksResultFailed(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, taskExecRepo)
	quickViewSvc := NewQuickViewService(db, nil)
	quickViewSvc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:      "shape",
			GeometryColumns: []string{"shape"},
			SRID:            4326,
			ExtentSRID:      4326,
			Extent:          []float64{120, 30, 121, 31},
			PrimaryKey:      "id",
			RecordCount:     100,
		}, nil
	})
	taskSvc.SetQuickViewService(quickViewSvc)
	setVectorMaterializedViewTargetResolver(taskSvc, "public", "roads")
	taskSvc.SetTileGenerator(&fakeTileCacheGenerator{
		result: &mvt.GenerateResult{
			ActualMaxZoom: 1,
			TotalTiles:    10,
			CachedTiles:   0,
			GenerationSec: 0.5,
			StopReason:    "all_empty",
		},
	}, 1)

	task := newTileCacheTaskDefinition()
	task.Config["tile"] = commonModels.JSONMap{
		"format":      "mvt",
		"min_zoom":    float64(0),
		"max_zoom":    float64(1),
		"source_srid": float64(4326),
		"target_srid": float64(3857),
		"extent_srid": float64(4326),
		"extent":      []interface{}{float64(120), float64(30), float64(121), float64(31)},
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute tile cache task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("execution status = %s, want failed", exec.Status)
	}
	if !strings.Contains(stringFromConfig(exec.ErrorDetails["message"]), "no non-empty tiles") {
		t.Fatalf("execution error_details = %#v, want no non-empty tiles", exec.ErrorDetails)
	}
	if exec.Metadata["cached_tiles"] != float64(0) {
		t.Fatalf("metadata cached_tiles = %#v, want 0", exec.Metadata["cached_tiles"])
	}

	results, _, err := tileCacheRepo.ListTileCache(context.Background(), repository.TileCacheFilter{
		TenantID: task.TenantID,
		TaskID:   task.ID,
	})
	if err != nil {
		t.Fatalf("list tile cache: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("tile cache result count = %d, want 1", len(results))
	}
	if results[0].Status != models.TileCacheStatusFailed {
		t.Fatalf("tile cache status = %s, want failed", results[0].Status)
	}
	if !strings.Contains(results[0].ErrorMessage, "no non-empty tiles") {
		t.Fatalf("tile cache error_message = %q, want no non-empty tiles", results[0].ErrorMessage)
	}
}

func TestTileCacheGenerationFailureKeepsLastTileProgress(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, taskExecRepo)
	quickViewSvc := NewQuickViewService(db, nil)
	quickViewSvc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:      "shape",
			GeometryColumns: []string{"shape"},
			SRID:            4326,
			ExtentSRID:      4326,
			Extent:          []float64{120, 30, 121, 31},
			PrimaryKey:      "id",
			RecordCount:     100,
		}, nil
	})
	taskSvc.SetQuickViewService(quickViewSvc)
	setVectorMaterializedViewTargetResolver(taskSvc, "public", "roads")
	taskSvc.SetTileGenerator(&fakeTileCacheGenerator{
		progress: &mvt.QuickViewProgress{
			Status:             "running",
			CurrentZoom:        1,
			MaxZoom:            2,
			TilesProcessed:     5,
			TilesTotalEstimate: 10,
			ProgressPercent:    50,
		},
		err: errors.New("generation interrupted"),
	}, 1)

	task := newTileCacheTaskDefinition()
	task.Config["tile"] = commonModels.JSONMap{
		"format":      "mvt",
		"min_zoom":    float64(0),
		"max_zoom":    float64(1),
		"source_srid": float64(4326),
		"target_srid": float64(3857),
		"extent_srid": float64(4326),
		"extent":      []interface{}{float64(120), float64(30), float64(121), float64(31)},
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute tile cache task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("execution status = %s, want failed", exec.Status)
	}
	if exec.Progress != 50 {
		t.Fatalf("execution progress = %d, want last tile progress 50", exec.Progress)
	}
	if exec.Metadata["tiles_processed"] != float64(5) {
		t.Fatalf("metadata tiles_processed = %#v, want 5", exec.Metadata["tiles_processed"])
	}
	if exec.Metadata["tiles_total_estimate"] != float64(10) {
		t.Fatalf("metadata tiles_total_estimate = %#v, want 10", exec.Metadata["tiles_total_estimate"])
	}
	if exec.CurrentStep == nil || !strings.Contains(*exec.CurrentStep, "5/10") {
		t.Fatalf("current_step = %#v, want tile progress detail", exec.CurrentStep)
	}
}

func TestTileCacheGenerationPersistsRenderableWGS84Extent(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, taskExecRepo)
	quickViewSvc := NewQuickViewService(db, nil)
	quickViewSvc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:      "shape",
			GeometryColumns: []string{"shape"},
			SRID:            4549,
			ExtentSRID:      4549,
			Extent:          []float64{570841.0277, 3404864.0397, 598936.5143, 3434951.8803},
			PrimaryKey:      "id",
			RecordCount:     73090,
		}, nil
	})
	taskSvc.SetQuickViewService(quickViewSvc)
	setVectorMaterializedViewTargetResolver(taskSvc, "public", "roads")
	taskSvc.SetTileGenerator(&fakeTileCacheGenerator{
		result: &mvt.GenerateResult{
			ActualMaxZoom: 12,
			TotalTiles:    38,
			CachedTiles:   33,
			GenerationSec: 2.8,
			StopReason:    "test_complete",
			ExtentWGS84:   []float64{120.73991920227512, 30.760374555538203, 121.03625518099717, 31.033743937252222},
		},
	}, 1)

	task := newTileCacheTaskDefinition()
	task.Config["tile"] = commonModels.JSONMap{
		"format":      "mvt",
		"min_zoom":    float64(3),
		"max_zoom":    float64(12),
		"source_srid": float64(4549),
		"target_srid": float64(3857),
		"extent_srid": float64(4549),
		"extent": []interface{}{
			float64(570841.0277),
			float64(3404864.0397),
			float64(598936.5143),
			float64(3434951.8803),
		},
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute tile cache task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %s, want success; error = %#v", exec.Status, exec.ErrorDetails)
	}

	results, _, err := tileCacheRepo.ListTileCache(context.Background(), repository.TileCacheFilter{
		TenantID: task.TenantID,
		TaskID:   task.ID,
	})
	if err != nil {
		t.Fatalf("list tile cache: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("tile cache result count = %d, want 1", len(results))
	}
	result := results[0]
	if result.ExtentSRID == nil || *result.ExtentSRID != 4326 {
		t.Fatalf("tile cache extent_srid = %#v, want 4326", result.ExtentSRID)
	}
	var extent []float64
	if err := json.Unmarshal(result.Extent, &extent); err != nil {
		t.Fatalf("tile cache extent = %s, want json array: %v", string(result.Extent), err)
	}
	wantExtent := []float64{120.73991920227512, 30.760374555538203, 121.03625518099717, 31.033743937252222}
	if fmt.Sprint(extent) != fmt.Sprint(wantExtent) {
		t.Fatalf("tile cache extent = %v, want WGS84 extent %v", extent, wantExtent)
	}
}

func TestTileCacheGenerationUsesIndexed3857Target(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, taskExecRepo)
	quickViewSvc := NewQuickViewService(db, nil)
	quickViewSvc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:      "SmGeometry",
			GeometryColumns: []string{"SmGeometry"},
			SRID:            2360,
			ExtentSRID:      4326,
			Extent:          []float64{104.4, 20.9, 112.1, 26.4},
			PrimaryKey:      "SmID",
			RecordCount:     10_597_882,
		}, nil
	})
	taskSvc.SetQuickViewService(quickViewSvc)
	taskSvc.SetRealtimeTileTargetResolver(fakeRealtimeTileTargetResolver{
		target: &RealtimeTileTarget{
			Schema:                       "public",
			Table:                        "dltb_mv3857",
			GeomColumn:                   "geom_3857",
			SRID:                         3857,
			VectorMaterializedViewTarget: true,
			TargetKind:                   VectorMaterializedViewTargetKindExternal3857MaterializedView,
		},
	})
	generator := &fakeTileCacheGenerator{
		result: &mvt.GenerateResult{
			ActualMaxZoom: 12,
			TotalTiles:    48,
			CachedTiles:   41,
			GenerationSec: 3.2,
			StopReason:    "test_complete",
			ExtentWGS84:   []float64{104.4, 20.9, 112.1, 26.4},
		},
	}
	taskSvc.SetTileGenerator(generator, 1)

	task := newTileCacheTaskDefinition()
	target, _ := asJSONMap(task.Config["target"])
	target["table"] = "dltb"
	target["locator"] = "addp://engine/11/path/public/dltb?type=table"
	task.Config["target"] = target
	task.Config["tile"] = commonModels.JSONMap{
		"format":      "mvt",
		"min_zoom":    float64(6),
		"max_zoom":    float64(12),
		"source_srid": float64(2360),
		"target_srid": float64(3857),
		"extent_srid": float64(4326),
		"extent":      []interface{}{float64(104.4), float64(20.9), float64(112.1), float64(26.4)},
	}
	task.Config["options"] = commonModels.JSONMap{
		"geometry_column": "SmGeometry",
		"primary_key":     "SmID",
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute tile cache task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %s, want success; error = %#v", exec.Status, exec.ErrorDetails)
	}
	if generator.lastConfig.Schema != "public" || generator.lastConfig.Table != "dltb_mv3857" || generator.lastConfig.GeomColumn != "geom_3857" || generator.lastConfig.SRID != 3857 {
		t.Fatalf("generator target = %s.%s.%s srid %d, want public.dltb_mv3857.geom_3857 srid 3857",
			generator.lastConfig.Schema, generator.lastConfig.Table, generator.lastConfig.GeomColumn, generator.lastConfig.SRID)
	}
	if generator.lastConfig.PrimaryKey != "" {
		t.Fatalf("generator primary_key = %q, want empty for vector materialized view target", generator.lastConfig.PrimaryKey)
	}
	if generator.lastConfig.OptimizationConfig == nil {
		t.Fatal("generator optimization config is nil, want default cache optimization config")
	}
	if generator.lastConfig.OptimizationConfig.TileSizeThresholds.MaxSizeMB != 4.0 {
		t.Fatalf("optimization max_size_mb = %v, want 4", generator.lastConfig.OptimizationConfig.TileSizeThresholds.MaxSizeMB)
	}
	if generator.lastConfig.OptimizationConfig.ExtentOptimization.BaseExtent != 1024 ||
		generator.lastConfig.OptimizationConfig.ExtentOptimization.MaxZoomExtent != 1024 ||
		generator.lastConfig.OptimizationConfig.ExtentOptimization.MinExtent != 256 {
		t.Fatalf("optimization extent config = %#v, want 1024/1024/256", generator.lastConfig.OptimizationConfig.ExtentOptimization)
	}
	targetMeta, ok := asJSONMap(exec.Metadata["tile_generation_target"])
	if !ok {
		t.Fatalf("execution metadata = %#v, want tile_generation_target", exec.Metadata)
	}
	if targetMeta["table"] != "dltb_mv3857" || targetMeta["geom_column"] != "geom_3857" {
		t.Fatalf("tile_generation_target = %#v, want dltb_mv3857 target", targetMeta)
	}
	if targetMeta["target_kind"] != VectorMaterializedViewTargetKindExternal3857MaterializedView {
		t.Fatalf("tile_generation_target = %#v, want external 3857 materialized view metadata", targetMeta)
	}
	optimizationMeta, ok := asJSONMap(exec.Metadata["optimization"])
	if !ok {
		t.Fatalf("execution metadata = %#v, want optimization config", exec.Metadata)
	}
	thresholds, ok := asJSONMap(optimizationMeta["tile_size_thresholds"])
	if !ok || thresholds["max_size_mb"] != float64(4) {
		t.Fatalf("optimization tile_size_thresholds = %#v, want max_size_mb 4", optimizationMeta["tile_size_thresholds"])
	}

	results, _, err := tileCacheRepo.ListTileCache(context.Background(), repository.TileCacheFilter{
		TenantID: task.TenantID,
		TaskID:   task.ID,
	})
	if err != nil {
		t.Fatalf("list tile cache: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("tile cache result count = %d, want 1", len(results))
	}
	expectedFingerprint := spatialItemFingerprint(11, "public", "dltb")
	if results[0].ItemFingerprint != expectedFingerprint {
		t.Fatalf("item_fingerprint = %s, want source item fingerprint %s", results[0].ItemFingerprint, expectedFingerprint)
	}
}

func TestTileCacheGenerationSkipsMetaWhenTaskHasSpatialFacts(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, taskExecRepo)
	metaCalls := 0
	quickViewSvc := NewQuickViewService(db, nil)
	quickViewSvc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		metaCalls++
		return nil, errors.New("meta backend unavailable")
	})
	taskSvc.SetQuickViewService(quickViewSvc)
	taskSvc.SetRealtimeTileTargetResolver(fakeRealtimeTileTargetResolver{})
	taskSvc.SetTileGenerator(&fakeTileCacheGenerator{
		result: &mvt.GenerateResult{
			ActualMaxZoom: 12,
			TotalTiles:    48,
			CachedTiles:   41,
			GenerationSec: 3.2,
		},
	}, 1)

	task := newTileCacheTaskDefinition()
	task.Config["tile"] = commonModels.JSONMap{
		"format":      "mvt",
		"min_zoom":    float64(6),
		"max_zoom":    float64(12),
		"source_srid": float64(2360),
		"target_srid": float64(3857),
		"extent_srid": float64(4326),
		"extent":      []interface{}{float64(104.4), float64(20.9), float64(112.1), float64(26.4)},
	}
	task.Config["options"] = commonModels.JSONMap{
		"geometry_column": "SmGeometry",
		"primary_key":     "SmID",
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute tile cache task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %s, want success; error = %#v", exec.Status, exec.ErrorDetails)
	}
	if metaCalls != 0 {
		t.Fatalf("meta loader calls = %d, want 0 when task config already has spatial facts", metaCalls)
	}
}

func TestTileCacheGenerationFallsBackToSourceWhenOptimizationTargetMissing(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, taskExecRepo)
	quickViewSvc := NewQuickViewService(db, nil)
	quickViewSvc.SetSpatialMetadataLoader(func(context.Context, uint, uint, string, string) (*SpatialMetadataResult, error) {
		return &SpatialMetadataResult{
			GeomColumn:      "SmGeometry",
			GeometryColumns: []string{"SmGeometry"},
			SRID:            2360,
			ExtentSRID:      4326,
			Extent:          []float64{104.4, 20.9, 112.1, 26.4},
			PrimaryKey:      "SmID",
			RecordCount:     10_597_882,
		}, nil
	})
	taskSvc.SetQuickViewService(quickViewSvc)
	taskSvc.SetRealtimeTileTargetResolver(fakeRealtimeTileTargetResolver{})
	taskSvc.SetTileGenerator(&fakeTileCacheGenerator{
		result: &mvt.GenerateResult{
			ActualMaxZoom: 12,
			TotalTiles:    48,
			CachedTiles:   41,
			GenerationSec: 3.2,
		},
	}, 1)

	task := newTileCacheTaskDefinition()
	task.Config["tile"] = commonModels.JSONMap{
		"format":      "mvt",
		"min_zoom":    float64(6),
		"max_zoom":    float64(12),
		"source_srid": float64(2360),
		"target_srid": float64(3857),
		"extent_srid": float64(4326),
		"extent":      []interface{}{float64(104.4), float64(20.9), float64(112.1), float64(26.4)},
	}
	task.Config["options"] = commonModels.JSONMap{
		"geometry_column": "SmGeometry",
		"primary_key":     "SmID",
	}
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, false)
	if err != nil {
		t.Fatalf("execute tile cache task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %s, want success", exec.Status)
	}
	if intFromTileCacheConfig(exec.Metadata["target_srid"], 0) != 3857 {
		t.Fatalf("metadata target_srid = %v, want tile target srid 3857", exec.Metadata["target_srid"])
	}
	targetMeta, ok := asJSONMap(exec.Metadata["tile_generation_target"])
	if !ok {
		t.Fatalf("tile_generation_target metadata = %#v", exec.Metadata["tile_generation_target"])
	}
	if targetMeta["schema"] != "public" || targetMeta["table"] != "roads" || targetMeta["geom_column"] != "SmGeometry" {
		t.Fatalf("tile_generation_target = %#v, want source table", targetMeta)
	}
	if intFromTileCacheConfig(targetMeta["srid"], 0) != 2360 {
		t.Fatalf("tile_generation_target.srid = %v, want 2360", targetMeta["srid"])
	}
	if targetMeta["target_kind"] != RealtimeTileTargetKindSourceTable {
		t.Fatalf("tile_generation_target = %#v, want source table metadata", targetMeta)
	}
	if targetMeta["optimization_recommended"] != true {
		t.Fatalf("optimization_recommended = %v, want true", targetMeta["optimization_recommended"])
	}
	if !strings.Contains(stringFromConfig(targetMeta["optimization_recommendation"]), "vector_materialized_view_generation") {
		t.Fatalf("optimization_recommendation = %#v, want vector_materialized_view_generation recommendation", targetMeta["optimization_recommendation"])
	}
}

type fakeTileCacheGenerator struct {
	result     *mvt.GenerateResult
	err        error
	progress   *mvt.QuickViewProgress
	sleepTime  time.Duration
	lastConfig mvt.QuickViewConfig
}

func (g *fakeTileCacheGenerator) GenerateMixed(ctx context.Context, cfg mvt.QuickViewConfig, progressTracker mvt.ProgressSink) (*mvt.GenerateResult, error) {
	g.lastConfig = cfg
	if g.progress != nil && progressTracker != nil {
		if err := progressTracker.UpdateProgress(ctx, g.progress); err != nil {
			return nil, err
		}
	}
	if g.sleepTime > 0 {
		time.Sleep(g.sleepTime)
	}
	if g.err != nil {
		return nil, g.err
	}
	if g.result != nil {
		return g.result, nil
	}
	return nil, errors.New("fake tile cache result is required")
}

type fakeWorkflowTileCacheGenerator struct {
	result   *mvt.GenerateResult
	metadata commonModels.JSONMap
	err      error
	lastReq  WorkflowTileCacheRequest
}

func (g *fakeWorkflowTileCacheGenerator) GenerateVectorTileCache(_ context.Context, req WorkflowTileCacheRequest) (*mvt.GenerateResult, commonModels.JSONMap, error) {
	g.lastReq = req
	if g.err != nil {
		return nil, nil, g.err
	}
	if g.result == nil {
		return nil, nil, errors.New("fake workflow tile cache result is required")
	}
	return g.result, g.metadata, nil
}

type fakeRealtimeTileTargetResolver struct {
	target *RealtimeTileTarget
	err    error
}

func (r fakeRealtimeTileTargetResolver) ResolveRealtimeTileTarget(context.Context, *uint, uint, string, string, string, int) (*RealtimeTileTarget, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.target, nil
}

type fakeTileCacheCleaner struct {
	deletedRefs []string
	err         error
}

func (c *fakeTileCacheCleaner) DeleteByStorageRef(_ context.Context, storageRef string) error {
	c.deletedRefs = append(c.deletedRefs, storageRef)
	return c.err
}

type tileCacheRuntimeInvalidationCall struct {
	tenantID    uint
	tileCacheID uint
}

type fakeTileCacheRuntimeInvalidator struct {
	calls []tileCacheRuntimeInvalidationCall
	err   error
}

func (i *fakeTileCacheRuntimeInvalidator) InvalidateTileCacheRuntimeCache(_ context.Context, tenantID uint, tileCacheID uint) error {
	i.calls = append(i.calls, tileCacheRuntimeInvalidationCall{tenantID: tenantID, tileCacheID: tileCacheID})
	return i.err
}

func setVectorMaterializedViewTargetResolver(taskSvc *TileCacheTaskService, schema, table string) {
	taskSvc.SetRealtimeTileTargetResolver(fakeRealtimeTileTargetResolver{
		target: &RealtimeTileTarget{
			Schema:                       schema,
			Table:                        table + "_mv3857",
			GeomColumn:                   "geom_3857",
			SRID:                         3857,
			VectorMaterializedViewTarget: true,
		},
	})
}

func newTileCacheTaskDefinition() *models.TileCacheTask {
	return &models.TileCacheTask{
		TenantID: 7,
		Name:     "瓦片缓存生成",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"source_engine_id": float64(11),
				"schema":           "public",
				"table":            "roads",
				"locator":          "addp://engine/11/path/public/roads?type=table",
			},
			"tile": commonModels.JSONMap{
				"format":      "mvt",
				"min_zoom":    float64(0),
				"max_zoom":    float64(1),
				"target_srid": float64(3857),
			},
			"options": commonModels.JSONMap{
				"geometry_column": "shape",
				"primary_key":     "id",
			},
		},
	}
}

func newTileCacheTaskServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE common.task_executions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		execution_id TEXT NOT NULL,
		module TEXT NOT NULL,
		task_type TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT '',
		source_task_id TEXT,
		source_task_name TEXT,
		parent_execution_id TEXT,
		status TEXT NOT NULL,
		progress INTEGER,
		current_step TEXT,
		trigger_type TEXT NOT NULL,
		triggered_by INTEGER,
		execution_config JSON,
		error_details JSON,
		metadata JSON,
		execution_time_ms INTEGER,
		rows_affected INTEGER,
		records_read INTEGER,
		records_written INTEGER,
		bytes_read INTEGER,
		bytes_written INTEGER,
		started_at DATETIME,
		completed_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create task_executions table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.vector_tile_cache_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		enabled BOOLEAN,
		last_execution_id TEXT,
		last_execution_status TEXT,
		last_run_at DATETIME,
		next_run_at DATETIME,
		schedule TEXT,
		created_by INTEGER,
		config JSON,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create vector_tile_cache_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.vector_tile_cache (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		item_id INTEGER,
		locator TEXT,
		task_id INTEGER,
		last_execution_id TEXT,
		tile_format TEXT NOT NULL,
		storage_ref TEXT,
		extent JSON,
		extent_srid INTEGER,
		min_zoom INTEGER,
		max_zoom INTEGER,
		status TEXT NOT NULL,
		error_message TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create vector_tile_cache table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.preview_state (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		locator TEXT,
		preferred_mode TEXT NOT NULL DEFAULT 'basic_preview',
		view_state JSON NOT NULL DEFAULT '{}',
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create preview_state table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.vector_materialized_view_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		enabled BOOLEAN,
		last_execution_id TEXT,
		last_execution_status TEXT,
		last_run_at DATETIME,
		next_run_at DATETIME,
		schedule TEXT,
		created_by INTEGER,
		config JSON,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create vector_materialized_view_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.vector_materialized_view (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
			item_fingerprint TEXT NOT NULL,
			item_id INTEGER,
			locator TEXT,
			task_id INTEGER,
		last_execution_id TEXT,
		source_engine_id INTEGER NOT NULL,
		source_schema TEXT NOT NULL,
		source_table TEXT NOT NULL,
		source_geometry_column TEXT NOT NULL,
		source_srid INTEGER NOT NULL,
		target_srid INTEGER NOT NULL,
		target_kind TEXT NOT NULL,
		target_schema TEXT NOT NULL,
		target_table TEXT NOT NULL,
		target_geometry_column TEXT NOT NULL,
		status TEXT NOT NULL,
		render_extent JSON,
		render_extent_srid INTEGER,
		row_count_estimate INTEGER,
		source_fingerprint_snapshot JSON,
		metadata JSON,
		error_message TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create vector_materialized_view table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.raster_cog (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		item_id INTEGER,
		locator TEXT,
			task_id INTEGER,
			last_execution_id TEXT,
			source_engine_id INTEGER NOT NULL,
			source_profile TEXT,
		source_size_bytes INTEGER,
		target_kind TEXT NOT NULL,
		storage_ref TEXT NOT NULL,
		file_name TEXT,
		size_bytes INTEGER,
		width INTEGER,
		height INTEGER,
		band_count INTEGER,
		source_srid INTEGER,
		source_crs TEXT,
		extent JSON,
		extent_srid INTEGER,
		status TEXT NOT NULL,
		metadata JSON,
		error_message TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create raster_cog table: %v", err)
	}
	return db
}

func waitForTileCacheTaskExecution(t *testing.T, repo *commonExecution.TaskExecutionRepository, executionID string, tenantID int) *commonExecution.TaskExecution {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		exec, err := repo.GetByExecutionID(context.Background(), executionID, tenantID)
		if err == nil && exec.IsCompleted() {
			return exec
		}
		time.Sleep(10 * time.Millisecond)
	}
	exec, err := repo.GetByExecutionID(context.Background(), executionID, tenantID)
	if err != nil {
		t.Fatalf("load execution after wait: %v", err)
	}
	t.Fatalf("execution status still %s after wait", exec.Status)
	return nil
}
