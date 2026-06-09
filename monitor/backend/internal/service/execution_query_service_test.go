package service

import (
	"context"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGetExecutionTreeByExecutionID(t *testing.T) {
	db := newExecutionQueryServiceTestDB(t)
	repo := commonExecution.NewTaskExecutionRepository(db)
	service := NewExecutionQueryService(repo)

	insertExecutionQueryServiceTestRow(t, db, 1, "root-exec", nil)
	parentID := "root-exec"
	insertExecutionQueryServiceTestRow(t, db, 2, "child-exec", &parentID)

	tree, err := service.GetExecutionTreeByExecutionID(context.Background(), "root-exec", 7)
	if err != nil {
		t.Fatalf("GetExecutionTreeByExecutionID() error = %v", err)
	}
	if tree.Execution.ExecutionID != "root-exec" {
		t.Fatalf("root execution_id = %s, want root-exec", tree.Execution.ExecutionID)
	}
	if len(tree.Children) != 1 {
		t.Fatalf("children len = %d, want 1", len(tree.Children))
	}
	if tree.Children[0].Execution.ExecutionID != "child-exec" {
		t.Fatalf("child execution_id = %s, want child-exec", tree.Children[0].Execution.ExecutionID)
	}
}

func newExecutionQueryServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE common.task_executions (
		id INTEGER PRIMARY KEY,
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
		trigger_type TEXT NOT NULL,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create task_executions table: %v", err)
	}
	return db
}

func insertExecutionQueryServiceTestRow(t *testing.T, db *gorm.DB, id int, executionID string, parentExecutionID *string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO common.task_executions (
		id, tenant_id, execution_id, module, task_type, source, parent_execution_id,
		status, progress, trigger_type, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, 7, executionID, commonExecution.ModuleOrchestrator, commonExecution.TaskTypeOrchestration,
		commonExecution.ModuleOrchestrator, parentExecutionID, commonExecution.ExecutionStatusSuccess,
		100, commonExecution.TriggerTypeManual, "2026-01-01 10:00:00", "2026-01-01 10:00:00",
	).Error; err != nil {
		t.Fatalf("insert task execution: %v", err)
	}
}
