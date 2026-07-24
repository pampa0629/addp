package service

import (
	"context"
	"errors"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/orchestrator/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExecutionLifecycleUsesRealStartAndTerminalTimes(t *testing.T) {
	db := newOrchestratorExecutionServiceTestDB(t)
	if err := db.Exec(`INSERT INTO orchestrator.orchestrations
		(id, tenant_id, name, steps, enabled, schedule, created_at, updated_at)
		VALUES (11, 7, 'daily orchestration', '[]', false, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error; err != nil {
		t.Fatalf("insert orchestration: %v", err)
	}
	service := NewExecutionService(db, repository.NewOrchestrationRepository(db))
	if _, err := service.CreateExecution(context.Background(), 11, 8, commonExecution.TriggerTypeManual); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant CreateExecution error = %v, want record not found", err)
	}
	if _, err := service.CreateExecution(context.Background(), 11, 0, commonExecution.TriggerTypeManual); err == nil {
		t.Fatal("tenant-less CreateExecution error is nil")
	}
	parentExecutionID := "parent-1"
	execution, err := service.CreateExecutionWithContext(
		context.Background(), 11, 7, commonExecution.TriggerTypeManual,
		commonExecution.ModuleOrchestrator, &parentExecutionID, 9,
	)
	if err != nil {
		t.Fatalf("CreateExecutionWithContext: %v", err)
	}
	if execution.Status != commonExecution.ExecutionStatusPending || execution.StartedAt != nil {
		t.Fatalf("created execution status=%s started_at=%v, want pending nil", execution.Status, execution.StartedAt)
	}
	if execution.SourceTaskID == nil || *execution.SourceTaskID != "11" || execution.SourceTaskName == nil || *execution.SourceTaskName == "" {
		t.Fatalf("persistent task identity = id:%v name:%v", execution.SourceTaskID, execution.SourceTaskName)
	}
	if execution.ParentExecutionID == nil || *execution.ParentExecutionID != parentExecutionID {
		t.Fatalf("parent_execution_id = %v, want %s", execution.ParentExecutionID, parentExecutionID)
	}
	if _, err := service.GetExecution(context.Background(), uint(execution.ID), 0); err == nil {
		t.Fatal("tenant-less GetExecution error is nil")
	}
	if _, err := service.GetExecutionByExecutionID(context.Background(), execution.ExecutionID, 0); err == nil {
		t.Fatal("tenant-less GetExecutionByExecutionID error is nil")
	}
	if _, err := service.GetExecution(context.Background(), uint(execution.ID), 8); err == nil {
		t.Fatal("cross-tenant GetExecution error is nil")
	}
	if _, err := service.GetExecutionByExecutionID(context.Background(), execution.ExecutionID, 8); err == nil {
		t.Fatal("cross-tenant GetExecutionByExecutionID error is nil")
	}
	if _, err := service.getExecutionInternal(context.Background(), uint(execution.ID)); err != nil {
		t.Fatalf("getExecutionInternal: %v", err)
	}

	if err := service.UpdateStatus(context.Background(), uint(execution.ID), commonExecution.ExecutionStatusRunning); err != nil {
		t.Fatalf("UpdateStatus(running): %v", err)
	}
	started, err := service.GetExecution(context.Background(), uint(execution.ID), 7)
	if err != nil {
		t.Fatalf("load running execution: %v", err)
	}
	if started.Status != commonExecution.ExecutionStatusRunning || started.StartedAt == nil {
		t.Fatalf("running execution status=%s started_at=%v", started.Status, started.StartedAt)
	}

	if err := service.FinishExecution(context.Background(), uint(execution.ID), commonExecution.ExecutionStatusFailed, "step failed", nil); err != nil {
		t.Fatalf("FinishExecution: %v", err)
	}
	finished, err := service.GetExecution(context.Background(), uint(execution.ID), 7)
	if err != nil {
		t.Fatalf("load finished execution: %v", err)
	}
	if finished.CompletedAt == nil || finished.ExecutionTimeMs == nil || *finished.ExecutionTimeMs < 0 {
		t.Fatalf("terminal times completed_at=%v execution_time_ms=%v", finished.CompletedAt, finished.ExecutionTimeMs)
	}
	if finished.ErrorDetails["message"] != "step failed" {
		t.Fatalf("error_details = %#v", finished.ErrorDetails)
	}
}

func newOrchestratorExecutionServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:orchestrator_execution_contract?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, schema := range []string{"orchestrator", "common"} {
		if err := db.Exec("ATTACH DATABASE ':memory:' AS " + schema).Error; err != nil {
			t.Fatalf("attach %s schema: %v", schema, err)
		}
	}
	statements := []string{
		`CREATE TABLE orchestrator.orchestrations (
			id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT,
			steps JSON NOT NULL, editor_layout JSON NOT NULL DEFAULT '{}', enabled BOOLEAN, schedule TEXT, last_run_at DATETIME, next_run_at DATETIME,
			last_execution_id TEXT, last_execution_status TEXT, created_by INTEGER,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
		)`,
		`CREATE TABLE common.task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, execution_id TEXT NOT NULL,
			module TEXT NOT NULL, task_type TEXT NOT NULL, source TEXT NOT NULL, source_task_id TEXT,
			source_task_name TEXT, parent_execution_id TEXT, status TEXT NOT NULL, progress INTEGER,
			current_step TEXT, trigger_type TEXT NOT NULL, triggered_by INTEGER, execution_config JSON,
			error_details JSON, metadata JSON, execution_time_ms INTEGER, rows_affected INTEGER,
			records_read INTEGER, records_written INTEGER, bytes_read INTEGER, bytes_written INTEGER,
			started_at DATETIME, completed_at DATETIME, created_at DATETIME, updated_at DATETIME
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	return db
}
