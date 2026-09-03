package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTileCacheExecutionLifecycleIsAtomic(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewTileCacheRepository(db)
	task := createTileCacheExecutionRepositoryTestTask(t, db, 7)
	createdAt := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	exec := newTileCacheRepositoryTestExecution("manager-tile-atomic-1", 7, createdAt)

	claimed, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec, false)
	if err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if claimed.LastExecutionID == nil || *claimed.LastExecutionID != exec.ExecutionID ||
		claimed.LastExecutionStatus == nil || *claimed.LastExecutionStatus != commonExecution.ExecutionStatusPending {
		t.Fatalf("claimed task summary = %#v/%#v, want %s/pending", claimed.LastExecutionID, claimed.LastExecutionStatus, exec.ExecutionID)
	}
	var storedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("load pending execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusPending || storedExecution.StartedAt != nil {
		t.Fatalf("pending execution status=%s started_at=%v", storedExecution.Status, storedExecution.StartedAt)
	}
	if storedExecution.SourceTaskID == nil || *storedExecution.SourceTaskID != fmt.Sprint(task.ID) {
		t.Fatalf("source_task_id = %v, want %d", storedExecution.SourceTaskID, task.ID)
	}
	if storedExecution.SourceTaskName == nil || *storedExecution.SourceTaskName != task.Name {
		t.Fatalf("source_task_name = %v, want %q", storedExecution.SourceTaskName, task.Name)
	}
	if storedExecution.ExecutionConfig["version"] != float64(1) {
		t.Fatalf("execution_config = %#v, want locked task config snapshot", storedExecution.ExecutionConfig)
	}

	duplicate := newTileCacheRepositoryTestExecution("manager-tile-atomic-duplicate", 7, createdAt)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, duplicate, false); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("duplicate ClaimExecution error = %v, want conflict", err)
	}

	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	var runningTask models.TileCacheTask
	if err := db.First(&runningTask, task.ID).Error; err != nil {
		t.Fatalf("load running task: %v", err)
	}
	if runningTask.LastExecutionStatus == nil || *runningTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning ||
		runningTask.LastRunAt == nil || !runningTask.LastRunAt.Equal(startedAt) {
		t.Fatalf("running task summary status=%v last_run_at=%v", runningTask.LastExecutionStatus, runningTask.LastRunAt)
	}

	artifact := models.TileCache{
		TenantID: task.TenantID, ItemFingerprint: "fp-1", TileFormat: "mvt",
		Status: models.TileCacheStatusGenerating, TaskID: &task.ID, LastExecutionID: &exec.ExecutionID,
	}
	if err := db.Create(&artifact).Error; err != nil {
		t.Fatalf("create tile cache artifact: %v", err)
	}
	completedAt := startedAt.Add(2 * time.Minute)
	if err := repo.CompleteExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, artifact.ID,
		map[string]interface{}{
			"status": models.TileCacheStatusReady, "storage_ref": `{"bucket":"manager"}`, "error_message": "",
		},
		map[string]interface{}{
			"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt,
			"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "progress": 100,
		}, completedAt); err != nil {
		t.Fatalf("CompleteExecution: %v", err)
	}
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("reload completed execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusSuccess || storedExecution.CompletedAt == nil || storedExecution.ExecutionTimeMs == nil {
		t.Fatalf("completed execution = %#v", storedExecution)
	}
	if err := db.First(&runningTask, task.ID).Error; err != nil {
		t.Fatalf("reload completed task: %v", err)
	}
	if runningTask.LastExecutionStatus == nil || *runningTask.LastExecutionStatus != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("completed task status = %v, want success", runningTask.LastExecutionStatus)
	}
	var storedArtifact models.TileCache
	if err := db.First(&storedArtifact, artifact.ID).Error; err != nil {
		t.Fatalf("reload completed artifact: %v", err)
	}
	if storedArtifact.Status != models.TileCacheStatusReady || storedArtifact.StorageRef == "" {
		t.Fatalf("completed artifact = %#v", storedArtifact)
	}
}

func TestTileCacheExecutionRequiresConfirmationForCurrentResult(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewTileCacheRepository(db)
	task := createTileCacheExecutionRepositoryTestTask(t, db, 27)
	result := models.TileCache{
		TenantID: task.TenantID, ItemFingerprint: "tile-confirmation", TaskID: &task.ID,
		TileFormat: "mvt", Status: models.TileCacheStatusReady,
	}
	if err := db.Create(&result).Error; err != nil {
		t.Fatalf("create current result: %v", err)
	}

	unconfirmed := newTileCacheRepositoryTestExecution("manager-tile-confirmation-required", int(task.TenantID), time.Now())
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, unconfirmed, false); !errors.Is(err, ErrExistingResultActionRequired) {
		t.Fatalf("unconfirmed ClaimExecution error = %v", err)
	}
	var executionCount int64
	if err := db.Model(&commonExecution.TaskExecution{}).Count(&executionCount).Error; err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if executionCount != 0 {
		t.Fatalf("unconfirmed claim created %d executions", executionCount)
	}
	var storedTask models.TileCacheTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load task: %v", err)
	}
	if storedTask.LastExecutionID != nil || storedTask.LastExecutionStatus != nil {
		t.Fatalf("unconfirmed claim changed task summary: id=%v status=%v", storedTask.LastExecutionID, storedTask.LastExecutionStatus)
	}

	confirmed := newTileCacheRepositoryTestExecution("manager-tile-confirmed", int(task.TenantID), time.Now())
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, confirmed, true); err != nil {
		t.Fatalf("confirmed ClaimExecution: %v", err)
	}
	if err := db.Model(&commonExecution.TaskExecution{}).Count(&executionCount).Error; err != nil {
		t.Fatalf("count confirmed executions: %v", err)
	}
	if executionCount != 1 {
		t.Fatalf("confirmed claim created %d executions, want 1", executionCount)
	}
}

