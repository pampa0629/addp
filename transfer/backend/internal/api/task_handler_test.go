package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/common/format"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskHandlerListTasksRejectsUnsupportedTaskType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewTaskHandler(nil)
	router.GET("/task-provider/tasks", handler.ProviderListTasks)

	req := httptest.NewRequest(http.MethodGet, "/task-provider/tasks?task_type=export", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestReplayTaskRejectsTaskConfigOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		`{"ranges":[{"partition":"0","start_offset":1,"end_offset":2}],"target":{"parent_locator":"addp://engine/8/path/replay?type=schema","name":"orders_replay"},"source":{"locator":"override"}}`,
		`{"ranges":[{"partition":"0","start_offset":1,"end_offset":2}],"target":{"parent_locator":"addp://engine/8/path/replay?type=schema","name":"orders_replay","policy":{"apply_mode":"append"}}}`,
	} {
		router := gin.New()
		router.POST("/task-definitions/:id/replay", NewTaskHandler(nil).ReplayTask)
		req := httptest.NewRequest(http.MethodPost, "/task-definitions/1/replay", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s request=%s", w.Code, w.Body.String(), body)
		}
	}
}

func TestApproveSchemaChangeRejectsUnknownAndEmptyFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		`{}`,
		`{"fields":[]}`,
		`{"fields":[{"source":"added","target":"added","target_type":"string","nullable":true,"legacy":true}]}`,
	} {
		router := gin.New()
		router.POST("/task-definitions/:id/schema-change/approve", NewTaskHandler(nil).ApproveSchemaChange)
		request := httptest.NewRequest(http.MethodPost, "/task-definitions/1/schema-change/approve", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestRespondReplayTaskErrorDoesNotExposeInternalDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/replay-error", func(c *gin.Context) {
		respondReplayTaskError(c, fmt.Errorf("kafka connection failed: password=secret"))
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/replay-error", nil))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteTaskErrorsUseStableStatusAndDoNotExposeCleanupDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/delete-running", func(c *gin.Context) {
		respondTaskServiceError(c, service.ErrTaskDeleteRequiresStopped)
	})
	router.GET("/delete-cleanup", func(c *gin.Context) {
		respondTaskServiceError(c, fmt.Errorf("%w: broker password=secret", service.ErrTaskDeleteCleanupFailed))
	})

	running := httptest.NewRecorder()
	router.ServeHTTP(running, httptest.NewRequest(http.MethodGet, "/delete-running", nil))
	if running.Code != http.StatusConflict {
		t.Fatalf("running delete status=%d body=%s", running.Code, running.Body.String())
	}
	cleanup := httptest.NewRecorder()
	router.ServeHTTP(cleanup, httptest.NewRequest(http.MethodGet, "/delete-cleanup", nil))
	if cleanup.Code != http.StatusServiceUnavailable || strings.Contains(cleanup.Body.String(), "secret") {
		t.Fatalf("cleanup delete status=%d body=%s", cleanup.Code, cleanup.Body.String())
	}
}

