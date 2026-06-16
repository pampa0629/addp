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

func TestTileCacheTaskSchedulerClaimsDueTaskAndCreatesScheduledExecution(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, taskExecRepo)
	scheduler := NewTileCacheTaskScheduler(taskSvc)

	task := newTileCacheTaskDefinition()
	task.Schedule = "* * * * *"
	dueAt := time.Now().Add(-time.Minute)
	task.NextRunAt = &dueAt
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}
	task.NextRunAt = &dueAt
	if err := tileCacheRepo.UpdateTask(context.Background(), task); err != nil {
		t.Fatalf("mark tile cache task due: %v", err)
	}

	scheduler.runDueScheduledTasks(context.Background())

	var executions []*commonExecution.TaskExecution
	if err := db.Where("module = ? AND task_type = ?", commonExecution.ModuleManager, commonExecution.TaskTypeTileCacheGeneration).Find(&executions).Error; err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("execution count = %d, want 1", len(executions))
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executions[0].ExecutionID, int(task.TenantID))
	if exec.TriggerType != commonExecution.TriggerTypeScheduled {
		t.Fatalf("trigger_type = %s, want scheduled", exec.TriggerType)
	}
	if exec.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("status = %s, want failed because tile generator is unavailable", exec.Status)
	}

	refreshed, err := tileCacheRepo.GetTask(context.Background(), task.ID, task.TenantID)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if refreshed.NextRunAt == nil || !refreshed.NextRunAt.After(dueAt) {
		t.Fatalf("next_run_at = %#v, want after %s", refreshed.NextRunAt, dueAt)
	}
	if refreshed.LastExecutionID == nil || *refreshed.LastExecutionID != exec.ExecutionID {
		t.Fatalf("last_execution_id = %#v, want %s", refreshed.LastExecutionID, exec.ExecutionID)
	}
	if refreshed.LastExecutionStatus == nil || *refreshed.LastExecutionStatus != commonExecution.ExecutionStatusFailed {
		t.Fatalf("last_execution_status = %#v, want failed", refreshed.LastExecutionStatus)
	}
}

func TestTileCacheTaskCreateValidatesScheduleAndSetsNextRun(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, nil)

	task := newTileCacheTaskDefinition()
	task.Schedule = "* * * * *"
	if err := taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("create scheduled tile cache task: %v", err)
	}
	if task.NextRunAt == nil {
		t.Fatal("next_run_at is nil, want calculated next run")
	}

	invalid := newTileCacheTaskDefinition()
	invalid.Schedule = "not a cron"
	if err := taskSvc.Create(context.Background(), invalid); err == nil {
		t.Fatal("create invalid scheduled tile cache task succeeded, want error")
	}
}

func TestTileCacheTaskCreateNormalizesTargetIdentity(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	taskSvc := NewTileCacheTaskService(tileCacheRepo, nil)

	task := newTileCacheTaskDefinition()
	target, _ := asJSONMap(task.Config["target"])
	delete(target, "locator")
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
	if err == nil || !strings.Contains(err.Error(), "source_engine_id, schema and table") {
		t.Fatalf("create error = %v, want missing standard target fields", err)
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

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil)
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
	setPrepared3857Resolver(taskSvc, "public", "roads")
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

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil)
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
	if _, ok := exec.Metadata["preparation_actions"]; !ok {
		t.Fatalf("metadata = %#v, want preparation_actions", exec.Metadata)
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
		Locator:  "addp://engine/11/table/public.roads",
	}, 11, "public", "roads")
	if err != nil {
		t.Fatalf("build quick view capability: %v", err)
	}
	if !capability.CanUseQuickView || capability.Status != QuickViewStatusAvailable {
		t.Fatalf("quick view capability = can_use:%v status:%s, want available", capability.CanUseQuickView, capability.Status)
	}
	if capability.DefaultTileCacheID == nil || *capability.DefaultTileCacheID != artifact.ID {
		t.Fatalf("default_tile_cache_id = %#v, want %d", capability.DefaultTileCacheID, artifact.ID)
	}

	target, _ := asJSONMap(task.Config["target"])
	target["item_id"] = float64(99)
	target["locator"] = "addp://engine/11/path/public/roads?type=table&item_id=99"
	task.Config["target"] = target
	if err := tileCacheRepo.UpdateTask(context.Background(), task); err != nil {
		t.Fatalf("update tile cache task target with ui-only identity fields: %v", err)
	}

	secondExecutionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil)
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
	thirdExecutionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil)
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
	setPrepared3857Resolver(taskSvc, "public", "roads")
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

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil)
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
	setPrepared3857Resolver(taskSvc, "public", "roads")
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

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil)
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
	setPrepared3857Resolver(taskSvc, "public", "roads")
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

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil)
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
			Schema:       "public",
			Table:        "dltb_mv3857",
			GeomColumn:   "geom_3857",
			SRID:         3857,
			Prepared3857: true,
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
	target["locator"] = "addp://engine/11/table/public.dltb"
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

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil)
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
		t.Fatalf("generator primary_key = %q, want empty for prepared 3857 target", generator.lastConfig.PrimaryKey)
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
	if targetMeta["table"] != "dltb_mv3857" || targetMeta["geom_column"] != "geom_3857" || targetMeta["prepared_3857"] != true {
		t.Fatalf("tile_generation_target = %#v, want prepared dltb_mv3857 target", targetMeta)
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

	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil)
	if err != nil {
		t.Fatalf("execute tile cache task: %v", err)
	}
	exec := waitForTileCacheTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %s, want success", exec.Status)
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
	if targetMeta["prepared_3857"] == true {
		t.Fatalf("prepared_3857 = true, want false for source fallback")
	}
	if targetMeta["optimization_recommended"] != true {
		t.Fatalf("optimization_recommended = %v, want true", targetMeta["optimization_recommended"])
	}
	if !strings.Contains(stringFromConfig(targetMeta["optimization_recommendation"]), "quick_view_optimization") {
		t.Fatalf("optimization_recommendation = %#v, want quick_view_optimization recommendation", targetMeta["optimization_recommendation"])
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

func setPrepared3857Resolver(taskSvc *TileCacheTaskService, schema, table string) {
	taskSvc.SetRealtimeTileTargetResolver(fakeRealtimeTileTargetResolver{
		target: &RealtimeTileTarget{
			Schema:       schema,
			Table:        table + "_mv3857",
			GeomColumn:   "geom_3857",
			SRID:         3857,
			Prepared3857: true,
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
				"locator":          "addp://engine/11/table/public.roads",
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
	if err := db.Exec(`CREATE TABLE manager.tile_cache_tasks (
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
		t.Fatalf("create tile_cache_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.tile_cache (
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
		t.Fatalf("create tile_cache table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.quick_view (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		locator TEXT,
		preferred_mode TEXT NOT NULL DEFAULT 'table_geojson',
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create quick_view table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.quick_view_optimization_tasks (
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
		t.Fatalf("create quick_view_optimization_tasks table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.quick_view_optimization (
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
		t.Fatalf("create quick_view_optimization table: %v", err)
	}
	return db
}

func waitForTileCacheTaskExecution(t *testing.T, repo *commonExecution.TaskExecutionRepository, executionID string, tenantID int) *commonExecution.TaskExecution {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		exec, err := repo.GetByExecutionID(context.Background(), executionID, tenantID)
		if err == nil && exec.Status != commonExecution.ExecutionStatusRunning {
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