func TestTileCacheStartRollsBackWhenOwnerSummaryCannotAdvance(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewTileCacheRepository(db)
	task := createTileCacheExecutionRepositoryTestTask(t, db, 8)
	createdAt := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	exec := newTileCacheRepositoryTestExecution("manager-tile-start-rollback", 8, createdAt)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec, false); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if err := db.Unscoped().Delete(&models.TileCacheTask{}, task.ID).Error; err != nil {
		t.Fatalf("delete owner task: %v", err)
	}

	err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, createdAt.Add(time.Minute))
	if !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("StartExecution error = %v, want conflict", err)
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load execution after rollback: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusPending || stored.StartedAt != nil {
		t.Fatalf("execution changed despite owner rollback: status=%s started_at=%v", stored.Status, stored.StartedAt)
	}
}

func TestTileCacheCompleteRollsBackWhenArtifactCannotAdvance(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewTileCacheRepository(db)
	task := createTileCacheExecutionRepositoryTestTask(t, db, 9)
	createdAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	exec := newTileCacheRepositoryTestExecution("manager-tile-complete-rollback", 9, createdAt)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec, false); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	completedAt := startedAt.Add(time.Minute)
	err := repo.CompleteExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, 999999,
		map[string]interface{}{"status": models.TileCacheStatusReady},
		map[string]interface{}{"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt}, completedAt)
	if !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("CompleteExecution error = %v, want conflict", err)
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load execution after rollback: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusRunning || stored.CompletedAt != nil {
		t.Fatalf("execution changed despite artifact rollback: status=%s completed_at=%v", stored.Status, stored.CompletedAt)
	}
}

func TestVectorMaterializedViewExecutionLifecycleIsAtomic(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewVectorMaterializedViewRepository(db)
	task := createVectorMaterializedViewExecutionRepositoryTestTask(t, db, 10)
	createdAt := time.Date(2026, 7, 17, 1, 0, 0, 0, time.UTC)
	exec := newManagerRepositoryTestExecution(
		"manager-vmv-atomic-1", 10, commonExecution.TaskTypeVectorMaterializedViewGeneration, createdAt,
	)

	claimed, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec, false)
	if err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if claimed.LastExecutionID == nil || *claimed.LastExecutionID != exec.ExecutionID ||
		claimed.LastExecutionStatus == nil || *claimed.LastExecutionStatus != commonExecution.ExecutionStatusPending {
		t.Fatalf("claimed task summary = %#v/%#v", claimed.LastExecutionID, claimed.LastExecutionStatus)
	}
	var storedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("load pending execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusPending || storedExecution.StartedAt != nil {
		t.Fatalf("pending execution status=%s started_at=%v", storedExecution.Status, storedExecution.StartedAt)
	}

	duplicate := newManagerRepositoryTestExecution(
		"manager-vmv-atomic-duplicate", 10, commonExecution.TaskTypeVectorMaterializedViewGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, duplicate, false); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("duplicate ClaimExecution error = %v, want conflict", err)
	}

	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	result := createVectorMaterializedViewExecutionRepositoryTestResult(t, db, task, exec.ExecutionID)
	completedAt := startedAt.Add(2 * time.Minute)
	if err := repo.CompleteExecution(
		context.Background(), task.ID, task.TenantID, exec.ExecutionID, result.ID,
		map[string]interface{}{"status": models.VectorMaterializedViewStatusReady, "error_message": ""},
		map[string]interface{}{
			"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt,
			"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "progress": 100,
		}, completedAt,
	); err != nil {
		t.Fatalf("CompleteExecution: %v", err)
	}
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("reload completed execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusSuccess || storedExecution.CompletedAt == nil || storedExecution.ExecutionTimeMs == nil {
		t.Fatalf("completed execution = %#v", storedExecution)
	}
	var storedTask models.VectorMaterializedViewTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("reload completed task: %v", err)
	}
	if storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("completed task status = %v", storedTask.LastExecutionStatus)
	}
	var storedResult models.VectorMaterializedView
	if err := db.First(&storedResult, result.ID).Error; err != nil {
		t.Fatalf("reload completed result: %v", err)
	}
	if storedResult.Status != models.VectorMaterializedViewStatusReady {
		t.Fatalf("completed result status = %s", storedResult.Status)
	}
}

