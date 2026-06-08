package repository

import (
	"fmt"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGetStatisticsUsesImportExecutionsAndStringSourceTaskID(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	repo := NewTaskRepository(db)

	taskA := createTaskRepositoryTestTask(t, db, 7, "task-a")
	taskB := createTaskRepositoryTestTask(t, db, 7, "task-b")
	createTaskRepositoryTestTask(t, db, 7, "task-c")

	createTaskRepositoryTestExecution(t, db, taskA, commonExecution.TaskTypeImport, commonExecution.ExecutionStatusSuccess, time.Now().Add(-time.Hour))
	createTaskRepositoryTestExecution(t, db, taskA, "transfer", commonExecution.ExecutionStatusFailed, time.Now())
	createTaskRepositoryTestExecution(t, db, taskB, commonExecution.TaskTypeImport, commonExecution.ExecutionStatusRunning, time.Now())

	stats, err := repo.GetStatistics(7)
	if err != nil {
		t.Fatalf("GetStatistics failed: %v", err)
	}

	if stats.NotExecutedTasks != 1 {
		t.Fatalf("NotExecutedTasks = %d, want 1", stats.NotExecutedTasks)
	}
	if stats.LastSuccessTasks != 1 {
		t.Fatalf("LastSuccessTasks = %d, want 1", stats.LastSuccessTasks)
	}
	if stats.LastRunningTasks != 1 {
		t.Fatalf("LastRunningTasks = %d, want 1", stats.LastRunningTasks)
	}
	if stats.LastFailedTasks != 0 {
		t.Fatalf("LastFailedTasks = %d, want 0", stats.LastFailedTasks)
	}
	if stats.TotalExecutions != 2 {
		t.Fatalf("TotalExecutions = %d, want 2", stats.TotalExecutions)
	}
}

func newTaskRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS transfer").Error; err != nil {
		t.Fatalf("attach transfer schema: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}

	statements := []string{
		`CREATE TABLE transfer.transfer_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			task_type TEXT NOT NULL,
			config JSON,
			schedule TEXT,
			batch_size INTEGER,
			enabled BOOLEAN,
			auto_scan_metadata BOOLEAN,
			status TEXT,
			progress REAL,
			created_by INTEGER,
			last_execution_id TEXT,
			last_execution_status TEXT,
			last_run_at DATETIME,
			next_run_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE common.task_executions (
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
		)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	return db
}

func createTaskRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID uint, name string) models.TransferTask {
	t.Helper()
	task := models.TransferTask{
		TenantID:  tenantID,
		Name:      name,
		TaskType:  commonExecution.TaskTypeImport,
		Config:    models.JSONMap{"mode": "batch"},
		BatchSize: 100,
		Status:    models.TaskStatusIdle,
		Progress:  0,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func createTaskRepositoryTestExecution(t *testing.T, db *gorm.DB, task models.TransferTask, taskType string, status string, startedAt time.Time) {
	t.Helper()
	taskName := task.Name
	execution := commonExecution.TaskExecution{
		TenantID:       int(task.TenantID),
		ExecutionID:    fmt.Sprintf("%s-%s-%d", task.Name, taskType, startedAt.UnixNano()),
		Module:         commonExecution.ModuleTransfer,
		TaskType:       taskType,
		Source:         commonExecution.ModuleTransfer,
		SourceTaskID:   commonExecution.NewSourceTaskIDFromUint(task.ID),
		SourceTaskName: &taskName,
		Status:         status,
		Progress:       100,
		TriggerType:    commonExecution.TriggerTypeManual,
		StartedAt:      &startedAt,
	}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
}
