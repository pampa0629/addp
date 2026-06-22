package service

import (
	"context"
	"errors"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/pdf"
	"github.com/addp/transfer/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateNewTaskConfigAcceptsRawCopy(t *testing.T) {
	err := validateNewTaskConfig(map[string]interface{}{
		"mode": "batch",
		"source": map[string]interface{}{
			"locator":        "addp://engine/1/path/docs/a.pdf?type=object",
			"data_type":      "document",
			"representation": "encoded",
			"format":         string(format.FormatPDF),
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/2/path/backup?type=directory",
			"name":           "a.pdf",
			"representation": "encoded",
			"policy":         map[string]interface{}{"write_mode": "overwrite"},
		},
	}, 1000)
	if err != nil {
		t.Fatalf("validateNewTaskConfig() error = %v", err)
	}
}

func TestValidateNewTaskConfigStillAcceptsTableTransfer(t *testing.T) {
	err := validateNewTaskConfig(map[string]interface{}{
		"mode":       "batch",
		"batch_size": 100,
		"source": map[string]interface{}{
			"locator":        "addp://engine/1/path/public/roads?type=table",
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/2/path/exports?type=directory",
			"name":           "roads.csv",
			"data_type":      "table",
			"representation": "encoded",
			"format":         string(format.FormatCSV),
			"policy":         map[string]interface{}{"write_mode": "overwrite"},
		},
	}, 1000)
	if err != nil {
		t.Fatalf("validateNewTaskConfig() error = %v", err)
	}
}

func TestCreateTaskPersistsNextRunAtWhenEnabled(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil, nil)

	task, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name:     "scheduled",
		TaskType: commonExecution.TaskTypeSync,
		Config:   validTableTransferTaskConfig(),
		Schedule: "0 */5 * * * *",
		Enabled:  boolPtr(true),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if !task.Enabled || task.NextRunAt == nil {
		t.Fatalf("task enabled/next_run_at = %v/%v, want true/non-nil", task.Enabled, task.NextRunAt)
	}
}

func TestCreateTaskKeepsScheduledTaskDisabledWithoutNextRunAt(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil, nil)

	task, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name:     "scheduled-disabled",
		TaskType: commonExecution.TaskTypeSync,
		Config:   validTableTransferTaskConfig(),
		Schedule: "0 */5 * * * *",
		Enabled:  boolPtr(false),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.Enabled || task.NextRunAt != nil {
		t.Fatalf("task enabled/next_run_at = %v/%v, want false/nil", task.Enabled, task.NextRunAt)
	}
}

func TestUpdateTaskClearsNextRunAtWhenScheduleRemoved(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil, nil)

	task, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name:     "scheduled",
		TaskType: commonExecution.TaskTypeSync,
		Config:   validTableTransferTaskConfig(),
		Schedule: "0 */5 * * * *",
		Enabled:  boolPtr(true),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	updated, err := taskSvc.UpdateTask(context.Background(), task.ID, 7, &models.UpdateTaskRequest{
		Schedule: strPtr(""),
	})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if updated.Enabled || updated.NextRunAt != nil {
		t.Fatalf("updated enabled/next_run_at = %v/%v, want false/nil", updated.Enabled, updated.NextRunAt)
	}
}

func TestCreateTaskRejectsMissingTaskType(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil, nil)

	_, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name:   "missing-task-type",
		Config: validTableTransferTaskConfig(),
	}, 7, 9)
	if err == nil {
		t.Fatal("CreateTask() error = nil, want unsupported task_type")
	}
	if !errors.Is(err, ErrUnsupportedTaskType) {
		t.Fatalf("CreateTask() error = %v, want ErrUnsupportedTaskType", err)
	}
}

func TestStartTaskRejectsPersistedNonSyncTaskType(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	taskSvc := NewTaskService(db, nil, nil, nil)

	task := models.TransferTask{
		TenantID:  7,
		Name:      "legacy",
		TaskType:  "import",
		Config:    validTableTransferTaskConfig(),
		BatchSize: 100,
		Status:    models.TaskStatusIdle,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create legacy task: %v", err)
	}

	_, err := taskSvc.StartTask(context.Background(), task.ID, 7, 9)
	if err == nil {
		t.Fatal("StartTask() error = nil, want unsupported task_type")
	}
	if !errors.Is(err, ErrUnsupportedTaskType) {
		t.Fatalf("StartTask() error = %v, want ErrUnsupportedTaskType", err)
	}
}

func newTransferTaskServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS transfer").Error; err != nil {
		t.Fatalf("attach transfer schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE transfer.transfer_tasks (
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
		)
	`).Error; err != nil {
		t.Fatalf("create transfer_tasks table: %v", err)
	}
	return db
}

func boolPtr(v bool) *bool { return &v }

func strPtr(v string) *string { return &v }

func validTableTransferTaskConfig() map[string]interface{} {
	return map[string]interface{}{
		"mode": "batch",
		"source": map[string]interface{}{
			"locator":        "addp://engine/1/path/public/roads?type=table",
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/2/path/exports?type=directory",
			"name":           "roads.csv",
			"data_type":      "table",
			"representation": "encoded",
			"format":         string(format.FormatCSV),
			"policy":         map[string]interface{}{"write_mode": "overwrite"},
		},
	}
}