func TestVectorMaterializedViewCompleteRollsBackWhenResultCannotAdvance(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewVectorMaterializedViewRepository(db)
	task := createVectorMaterializedViewExecutionRepositoryTestTask(t, db, 11)
	createdAt := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	exec := newManagerRepositoryTestExecution(
		"manager-vmv-complete-rollback", 11, commonExecution.TaskTypeVectorMaterializedViewGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec, false); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	completedAt := startedAt.Add(time.Minute)
	err := repo.CompleteExecution(
		context.Background(), task.ID, task.TenantID, exec.ExecutionID, 999999,
		map[string]interface{}{"status": models.VectorMaterializedViewStatusReady},
		map[string]interface{}{"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt}, completedAt,
	)
	if !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("CompleteExecution error = %v, want conflict", err)
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load execution after rollback: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusRunning || stored.CompletedAt != nil {
		t.Fatalf("execution changed despite result rollback: status=%s completed_at=%v", stored.Status, stored.CompletedAt)
	}
}

func TestRasterCOGExecutionLifecycleIsAtomic(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewRasterCOGRepository(db)
	task := createRasterCOGExecutionRepositoryTestTask(t, db, 12)
	createdAt := time.Date(2026, 7, 17, 3, 0, 0, 0, time.UTC)
	exec := newManagerRepositoryTestExecution(
		"manager-raster-cog-atomic-1", 12, commonExecution.TaskTypeRasterCOGGeneration, createdAt,
	)

	claimed, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec, false)
	if err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if claimed.LastExecutionID == nil || *claimed.LastExecutionID != exec.ExecutionID ||
		claimed.LastExecutionStatus == nil || *claimed.LastExecutionStatus != commonExecution.ExecutionStatusPending {
		t.Fatalf("claimed task summary = %#v/%#v", claimed.LastExecutionID, claimed.LastExecutionStatus)
	}
	var storedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("load pending execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusPending || storedExecution.StartedAt != nil {
		t.Fatalf("pending execution status=%s started_at=%v", storedExecution.Status, storedExecution.StartedAt)
	}

	duplicate := newManagerRepositoryTestExecution(
		"manager-raster-cog-atomic-duplicate", 12, commonExecution.TaskTypeRasterCOGGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, duplicate, false); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("duplicate ClaimExecution error = %v, want conflict", err)
	}

	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	result := createRasterCOGExecutionRepositoryTestResult(t, db, task, exec.ExecutionID)
	completedAt := startedAt.Add(2 * time.Minute)
	if err := repo.CompleteExecution(
		context.Background(), task.ID, task.TenantID, exec.ExecutionID, result.ID,
		map[string]interface{}{"status": models.RasterCOGStatusReady, "error_message": ""},
		map[string]interface{}{
			"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt,
			"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "progress": 100,
		}, completedAt,
	); err != nil {
		t.Fatalf("CompleteExecution: %v", err)
	}
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("reload completed execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusSuccess || storedExecution.CompletedAt == nil || storedExecution.ExecutionTimeMs == nil {
		t.Fatalf("completed execution = %#v", storedExecution)
	}
	var storedTask models.RasterCOGTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("reload completed task: %v", err)
	}
	if storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("completed task status = %v", storedTask.LastExecutionStatus)
	}
	var storedResult models.RasterCOG
	if err := db.First(&storedResult, result.ID).Error; err != nil {
		t.Fatalf("reload completed result: %v", err)
	}
	if storedResult.Status != models.RasterCOGStatusReady {
		t.Fatalf("completed result status = %s", storedResult.Status)
	}
}

func TestRasterMosaicExecutionLifecycleIsAtomic(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewRasterMosaicRepository(db)
	task := createRasterMosaicExecutionRepositoryTestTask(t, db, 13)
	createdAt := time.Date(2026, 7, 17, 4, 0, 0, 0, time.UTC)
	exec := newManagerRepositoryTestExecution(
		"manager-raster-mosaic-atomic-1", 13, commonExecution.TaskTypeRasterMosaicGeneration, createdAt,
	)

	claimed, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec)
	if err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if claimed.LastExecutionID == nil || *claimed.LastExecutionID != exec.ExecutionID ||
		claimed.LastExecutionStatus == nil || *claimed.LastExecutionStatus != commonExecution.ExecutionStatusPending {
		t.Fatalf("claimed task summary = %#v/%#v", claimed.LastExecutionID, claimed.LastExecutionStatus)
	}
	var storedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("load pending execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusPending || storedExecution.StartedAt != nil {
		t.Fatalf("pending execution status=%s started_at=%v", storedExecution.Status, storedExecution.StartedAt)
	}

	duplicate := newManagerRepositoryTestExecution(
		"manager-raster-mosaic-atomic-duplicate", 13, commonExecution.TaskTypeRasterMosaicGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, duplicate); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("duplicate ClaimExecution error = %v, want conflict", err)
	}

	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	completedAt := startedAt.Add(2 * time.Minute)
	if err := repo.CompleteExecution(
		context.Background(), task.ID, task.TenantID, exec.ExecutionID,
		map[string]interface{}{
			"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt,
			"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "progress": 100,
		}, completedAt,
	); err != nil {
		t.Fatalf("CompleteExecution: %v", err)
	}
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("reload completed execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusSuccess || storedExecution.CompletedAt == nil || storedExecution.ExecutionTimeMs == nil {
		t.Fatalf("completed execution = %#v", storedExecution)
	}
	var storedTask models.RasterMosaicTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("reload completed task: %v", err)
	}
	if storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("completed task status = %v", storedTask.LastExecutionStatus)
	}
}

func TestModel3DGLBExecutionLifecycleIsAtomic(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewModel3DGLBRepository(db)
	task := createModel3DGLBExecutionRepositoryTestTask(t, db, 14)
	createdAt := time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC)
	exec := newManagerRepositoryTestExecution(
		"manager-model-3d-glb-atomic-1", 14, commonExecution.TaskTypeModel3DGLBGeneration, createdAt,
	)

	claimed, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec, false)
	if err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if claimed.LastExecutionID == nil || *claimed.LastExecutionID != exec.ExecutionID ||
		claimed.LastExecutionStatus == nil || *claimed.LastExecutionStatus != commonExecution.ExecutionStatusPending {
		t.Fatalf("claimed task summary = %#v/%#v", claimed.LastExecutionID, claimed.LastExecutionStatus)
	}
	var storedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("load pending execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusPending || storedExecution.StartedAt != nil {
		t.Fatalf("pending execution status=%s started_at=%v", storedExecution.Status, storedExecution.StartedAt)
	}

	duplicate := newManagerRepositoryTestExecution(
		"manager-model-3d-glb-atomic-duplicate", 14, commonExecution.TaskTypeModel3DGLBGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, duplicate, false); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("duplicate ClaimExecution error = %v, want conflict", err)
	}

	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	result := createModel3DGLBExecutionRepositoryTestResult(t, db, task, exec.ExecutionID)
	completedAt := startedAt.Add(2 * time.Minute)
	if err := repo.CompleteExecution(
		context.Background(), task.ID, task.TenantID, exec.ExecutionID, result.ID,
		map[string]interface{}{"status": models.Model3DGLBStatusReady, "error_message": ""},
		map[string]interface{}{
			"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt,
			"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "progress": 100,
		}, completedAt,
	); err != nil {
		t.Fatalf("CompleteExecution: %v", err)
	}
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("reload completed execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusSuccess || storedExecution.CompletedAt == nil || storedExecution.ExecutionTimeMs == nil {
		t.Fatalf("completed execution = %#v", storedExecution)
	}
	var storedTask models.Model3DGLBTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("reload completed task: %v", err)
	}
	if storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("completed task status = %v", storedTask.LastExecutionStatus)
	}
	var storedResult models.Model3DGLB
	if err := db.First(&storedResult, result.ID).Error; err != nil {
		t.Fatalf("reload completed result: %v", err)
	}
	if storedResult.Status != models.Model3DGLBStatusReady {
		t.Fatalf("completed result status = %s", storedResult.Status)
	}
}