func TestTaskHandlerDeadLettersAreTaskScopedAndDoNotExposePayloadReference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTransferTaskHandlerTestDB(t)
	task := &models.TransferTask{
		TenantID: 7, Name: "dead letter task", TaskType: commonExecution.TaskTypeSync,
		Config: validTransferTaskHandlerContinuousConfig(), BatchSize: 100,
		Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	observedAt := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	deadLetter := &models.DeadLetter{
		Identity: "a220d5ad-d86e-52ca-ad4f-5ff2d8bfad1c", TenantID: 7, TaskID: task.ID,
		ApplyIdentity: task.ApplyIdentity, FirstExecutionID: "execution-1", LastExecutionID: "execution-2",
		SourceIdentity: "addp://engine/30/path/orders.events?type=topic", SourceTopic: "orders.events",
		SourcePartition: "2", SourceOffset: 41, ErrorCode: "invalid_json_object", ErrorCategory: "record_decode",
		ErrorMessage: "record value must be a JSON object", PayloadTopic: "__addp_dlq.7.1", PayloadPartition: 0,
		PayloadOffset: 19, PayloadAvailable: true, FirstObservedAt: observedAt, LastObservedAt: observedAt, OccurrenceCount: 2,
	}
	if err := db.Create(deadLetter).Error; err != nil {
		t.Fatal(err)
	}

	taskSvc := service.NewTaskService(db, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTransferTestAuthContext(t, c, 7, 9)
		c.Next()
	})
	handler := NewTaskHandler(taskSvc)
	router.GET("/task-definitions/:id/dead-letters", handler.ListDeadLetters)
	router.GET("/task-definitions/:id/dead-letters/:identity", handler.GetDeadLetter)

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/task-definitions/%d/dead-letters?source_partition=2&page=1&page_size=20", task.ID), nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var response struct {
		Data  []map[string]interface{} `json:"data"`
		Total int64                    `json:"total"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || len(response.Data) != 1 {
		t.Fatalf("list response=%s", list.Body.String())
	}
	for _, forbidden := range []string{"tenant_id", "task_id", "apply_identity", "payload_topic", "payload_partition", "payload_offset"} {
		if _, exists := response.Data[0][forbidden]; exists {
			t.Fatalf("public dead-letter response exposed %s: %s", forbidden, list.Body.String())
		}
	}

	detail := httptest.NewRecorder()
	router.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/task-definitions/%d/dead-letters/%s", task.ID, deadLetter.Identity), nil))
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), "__addp_dlq") {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/task-definitions/%d/dead-letters/not-a-uuid", task.ID), nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid identity status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestTaskHandlerResumeSchemaBlockedCDCReturnsConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTransferTaskHandlerTestDB(t)
	task := &models.TransferTask{
		TenantID: 7, Name: "blocked cdc", TaskType: commonExecution.TaskTypeSync,
		Config: validTransferTaskHandlerCDCConfig(), BatchSize: 100,
		Status: models.TaskStatusBlocked, DesiredState: models.TaskDesiredStateRunning,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	taskSvc := service.NewTaskService(db, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTransferTestAuthContext(t, c, 7, 9)
		c.Next()
	})
	router.POST("/task-definitions/:id/resume", NewTaskHandler(taskSvc).ResumeTask)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/task-definitions/%d/resume", task.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "结构变化") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestExecutionHandlerRetrySchemaBlockedCDCReturnsConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTransferTaskHandlerTestDB(t)
	task := &models.TransferTask{
		TenantID: 7, Name: "blocked cdc", TaskType: commonExecution.TaskTypeSync,
		Config: validTransferTaskHandlerCDCConfig(), BatchSize: 100,
		Status: models.TaskStatusBlocked, DesiredState: models.TaskDesiredStateRunning,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	execution := commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: "blocked-cdc-execution", Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleTransfer,
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), Status: commonExecution.ExecutionStatusFailed,
		TriggerType: commonExecution.TriggerTypeManual,
	}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	executionSvc := service.NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTransferTestAuthContext(t, c, 7, 9)
		c.Next()
	})
	router.POST("/executions/:execution_id/retry", NewExecutionHandler(executionSvc).RetryExecution)
	req := httptest.NewRequest(http.MethodPost, "/executions/blocked-cdc-execution/retry", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "结构变化") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestTaskHandlerListTasksUsesStandardItemsShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTransferTaskHandlerTestDB(t)
	taskSvc := service.NewTaskService(db, nil, nil)
	_, err := taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name:     "sync task",
		TaskType: commonExecution.TaskTypeSync,
		Config:   validTransferTaskHandlerConfig(),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	_, err = taskSvc.CreateTask(context.Background(), &models.CreateTaskRequest{
		Name: "continuous sync task", TaskType: commonExecution.TaskTypeSync, Config: validTransferTaskHandlerContinuousConfig(),
	}, 7, 9)
	if err != nil {
		t.Fatalf("CreateTask(continuous) error = %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTransferTestAuthContext(t, c, 7, 9)
		c.Next()
	})
	handler := NewTaskHandler(taskSvc)
	router.GET("/task-provider/tasks", handler.ProviderListTasks)
	router.GET("/task-definitions", handler.ListTasks)

	req := httptest.NewRequest(http.MethodGet, "/task-provider/tasks?task_type=sync", nil)
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

	allReq := httptest.NewRequest(http.MethodGet, "/task-definitions", nil)
	all := httptest.NewRecorder()
	router.ServeHTTP(all, allReq)
	var allResp struct {
		Items []models.TransferTask `json:"items"`
	}
	if all.Code != http.StatusOK || json.Unmarshal(all.Body.Bytes(), &allResp) != nil || len(allResp.Items) != 2 {
		t.Fatalf("Console task list should keep bounded and continuous tasks: status=%d body=%s", all.Code, all.Body.String())
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
	taskSvc := service.NewTaskService(db, nil, nil)
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
		setTransferTestAuthContext(t, c, 7, 9)
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
			apply_identity TEXT NOT NULL UNIQUE,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			task_type TEXT NOT NULL,
			config JSON,
			schedule TEXT,
			batch_size INTEGER,
			enabled BOOLEAN,
			auto_scan_metadata BOOLEAN,
			initial_metadata_scan_status TEXT NOT NULL DEFAULT '',
			initial_metadata_scan_claim_token TEXT NOT NULL DEFAULT '',
			initial_metadata_scan_lease_until DATETIME,
			initial_metadata_scan_attempt INTEGER NOT NULL DEFAULT 0,
			initial_metadata_scan_execution_id TEXT NOT NULL DEFAULT '',
			initial_metadata_scan_error TEXT NOT NULL DEFAULT '',
			status TEXT,
			desired_state TEXT NOT NULL DEFAULT 'stopped',
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
	if err := db.Exec(`
		CREATE TABLE transfer.dead_letters (
			identity TEXT PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			task_id INTEGER NOT NULL,
			apply_identity TEXT NOT NULL,
			first_execution_id TEXT NOT NULL,
			last_execution_id TEXT NOT NULL,
			source_identity TEXT NOT NULL,
			source_topic TEXT NOT NULL,
			source_partition TEXT NOT NULL,
			source_offset INTEGER NOT NULL,
			source_timestamp DATETIME,
			error_code TEXT NOT NULL,
			error_category TEXT NOT NULL,
			error_message TEXT NOT NULL,
			payload_topic TEXT NOT NULL,
			payload_partition INTEGER NOT NULL,
			payload_offset INTEGER NOT NULL,
			payload_available BOOLEAN NOT NULL,
			first_observed_at DATETIME NOT NULL,
			last_observed_at DATETIME NOT NULL,
			occurrence_count INTEGER NOT NULL,
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE(apply_identity, source_identity, source_partition, source_offset)
		)
	`).Error; err != nil {
		t.Fatalf("create dead_letters table: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
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

func validTransferTaskHandlerContinuousConfig() map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
		"load":    map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{"type": "kafka"}},
		"source": map[string]interface{}{
			"locator": "addp://engine/30/path/orders.events?type=topic", "representation": "native",
			"change_stream": map[string]interface{}{
				"envelope": "record", "encoding": "json", "key": map[string]interface{}{"source": "value", "fields": []interface{}{"id"}},
				"start": map[string]interface{}{"mode": "committed", "initial": "earliest"}, "poll_batch_size": 100,
			},
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/8/path/public?type=schema", "name": "orders", "data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{map[string]interface{}{"source": "id", "target": "id", "target_type": "bigint", "nullable": false}},
		}},
	}
}

func validTransferTaskHandlerCDCConfig() map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
		"load": map[string]interface{}{
			"mode": "incremental", "change_detection": map[string]interface{}{"type": "cdc", "bootstrap": "initial_snapshot"},
		},
		"source": map[string]interface{}{
			"locator": "addp://engine/12/path/public/orders?type=table", "data_type": "table", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/20/path/public?type=schema", "name": "orders_cdc", "data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert_delete", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{map[string]interface{}{"source": "id", "target": "id", "target_type": "bigint", "nullable": false}},
		}},
	}
}
