package service

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEmbeddingTaskExecuteReusesSingleExecution(t *testing.T) {
	db := newEmbeddingTaskServiceTestDB(t)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	embeddingSvc := &EmbeddingService{
		vectorRepo:   embeddingRepo,
		taskExecRepo: taskExecRepo,
		cfg:          &config.Config{},
		log:          slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}
	taskSvc := NewEmbeddingTaskService(embeddingRepo, embeddingSvc, taskExecRepo)

	task := &models.EmbeddingTask{
		TenantID:  7,
		Name:      "目录向量化",
		Enabled:   true,
		EngineID:  11,
		Bucket:    "bucket-a",
		Prefix:    "datasets/",
		Recursive: true,
	}
	if err := embeddingRepo.CreateEmbeddingTask(context.Background(), task); err != nil {
		t.Fatalf("create embedding task: %v", err)
	}

	parentExecutionID := "parent-execution"
	executionID, err := taskSvc.Execute(context.Background(), task.ID, task.TenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleOrchestrator, &parentExecutionID)
	if err != nil {
		t.Fatalf("execute embedding task: %v", err)
	}

	exec := waitForEmbeddingTaskExecution(t, taskExecRepo, executionID, int(task.TenantID))
	if exec.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("execution status = %s, want failed", exec.Status)
	}
	if exec.Module != commonExecution.ModuleManager || exec.TaskType != commonExecution.TaskTypeEmbedding {
		t.Fatalf("execution owner = %s/%s, want manager/embedding", exec.Module, exec.TaskType)
	}
	if exec.Source != commonExecution.ModuleOrchestrator {
		t.Fatalf("execution source = %s, want orchestrator", exec.Source)
	}
	if exec.ParentExecutionID == nil || *exec.ParentExecutionID != parentExecutionID {
		t.Fatalf("parent execution = %#v, want %s", exec.ParentExecutionID, parentExecutionID)
	}

	var count int64
	if err := db.Model(&commonExecution.TaskExecution{}).
		Where("module = ? AND task_type = ?", commonExecution.ModuleManager, commonExecution.TaskTypeEmbedding).
		Count(&count).Error; err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if count != 1 {
		t.Fatalf("manager embedding execution count = %d, want 1", count)
	}

	var refreshed models.EmbeddingTask
	if err := db.First(&refreshed, task.ID).Error; err != nil {
		t.Fatalf("load refreshed task: %v", err)
	}
	if refreshed.LastExecutionID == nil || *refreshed.LastExecutionID != executionID {
		t.Fatalf("last_execution_id = %#v, want %s", refreshed.LastExecutionID, executionID)
	}
	if refreshed.LastExecutionStatus == nil || *refreshed.LastExecutionStatus != commonExecution.ExecutionStatusFailed {
		t.Fatalf("last_execution_status = %#v, want failed", refreshed.LastExecutionStatus)
	}
}

func newEmbeddingTaskServiceTestDB(t *testing.T) *gorm.DB {
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
	if err := db.Exec(`CREATE TABLE manager.embedding_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		enabled BOOLEAN,
		last_execution_id TEXT,
		last_execution_status TEXT,
		last_run_at DATETIME,
		created_by INTEGER,
		engine_id INTEGER NOT NULL,
		bucket TEXT NOT NULL,
		prefix TEXT,
		recursive BOOLEAN,
		modality TEXT,
		file_types TEXT,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create embedding_tasks table: %v", err)
	}
	return db
}

func waitForEmbeddingTaskExecution(t *testing.T, repo *commonExecution.TaskExecutionRepository, executionID string, tenantID int) *commonExecution.TaskExecution {
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