func TestModel3DGLBCompleteRollsBackWhenResultLosesOwnership(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewModel3DGLBRepository(db)
	task := createModel3DGLBExecutionRepositoryTestTask(t, db, 15)
	createdAt := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	exec := newManagerRepositoryTestExecution(
		"manager-model-3d-glb-complete-rollback", 15, commonExecution.TaskTypeModel3DGLBGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec, false); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	result := createModel3DGLBExecutionRepositoryTestResult(t, db, task, "newer-execution")
	completedAt := startedAt.Add(time.Minute)
	err := repo.CompleteExecution(
		context.Background(), task.ID, task.TenantID, exec.ExecutionID, result.ID,
		map[string]interface{}{"status": models.Model3DGLBStatusReady},
		map[string]interface{}{"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt}, completedAt,
	)
	if !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("CompleteExecution error = %v, want conflict", err)
	}
	var storedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("load execution after rollback: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusRunning || storedExecution.CompletedAt != nil {
		t.Fatalf("execution changed despite result fencing rollback: status=%s completed_at=%v", storedExecution.Status, storedExecution.CompletedAt)
	}
	var storedResult models.Model3DGLB
	if err := db.First(&storedResult, result.ID).Error; err != nil {
		t.Fatalf("load result after rollback: %v", err)
	}
	if storedResult.Status != models.Model3DGLBStatusBuilding || storedResult.LastExecutionID == nil || *storedResult.LastExecutionID != "newer-execution" {
		t.Fatalf("result changed despite fencing rollback: %#v", storedResult)
	}
}

func TestGaussianSplatKSplatExecutionLifecycleIsAtomic(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewGaussianSplatKSplatRepository(db)
	task := createGaussianSplatKSplatExecutionRepositoryTestTask(t, db, 16)
	createdAt := time.Date(2026, 7, 17, 7, 0, 0, 0, time.UTC)
	exec := newManagerRepositoryTestExecution(
		"manager-gaussian-splat-ksplat-atomic-1", 16, commonExecution.TaskTypeGaussianSplatKSplatGeneration, createdAt,
	)

	claimed, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec, false)
	if err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if claimed.LastExecutionID == nil || *claimed.LastExecutionID != exec.ExecutionID ||
		claimed.LastExecutionStatus == nil || *claimed.LastExecutionStatus != commonExecution.ExecutionStatusPending {
		t.Fatalf("claimed task summary = %#v/%#v", claimed.LastExecutionID, claimed.LastExecutionStatus)
	}
	var storedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("load pending execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusPending || storedExecution.StartedAt != nil {
		t.Fatalf("pending execution status=%s started_at=%v", storedExecution.Status, storedExecution.StartedAt)
	}
	if storedExecution.ExecutionConfig["version"] != float64(1) {
		t.Fatalf("execution_config = %#v, want locked task config snapshot", storedExecution.ExecutionConfig)
	}

	duplicate := newManagerRepositoryTestExecution(
		"manager-gaussian-splat-ksplat-atomic-duplicate", 16, commonExecution.TaskTypeGaussianSplatKSplatGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, duplicate, false); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("duplicate ClaimExecution error = %v, want conflict", err)
	}

	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	result := createGaussianSplatKSplatExecutionRepositoryTestResult(t, db, task, exec.ExecutionID)
	completedAt := startedAt.Add(2 * time.Minute)
	if err := repo.CompleteExecution(
		context.Background(), task.ID, task.TenantID, exec.ExecutionID, result.ID,
		map[string]interface{}{"status": models.GaussianSplatKSplatStatusReady, "error_message": ""},
		map[string]interface{}{
			"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt,
			"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "progress": 100,
		}, completedAt,
	); err != nil {
		t.Fatalf("CompleteExecution: %v", err)
	}
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("reload completed execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusSuccess || storedExecution.CompletedAt == nil || storedExecution.ExecutionTimeMs == nil {
		t.Fatalf("completed execution = %#v", storedExecution)
	}
	var storedTask models.GaussianSplatKSplatTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("reload completed task: %v", err)
	}
	if storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("completed task status = %v", storedTask.LastExecutionStatus)
	}
	var storedResult models.GaussianSplatKSplat
	if err := db.First(&storedResult, result.ID).Error; err != nil {
		t.Fatalf("reload completed result: %v", err)
	}
	if storedResult.Status != models.GaussianSplatKSplatStatusReady {
		t.Fatalf("completed result status = %s", storedResult.Status)
	}
}

