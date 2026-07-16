package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
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

	claimed, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec)
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
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, duplicate); !errors.Is(err, commonAPI.ErrConflict) {
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

func TestTileCacheStartRollsBackWhenOwnerSummaryCannotAdvance(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo := NewTileCacheRepository(db)
	task := createTileCacheExecutionRepositoryTestTask(t, db, 8)
	createdAt := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	exec := newTileCacheRepositoryTestExecution("manager-tile-start-rollback", 8, createdAt)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec); err != nil {
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
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec); err != nil {
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
		"manager-vmv-atomic-duplicate", 10, commonExecution.TaskTypeVectorMaterializedViewGeneration, createdAt,
	)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, duplicate); !errors.Is(err, commonAPI.ErrConflict) {
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
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec); err != nil {
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
	for _, schema := range []string{"common", "manager"} {
		if err := db.Exec("ATTACH DATABASE ':memory:' AS " + schema).Error; err != nil {
			t.Fatalf("attach %s schema: %v", schema, err)
		}
	}
	if err := db.Exec(tileCacheRepositoryExecutionTableSQL).Error; err != nil {
		t.Fatalf("create common execution test table: %v", err)
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
		storage_ref TEXT, extent JSON, extent_srid INTEGER, min_zoom INTEGER, max_zoom INTEGER,
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
	return db
}

const tileCacheRepositoryExecutionTableSQL = `CREATE TABLE common.task_executions (
	id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, execution_id TEXT NOT NULL UNIQUE,
	module TEXT NOT NULL, task_type TEXT NOT NULL, source TEXT NOT NULL DEFAULT '', source_task_id TEXT,
	source_task_name TEXT, parent_execution_id TEXT, status TEXT NOT NULL, progress INTEGER,
	current_step TEXT, trigger_type TEXT NOT NULL, triggered_by INTEGER, execution_config JSON,
	error_details JSON, metadata JSON, execution_time_ms INTEGER, rows_affected INTEGER,
	records_read INTEGER, records_written INTEGER, bytes_read INTEGER, bytes_written INTEGER,
	started_at DATETIME, completed_at DATETIME, created_at DATETIME, updated_at DATETIME
)`

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
