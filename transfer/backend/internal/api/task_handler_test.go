package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type taskHandlerQueue struct{}

func (taskHandlerQueue) EnqueueExecuteTask(context.Context, uint, uint, uint) error { return nil }
func (taskHandlerQueue) Close() error                                               { return nil }

func TestTaskHandlerListTasksRejectsUnsupportedTaskType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewTaskHandler(nil)
	router.GET("/tasks", handler.ListTasks)

	req := httptest.NewRequest(http.MethodGet, "/tasks?task_type=export", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestTaskHandlerListTasksUsesStandardItemsShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTransferTaskHandlerTestDB(t)
	taskSvc := service.NewTaskService(db, nil, nil, taskHandlerQueue{})
	_, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name:     "sync task",
		TaskType: commonExecution.TaskTypeSync,
		Config:   validTransferTaskHandlerConfig(),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", uint(7))
		c.Next()
	})
	handler := NewTaskHandler(taskSvc)
	router.GET("/tasks", handler.ListTasks)

	req := httptest.NewRequest(http.MethodGet, "/tasks?task_type=sync", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Items []struct {
			TaskType string `json:"task_type"`
		} `json:"items"`
		Data []struct {
			TaskType string `json:"task_type"`
		} `json:"data"`
		Total      int64 `json:"total"`
		TotalPages *int  `json:"total_pages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if len(resp.Items) != 1 || resp.Items[0].TaskType != commonExecution.TaskTypeSync {
		t.Fatalf("items = %#v, want one sync task; body=%s", resp.Items, w.Body.String())
	}
	if resp.Data != nil {
		t.Fatalf("data = %#v, want omitted in TaskProvider standard response; body=%s", resp.Data, w.Body.String())
	}
	if resp.TotalPages != nil {
		t.Fatalf("total_pages = %d, want omitted in TaskProvider standard response; body=%s", *resp.TotalPages, w.Body.String())
	}
}

func TestProviderExecuteTaskRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/tasks/:task_type/:id/execute", NewTaskHandler(nil).ProviderExecuteTask)

	req := httptest.NewRequest(http.MethodPost, "/tasks/sync/1/execute", strings.NewReader(`{"legacy":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unknown field") {
		t.Fatalf("body = %s, want unknown field error", w.Body.String())
	}
}

func TestProviderExecuteTaskUsesStandardExecutionShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTransferTaskHandlerTestDB(t)
	taskSvc := service.NewTaskService(db, nil, nil, taskHandlerQueue{})
	executionSvc := service.NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	taskSvc.SetExecutionService(executionSvc)
	task, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name:     "sync task",
		TaskType: commonExecution.TaskTypeSync,
		Config:   validTransferTaskHandlerConfig(),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", uint(7))
		c.Set("user_id", uint(9))
		c.Next()
	})
	router.POST("/tasks/:task_type/:id/execute", NewTaskHandler(taskSvc).ProviderExecuteTask)

	req := httptest.NewRequest(http.MethodPost, "/tasks/sync/1/execute", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var resp struct {
		ExecutionID string `json:"execution_id"`
		Status      string `json:"status"`
		ID          uint   `json:"id"`
		Data        any    `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if resp.ExecutionID == "" || resp.Status != string(models.ExecutionStatusPending) {
		t.Fatalf("response = %#v, want execution_id and pending status; body=%s", resp, w.Body.String())
	}
	if resp.ID != 0 || resp.Data != nil {
		t.Fatalf("response leaks non-standard fields id=%d data=%#v body=%s", resp.ID, resp.Data, w.Body.String())
	}

	var stored models.TransferTask
	if err := db.First(&stored, task.ID).Error; err != nil {
		t.Fatalf("load transfer task: %v", err)
	}
	if stored.Status != models.TaskStatusRunning {
		t.Fatalf("task status = %s, want running", stored.Status)
	}
}

func newTransferTaskHandlerTestDB(t *testing.T) *gorm.DB {
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
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE common.task_executions (
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
		)
	`).Error; err != nil {
		t.Fatalf("create task_executions table: %v", err)
	}
	return db
}

func validTransferTaskHandlerConfig() map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
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
			"policy":         map[string]interface{}{"apply_mode": "replace"},
		},
	}
}