func TestGaussianSplatKSplatCompleteRollsBackWhenResultLosesOwnership(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewGaussianSplatKSplatRepository(db)
	task := createGaussianSplatKSplatExecutionRepositoryTestTask(t, db, 17)
	createdAt := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	exec := newManagerRepositoryTestExecution(
		"manager-gaussian-splat-ksplat-complete-rollback", 17, commonExecution.TaskTypeGaussianSplatKSplatGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec, false); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	result := createGaussianSplatKSplatExecutionRepositoryTestResult(t, db, task, "newer-execution")
	completedAt := startedAt.Add(time.Minute)
	err := repo.CompleteExecution(
		context.Background(), task.ID, task.TenantID, exec.ExecutionID, result.ID,
		map[string]interface{}{"status": models.GaussianSplatKSplatStatusReady},
		map[string]interface{}{"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt}, completedAt,
	)
	if !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("CompleteExecution error = %v, want conflict", err)
	}
	var storedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("load execution after rollback: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusRunning || storedExecution.CompletedAt != nil {
		t.Fatalf("execution changed despite result fencing rollback: status=%s completed_at=%v", storedExecution.Status, storedExecution.CompletedAt)
	}
}

func TestPointCloudCOPCExecutionLifecycleIsAtomic(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewPointCloudCOPCRepository(db)
	task := createPointCloudCOPCExecutionRepositoryTestTask(t, db, 18)
	createdAt := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	exec := newManagerRepositoryTestExecution(
		"manager-point-cloud-copc-atomic-1", 18, commonExecution.TaskTypePointCloudCOPCGeneration, createdAt,
	)
	claimed, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec, false)
	if err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if claimed.LastExecutionStatus == nil || *claimed.LastExecutionStatus != commonExecution.ExecutionStatusPending {
		t.Fatalf("claimed task status = %#v", claimed.LastExecutionStatus)
	}
	duplicate := newManagerRepositoryTestExecution(
		"manager-point-cloud-copc-atomic-duplicate", 18, commonExecution.TaskTypePointCloudCOPCGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, duplicate, false); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("duplicate ClaimExecution error = %v, want conflict", err)
	}
	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	if err := repo.UpdateRunningExecutionProgress(context.Background(), task.TenantID, exec.ExecutionID, map[string]interface{}{
		"progress": 42, "updated_at": startedAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("UpdateRunningExecutionProgress: %v", err)
	}
	result := createPointCloudCOPCExecutionRepositoryTestResult(t, db, task, exec.ExecutionID)
	completedAt := startedAt.Add(2 * time.Minute)
	if err := repo.CompleteExecution(
		context.Background(), task.ID, task.TenantID, exec.ExecutionID, result.ID,
		map[string]interface{}{"status": models.PointCloudCOPCStatusReady, "error_message": ""},
		map[string]interface{}{
			"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt,
			"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "progress": 100,
		}, completedAt,
	); err != nil {
		t.Fatalf("CompleteExecution: %v", err)
	}
	if err := repo.UpdateRunningExecutionProgress(context.Background(), task.TenantID, exec.ExecutionID, map[string]interface{}{"progress": 90}); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("late progress error = %v, want conflict", err)
	}
	storedExecution, err := repo.GetExecution(context.Background(), task.TenantID, exec.ExecutionID)
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusSuccess || storedExecution.Progress != 100 {
		t.Fatalf("completed execution = %#v", storedExecution)
	}
	var storedResult models.PointCloudCOPC
	if err := db.First(&storedResult, result.ID).Error; err != nil {
		t.Fatalf("reload completed result: %v", err)
	}
	if storedResult.Status != models.PointCloudCOPCStatusReady {
		t.Fatalf("completed result status = %s", storedResult.Status)
	}
}

