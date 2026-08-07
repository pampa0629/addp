package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCheckTaskExecutionLifecycleIsAtomic(t *testing.T) {
	db := newCheckTaskRepositoryTestDB(t)
	repo := NewCheckTaskRepository(db)
	task := createCheckTaskRepositoryTestTask(t, db, 7)
	createdAt := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	exec := newQualityRepositoryTestExecution("quality-atomic-1", 7, createdAt)

	claimed, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec)
	if err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if claimed.LastExecutionID != exec.ExecutionID || claimed.LastExecutionStatus != commonExecution.ExecutionStatusPending {
		t.Fatalf("claimed task summary = %s/%s, want %s/pending", claimed.LastExecutionID, claimed.LastExecutionStatus, exec.ExecutionID)
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

	duplicate := newQualityRepositoryTestExecution("quality-atomic-duplicate", 7, createdAt)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, duplicate); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("duplicate ClaimExecution error = %v, want conflict", err)
	}

	startedAt := createdAt.Add(time.Minute)
	if err := repo.StartExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, startedAt); err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	var runningTask models.CheckTask
	if err := db.First(&runningTask, task.ID).Error; err != nil {
		t.Fatalf("load running task: %v", err)
	}
	if runningTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning || runningTask.LastRunAt == nil || !runningTask.LastRunAt.Equal(startedAt) {
		t.Fatalf("running task summary status=%s last_run_at=%v", runningTask.LastExecutionStatus, runningTask.LastRunAt)
	}

	completedAt := startedAt.Add(2 * time.Minute)
	if err := repo.CompleteExecution(context.Background(), task.ID, task.TenantID, exec.ExecutionID, map[string]interface{}{
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
	if runningTask.LastExecutionStatus != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("completed task status = %s, want success", runningTask.LastExecutionStatus)
	}
}

func TestCheckTaskStartRollsBackWhenOwnerSummaryCannotAdvance(t *testing.T) {
	db := newCheckTaskRepositoryTestDB(t)
	repo := NewCheckTaskRepository(db)
	task := createCheckTaskRepositoryTestTask(t, db, 8)
	createdAt := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	exec := newQualityRepositoryTestExecution("quality-start-rollback", 8, createdAt)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if err := db.Delete(&models.CheckTask{}, task.ID).Error; err != nil {
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

func newCheckTaskRepositoryTestDB(t *testing.T) *gorm.DB {
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
	for _, schema := range []string{"common", "quality"} {
		if err := db.Exec("ATTACH DATABASE ':memory:' AS " + schema).Error; err != nil {
			t.Fatalf("attach %s schema: %v", schema, err)
		}
	}
	if err := db.Exec(qualityRepositoryExecutionTableSQL).Error; err != nil {
		t.Fatalf("create common execution test table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE quality.check_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		engine_id INTEGER NOT NULL,
		schema_name TEXT,
		table_name TEXT,
		enabled BOOLEAN,
		created_by INTEGER NOT NULL,
		updated_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		last_run_at DATETIME,
		next_run_at DATETIME,
		last_execution_id TEXT,
		last_execution_status TEXT
	)`).Error; err != nil {
		t.Fatalf("create quality check task test table: %v", err)
	}
	return db
}

const qualityRepositoryExecutionTableSQL = `CREATE TABLE common.task_executions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	tenant_id INTEGER NOT NULL,
	execution_id TEXT NOT NULL UNIQUE,
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
	actor_principal_id INTEGER,
	actor_tenant_membership_id INTEGER,
	issued_authorization_version INTEGER,
	execution_authorization_id INTEGER,
	authorization_effects TEXT,
	authorization_expires_at DATETIME,
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
)`

func createCheckTaskRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID int64) models.CheckTask {
	t.Helper()
	task := models.CheckTask{
		TenantID: tenantID, Name: "quality-check", EngineID: 12, SchemaName: "public", Table: "orders",
		Enabled: true, CreatedBy: 1,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create check task: %v", err)
	}
	return task
}

func newQualityRepositoryTestExecution(executionID string, tenantID int, createdAt time.Time) *commonExecution.TaskExecution {
	return &commonExecution.TaskExecution{
		ExecutionID: executionID, TenantID: tenantID, Module: commonExecution.ModuleQuality,
		TaskType: commonExecution.TaskTypeQualityCheck, Source: commonExecution.ModuleQuality,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}