func TestModel3DTilesExecutionRequiresConfirmationAndFencesResult(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewModel3DTilesRepository(db)
	task := createModel3DTilesExecutionRepositoryTestTask(t, db, 20, "tiles-fp", models.Model3DTilesTargetFormatS3M)
	createdAt := time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC)
	first := newManagerRepositoryTestExecution("manager-model3d-tiles-first", 20, commonExecution.TaskTypeModel3DTilesGeneration, createdAt)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, first, false); err != nil {
		t.Fatalf("first ClaimExecution: %v", err)
	}
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, first.ExecutionID, createdAt.Add(time.Minute)); err != nil {
		t.Fatalf("first StartExecution: %v", err)
	}
	result := createModel3DTilesExecutionRepositoryTestResult(t, db, task, first.ExecutionID)
	completedAt := createdAt.Add(2 * time.Minute)
	if err := repo.CompleteExecution(context.Background(), task.ID, task.TenantID, first.ExecutionID, result.ID,
		map[string]interface{}{"status": models.Model3DTilesStatusReady},
		map[string]interface{}{"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt, "progress": 100}, completedAt); err != nil {
		t.Fatalf("first CompleteExecution: %v", err)
	}

	var countBefore int64
	if err := db.Model(&commonExecution.TaskExecution{}).Count(&countBefore).Error; err != nil {
		t.Fatalf("count executions before rejected refresh: %v", err)
	}
	unconfirmed := newManagerRepositoryTestExecution("manager-model3d-tiles-unconfirmed", 20, commonExecution.TaskTypeModel3DTilesGeneration, completedAt)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, unconfirmed, false); !errors.Is(err, ErrExistingResultActionRequired) {
		t.Fatalf("unconfirmed ClaimExecution error = %v", err)
	}
	var countAfter int64
	if err := db.Model(&commonExecution.TaskExecution{}).Count(&countAfter).Error; err != nil {
		t.Fatalf("count executions after rejected refresh: %v", err)
	}
	if countAfter != countBefore {
		t.Fatalf("rejected refresh created execution: before=%d after=%d", countBefore, countAfter)
	}

	confirmed := newManagerRepositoryTestExecution("manager-model3d-tiles-confirmed", 20, commonExecution.TaskTypeModel3DTilesGeneration, completedAt)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, confirmed, true); err != nil {
		t.Fatalf("confirmed ClaimExecution: %v", err)
	}
	activeUnconfirmed := newManagerRepositoryTestExecution("manager-model3d-tiles-active-unconfirmed", 20, commonExecution.TaskTypeModel3DTilesGeneration, completedAt)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, activeUnconfirmed, false); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("active unconfirmed ClaimExecution error = %v, want active conflict", err)
	}
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, confirmed.ExecutionID, completedAt.Add(time.Minute)); err != nil {
		t.Fatalf("confirmed StartExecution: %v", err)
	}
	if err := db.Model(&models.Model3DTiles{}).Where("id = ?", result.ID).Update("last_execution_id", confirmed.ExecutionID).Error; err != nil {
		t.Fatalf("assign result to confirmed execution: %v", err)
	}
	if err := db.Model(&models.Model3DTiles{}).Where("id = ?", result.ID).Update("last_execution_id", "newer-execution").Error; err != nil {
		t.Fatalf("move result fence: %v", err)
	}
	if err := repo.CompleteExecution(context.Background(), task.ID, task.TenantID, confirmed.ExecutionID, result.ID,
		map[string]interface{}{"status": models.Model3DTilesStatusReady},
		map[string]interface{}{"status": commonExecution.ExecutionStatusSuccess, "completed_at": completedAt.Add(2 * time.Minute)}, completedAt.Add(2*time.Minute)); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("fenced CompleteExecution error = %v, want conflict", err)
	}
	stored, err := repo.GetExecution(context.Background(), confirmed.ExecutionID, task.TenantID)
	if err != nil {
		t.Fatalf("load confirmed execution after rollback: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusRunning || stored.CompletedAt != nil {
		t.Fatalf("confirmed execution changed despite result fence: status=%s completed_at=%v", stored.Status, stored.CompletedAt)
	}

	threeDTilesTask := createModel3DTilesExecutionRepositoryTestTask(t, db, 20, "tiles-fp", models.Model3DTilesTargetFormat3DTiles)
	otherFormat := newManagerRepositoryTestExecution("manager-model3d-tiles-other-format", 20, commonExecution.TaskTypeModel3DTilesGeneration, completedAt)
	if _, err := repo.ClaimExecution(context.Background(), threeDTilesTask.ID, threeDTilesTask.TenantID, otherFormat, false); err != nil {
		t.Fatalf("other target format should not require S3M confirmation: %v", err)
	}
}

func newTileCacheExecutionRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.vector_tile_cache_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
		description TEXT, enabled BOOLEAN, schedule TEXT, next_run_at DATETIME, last_run_at DATETIME,
		last_execution_id TEXT, last_execution_status TEXT, config JSON, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create tile cache task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.vector_tile_cache (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
		item_id INTEGER, locator TEXT, task_id INTEGER, last_execution_id TEXT, tile_format TEXT NOT NULL,
		storage_ref TEXT, source_version TEXT NOT NULL, profile_hash TEXT NOT NULL, extent JSON, extent_srid INTEGER, min_zoom INTEGER, max_zoom INTEGER,
		status TEXT NOT NULL, error_message TEXT, created_by INTEGER, created_at DATETIME,
		updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create tile cache artifact table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.vector_materialized_view_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
		description TEXT, enabled BOOLEAN, schedule TEXT, next_run_at DATETIME, last_run_at DATETIME,
		last_execution_id TEXT, last_execution_status TEXT, config JSON, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create vector materialized view task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.vector_materialized_view (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
		item_id INTEGER, locator TEXT, task_id INTEGER, last_execution_id TEXT, source_engine_id INTEGER,
		source_schema TEXT, source_table TEXT, source_geometry_column TEXT, source_srid INTEGER,
		target_srid INTEGER, target_kind TEXT, target_schema TEXT, target_table TEXT,
		target_geometry_column TEXT, status TEXT, render_extent JSON, render_extent_srid INTEGER,
		row_count_estimate INTEGER, source_fingerprint_snapshot JSON, metadata JSON, error_message TEXT,
		created_by INTEGER, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create vector materialized view result table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.raster_cog_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
		description TEXT, enabled BOOLEAN, schedule TEXT, next_run_at DATETIME, last_run_at DATETIME,
		last_execution_id TEXT, last_execution_status TEXT, config JSON, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create raster COG task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.raster_cog (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
		item_id INTEGER, locator TEXT, task_id INTEGER, last_execution_id TEXT, source_engine_id INTEGER,
		source_profile TEXT, source_size_bytes INTEGER, target_kind TEXT, storage_ref TEXT, file_name TEXT,
		size_bytes INTEGER, width INTEGER, height INTEGER, band_count INTEGER, source_srid INTEGER,
		source_crs TEXT, extent JSON, extent_srid INTEGER, status TEXT, metadata JSON, error_message TEXT,
		created_by INTEGER, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create raster COG result table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.raster_mosaic_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
		description TEXT, enabled BOOLEAN, schedule TEXT, next_run_at DATETIME, last_run_at DATETIME,
		last_execution_id TEXT, last_execution_status TEXT, config JSON, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create raster mosaic task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model_3d_glb_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
		description TEXT, enabled BOOLEAN, schedule TEXT, next_run_at DATETIME, last_run_at DATETIME,
		last_execution_id TEXT, last_execution_status TEXT, config JSON, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model 3d GLB task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model_3d_glb (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
		item_id INTEGER, locator TEXT, task_id INTEGER, last_execution_id TEXT, source_engine_id INTEGER,
		source_format TEXT, source_size_bytes INTEGER, storage_ref TEXT, file_name TEXT, size_bytes INTEGER,
		content_url TEXT, status TEXT, metadata JSON, error_message TEXT, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model 3d GLB result table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.gaussian_splat_ksplat_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
		description TEXT, enabled BOOLEAN, schedule TEXT, next_run_at DATETIME, last_run_at DATETIME,
		last_execution_id TEXT, last_execution_status TEXT, config JSON, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create gaussian splat KSplat task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.gaussian_splat_ksplat (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
		item_id INTEGER, locator TEXT, task_id INTEGER, last_execution_id TEXT, source_engine_id INTEGER,
		source_format TEXT, source_size_bytes INTEGER, storage_ref TEXT, file_name TEXT, size_bytes INTEGER,
		content_url TEXT, status TEXT, metadata JSON, error_message TEXT, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create gaussian splat KSplat result table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.point_cloud_copc_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
		description TEXT, enabled BOOLEAN, schedule TEXT, next_run_at DATETIME, last_run_at DATETIME,
		last_execution_id TEXT, last_execution_status TEXT, config JSON, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create point cloud COPC task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.point_cloud_copc (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
		item_id INTEGER, locator TEXT, task_id INTEGER, last_execution_id TEXT, source_engine_id INTEGER,
		source_format TEXT, source_size_bytes INTEGER, storage_ref TEXT, file_name TEXT, size_bytes INTEGER,
		content_url TEXT, status TEXT, metadata JSON, error_message TEXT, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create point cloud COPC result table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model3d_tiles_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
		description TEXT, enabled BOOLEAN, schedule TEXT, next_run_at DATETIME, last_run_at DATETIME,
		last_execution_id TEXT, last_execution_status TEXT, config JSON, created_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model3d tiles task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model3d_tiles (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
		item_id INTEGER, locator TEXT, task_id INTEGER, last_execution_id TEXT, source_engine_id INTEGER,
		source_format TEXT, source_size_bytes INTEGER, target_format TEXT, storage_ref TEXT,
		manifest_ref TEXT, file_count INTEGER, size_bytes INTEGER, status TEXT, metadata JSON,
		error_message TEXT, created_by INTEGER, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model3d tiles result table: %v", err)
	}
	return db
}

func createTileCacheExecutionRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID uint) models.TileCacheTask {
	t.Helper()
	task := models.TileCacheTask{
		TenantID: tenantID, Name: "tile-cache", Enabled: true,
		Config: commonModels.JSONMap{"version": 1, "target": commonModels.JSONMap{"item_fingerprint": "fp-1"}},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create tile cache task: %v", err)
	}
	return task
}

func newTileCacheRepositoryTestExecution(executionID string, tenantID int, createdAt time.Time) *commonExecution.TaskExecution {
	return newManagerRepositoryTestExecution(executionID, tenantID, commonExecution.TaskTypeVectorTileCacheGeneration, createdAt)
}

func newManagerRepositoryTestExecution(executionID string, tenantID int, taskType string, createdAt time.Time) *commonExecution.TaskExecution {
	return &commonExecution.TaskExecution{
		ExecutionID: executionID, TenantID: tenantID, Module: commonExecution.ModuleManager,
		TaskType: taskType, Source: commonExecution.ModuleManager,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}

func createVectorMaterializedViewExecutionRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID uint) models.VectorMaterializedViewTask {
	t.Helper()
	task := models.VectorMaterializedViewTask{
		TenantID: tenantID, Name: "vector-materialized-view", Enabled: true,
		Config: commonModels.JSONMap{"version": 1, "target": commonModels.JSONMap{"item_fingerprint": "vmv-fp"}},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create vector materialized view task: %v", err)
	}
	return task
}

func createVectorMaterializedViewExecutionRepositoryTestResult(
	t *testing.T, db *gorm.DB, task models.VectorMaterializedViewTask, executionID string,
) models.VectorMaterializedView {
	t.Helper()
	result := models.VectorMaterializedView{
		TenantID: task.TenantID, ItemFingerprint: "vmv-fp", TaskID: &task.ID, LastExecutionID: &executionID,
		SourceEngineID: 11, SourceSchema: "public", SourceTable: "roads", SourceGeometryColumn: "shape",
		SourceSRID: 4326, TargetSRID: 3857,
		TargetKind:   models.VectorMaterializedViewTargetKindSourceSchemaMaterializedView,
		TargetSchema: "public", TargetTable: "addp_vmv_test",
		TargetGeometryColumn:      models.VectorMaterializedViewTargetGeometryColumn,
		Status:                    models.VectorMaterializedViewStatusBuilding,
		SourceFingerprintSnapshot: commonModels.JSONMap{}, Metadata: commonModels.JSONMap{},
	}
	if err := db.Create(&result).Error; err != nil {
		t.Fatalf("create vector materialized view result: %v", err)
	}
	return result
}

func createRasterCOGExecutionRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID uint) models.RasterCOGTask {
	t.Helper()
	task := models.RasterCOGTask{
		TenantID: tenantID, Name: "raster-cog", Enabled: true,
		Config: commonModels.JSONMap{"version": 1, "target": commonModels.JSONMap{"item_fingerprint": "cog-fp"}},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create raster COG task: %v", err)
	}
	return task
}

func createRasterCOGExecutionRepositoryTestResult(
	t *testing.T, db *gorm.DB, task models.RasterCOGTask, executionID string,
) models.RasterCOG {
	t.Helper()
	result := models.RasterCOG{
		TenantID: task.TenantID, ItemFingerprint: "cog-fp", TaskID: &task.ID, LastExecutionID: &executionID,
		SourceEngineID: 11, TargetKind: models.RasterCOGTargetKindMinIO,
		StorageRef: `{"type":"object","bucket":"manager","object":"cog-fp.tif"}`,
		Status:     models.RasterCOGStatusBuilding, Metadata: commonModels.JSONMap{},
	}
	if err := db.Create(&result).Error; err != nil {
		t.Fatalf("create raster COG result: %v", err)
	}
	return result
}

func createRasterMosaicExecutionRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID uint) models.RasterMosaicTask {
	t.Helper()
	task := models.RasterMosaicTask{
		TenantID: tenantID, Name: "raster-mosaic", Enabled: true,
		Config: commonModels.JSONMap{"version": 1, "source": commonModels.JSONMap{"node_locator": "addp://engine/1/path/rasters?type=node"}},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create raster mosaic task: %v", err)
	}
	return task
}

func createModel3DGLBExecutionRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID uint) models.Model3DGLBTask {
	t.Helper()
	task := models.Model3DGLBTask{
		TenantID: tenantID, Name: "model-3d-glb", Enabled: true,
		Config: commonModels.JSONMap{"version": 1, "source": commonModels.JSONMap{"item_fingerprint": "glb-fp"}},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create model 3d GLB task: %v", err)
	}
	return task
}

func createModel3DGLBExecutionRepositoryTestResult(
	t *testing.T, db *gorm.DB, task models.Model3DGLBTask, executionID string,
) models.Model3DGLB {
	t.Helper()
	result := models.Model3DGLB{
		TenantID: task.TenantID, ItemFingerprint: "glb-fp", TaskID: &task.ID, LastExecutionID: &executionID,
		SourceEngineID: 11, SourceFormat: "osgb",
		StorageRef: `{"type":"object","bucket":"manager","object":"glb-fp.glb"}`,
		Status:     models.Model3DGLBStatusBuilding, Metadata: commonModels.JSONMap{},
	}
	if err := db.Create(&result).Error; err != nil {
		t.Fatalf("create model 3d GLB result: %v", err)
	}
	return result
}

func createGaussianSplatKSplatExecutionRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID uint) models.GaussianSplatKSplatTask {
	t.Helper()
	task := models.GaussianSplatKSplatTask{
		TenantID: tenantID, Name: "gaussian-splat-ksplat", Enabled: true,
		Config: commonModels.JSONMap{"version": 1, "source": commonModels.JSONMap{"item_fingerprint": "ksplat-fp"}},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create gaussian splat KSplat task: %v", err)
	}
	return task
}

func createGaussianSplatKSplatExecutionRepositoryTestResult(
	t *testing.T, db *gorm.DB, task models.GaussianSplatKSplatTask, executionID string,
) models.GaussianSplatKSplat {
	t.Helper()
	result := models.GaussianSplatKSplat{
		TenantID: task.TenantID, ItemFingerprint: "ksplat-fp", TaskID: &task.ID, LastExecutionID: &executionID,
		SourceEngineID: 11, SourceFormat: "ply",
		StorageRef: `{"type":"object","bucket":"manager","object":"ksplat-fp.ksplat"}`,
		Status:     models.GaussianSplatKSplatStatusBuilding, Metadata: commonModels.JSONMap{},
	}
	if err := db.Create(&result).Error; err != nil {
		t.Fatalf("create gaussian splat KSplat result: %v", err)
	}
	return result
}

func createPointCloudCOPCExecutionRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID uint) models.PointCloudCOPCTask {
	t.Helper()
	task := models.PointCloudCOPCTask{
		TenantID: tenantID, Name: "point-cloud-copc", Enabled: true,
		Config: commonModels.JSONMap{"version": 1, "source": commonModels.JSONMap{"item_fingerprint": "copc-fp"}},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create point cloud COPC task: %v", err)
	}
	return task
}

func createPointCloudCOPCExecutionRepositoryTestResult(
	t *testing.T, db *gorm.DB, task models.PointCloudCOPCTask, executionID string,
) models.PointCloudCOPC {
	t.Helper()
	result := models.PointCloudCOPC{
		TenantID: task.TenantID, ItemFingerprint: "copc-fp", TaskID: &task.ID, LastExecutionID: &executionID,
		SourceEngineID: 11, SourceFormat: "laz",
		StorageRef: `{"type":"object","bucket":"manager","object":"copc-fp.copc.laz"}`,
		Status:     models.PointCloudCOPCStatusBuilding, Metadata: commonModels.JSONMap{},
	}
	if err := db.Create(&result).Error; err != nil {
		t.Fatalf("create point cloud COPC result: %v", err)
	}
	return result
}

func createModel3DTilesExecutionRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID uint, fingerprint, targetFormat string) models.Model3DTilesTask {
	t.Helper()
	task := models.Model3DTilesTask{
		TenantID: tenantID, Name: "model3d-tiles", Enabled: true,
		Config: commonModels.JSONMap{
			"source":        commonModels.JSONMap{"item_fingerprint": fingerprint},
			"target_format": targetFormat,
		},
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create model3d tiles task: %v", err)
	}
	return task
}

func createModel3DTilesExecutionRepositoryTestResult(t *testing.T, db *gorm.DB, task models.Model3DTilesTask, executionID string) models.Model3DTiles {
	t.Helper()
	source := task.Config["source"].(map[string]interface{})
	result := models.Model3DTiles{
		TenantID: task.TenantID, ItemFingerprint: source["item_fingerprint"].(string), TaskID: &task.ID,
		LastExecutionID: &executionID, SourceEngineID: 11, SourceFormat: "osgb_scene",
		TargetFormat: task.Config["target_format"].(string), StorageRef: "storage-ref", ManifestRef: "tileset.json",
		Status: models.Model3DTilesStatusBuilding, Metadata: commonModels.JSONMap{},
	}
	if err := db.Create(&result).Error; err != nil {
		t.Fatalf("create model3d tiles result: %v", err)
	}
	return result
}
