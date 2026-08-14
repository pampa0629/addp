package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resume"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeTaskQueue struct {
	err         error
	enqueued    bool
	taskID      uint
	executionID uint
	tenantID    uint
}

func (q *fakeTaskQueue) EnqueueExecuteTask(ctx context.Context, taskID, executionID, tenantID uint) error {
	q.enqueued = true
	q.taskID = taskID
	q.executionID = executionID
	q.tenantID = tenantID
	return q.err
}

func (q *fakeTaskQueue) Close() error {
	return nil
}

func TestRetryExecutionEnqueuesRestartExecution(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	oldExecution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusFailed)
	queue := &fakeTaskQueue{}
	service := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	service.SetTaskQueue(queue)

	newExecution, err := service.RetryExecution(ctx, uint(oldExecution.ID), uint(task.TenantID), 9)
	if err != nil {
		t.Fatalf("RetryExecution failed: %v", err)
	}
	if newExecution == nil {
		t.Fatal("RetryExecution returned nil execution")
	}
	if !queue.enqueued {
		t.Fatal("retry execution was not enqueued")
	}
	if queue.taskID != task.ID || queue.executionID != newExecution.ID || queue.tenantID != uint(task.TenantID) {
		t.Fatalf("enqueued payload = task %d execution %d tenant %d, want task %d execution %d tenant %d",
			queue.taskID, queue.executionID, queue.tenantID, task.ID, newExecution.ID, task.TenantID)
	}

	var updatedTask models.TransferTask
	if err := db.First(&updatedTask, task.ID).Error; err != nil {
		t.Fatalf("load updated task: %v", err)
	}
	if updatedTask.Status != models.TaskStatusRunning || updatedTask.Progress != 0 {
		t.Fatalf("task state = %s progress %.2f, want running progress 0", updatedTask.Status, updatedTask.Progress)
	}

	var storedExecution commonExecution.TaskExecution
	if err := db.First(&storedExecution, newExecution.ID).Error; err != nil {
		t.Fatalf("load new execution: %v", err)
	}
	if storedExecution.Status != commonExecution.ExecutionStatusPending || storedExecution.TriggerType != commonExecution.TriggerTypeManual {
		t.Fatalf("new execution status=%s trigger=%s, want pending manual", storedExecution.Status, storedExecution.TriggerType)
	}
	if storedExecution.StartedAt != nil {
		t.Fatalf("pending retry started_at = %v, want nil", storedExecution.StartedAt)
	}
	if len(storedExecution.ExecutionConfig) == 0 {
		t.Fatal("pending retry execution_config is empty")
	}
}

func TestUpdateStatusStartsPendingExecution(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	execution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusPending)
	service := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))

	if err := service.UpdateStatus(ctx, uint(execution.ID), models.ExecutionStatusRunning); err != nil {
		t.Fatalf("UpdateStatus(running): %v", err)
	}
	var stored commonExecution.TaskExecution
	if err := db.First(&stored, execution.ID).Error; err != nil {
		t.Fatalf("load started execution: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusRunning || stored.StartedAt == nil {
		t.Fatalf("started execution status=%s started_at=%v", stored.Status, stored.StartedAt)
	}
}

func TestGetExecutionLogsIncludesTerminalFailure(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	execution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusFailed)
	execution.ErrorDetails = commonModels.JSONMap{
		"logs":    "2026-07-31T22:38:05Z batch=0\n",
		"message": "failed to write target geometry",
	}
	if err := db.Save(&execution).Error; err != nil {
		t.Fatalf("save execution error details: %v", err)
	}
	service := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))

	logs, err := service.GetExecutionLogs(ctx, uint(execution.ID), uint(task.TenantID))
	if err != nil {
		t.Fatalf("GetExecutionLogs() error = %v", err)
	}
	want := "2026-07-31T22:38:05Z batch=0\nERROR failed to write target geometry"
	if logs != want {
		t.Fatalf("logs = %q, want %q", logs, want)
	}
}

func TestGetExecutionLogsUsesTerminalFailureWhenProgressLogsAreEmpty(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	execution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusFailed)
	execution.ErrorDetails = commonModels.JSONMap{"message": "failed before first batch"}
	if err := db.Save(&execution).Error; err != nil {
		t.Fatalf("save execution error details: %v", err)
	}
	service := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))

	logs, err := service.GetExecutionLogs(ctx, uint(execution.ID), uint(task.TenantID))
	if err != nil {
		t.Fatalf("GetExecutionLogs() error = %v", err)
	}
	if logs != "ERROR failed before first batch" {
		t.Fatalf("logs = %q, want terminal failure log", logs)
	}
}

func TestRetryExecutionDoesNotCarryCheckpointState(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	oldExecution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusFailed)
	oldExecution.Metadata = commonModels.JSONMap{
		"checkpoint_offset": int64(8),
		"checkpoint_state": map[string]interface{}{
			"resume_marker": map[string]interface{}{"provider": "test.source"},
			"commit_marker": map[string]interface{}{"provider": "test.target"},
		},
	}
	if err := db.Save(&oldExecution).Error; err != nil {
		t.Fatalf("save old execution metadata: %v", err)
	}
	queue := &fakeTaskQueue{}
	service := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	service.SetTaskQueue(queue)

	newExecution, err := service.RetryExecution(ctx, uint(oldExecution.ID), uint(task.TenantID), 9)
	if err != nil {
		t.Fatalf("RetryExecution failed: %v", err)
	}

	var storedExecution commonExecution.TaskExecution
	if err := db.First(&storedExecution, newExecution.ID).Error; err != nil {
		t.Fatalf("load new execution: %v", err)
	}
	if len(storedExecution.Metadata) != 0 {
		t.Fatalf("new retry metadata = %#v, want empty restart metadata", storedExecution.Metadata)
	}
}

func TestRetryExecutionRollsBackWhenEnqueueFails(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	oldExecution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusFailed)
	queue := &fakeTaskQueue{err: fmt.Errorf("queue down")}
	service := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	service.SetTaskQueue(queue)

	_, err := service.RetryExecution(ctx, uint(oldExecution.ID), uint(task.TenantID), 9)
	if err == nil {
		t.Fatal("RetryExecution succeeded, want enqueue error")
	}
	if !queue.enqueued {
		t.Fatal("retry execution did not attempt enqueue")
	}

	var updatedTask models.TransferTask
	if err := db.First(&updatedTask, task.ID).Error; err != nil {
		t.Fatalf("load updated task: %v", err)
	}
	if updatedTask.Status != models.TaskStatusIdle || updatedTask.Progress != 0 {
		t.Fatalf("task state = %s progress %.2f, want idle progress 0", updatedTask.Status, updatedTask.Progress)
	}

	var executions []commonExecution.TaskExecution
	if err := db.Order("id asc").Find(&executions).Error; err != nil {
		t.Fatalf("load executions: %v", err)
	}
	if len(executions) != 2 {
		t.Fatalf("execution count = %d, want 2", len(executions))
	}
	newExecution := executions[1]
	if newExecution.Status != commonExecution.ExecutionStatusFailed {
		t.Fatalf("new execution status = %s, want failed", newExecution.Status)
	}
	if newExecution.ErrorDetails["message"] != "queue down" {
		t.Fatalf("new execution error message = %#v, want queue down", newExecution.ErrorDetails["message"])
	}
}

func TestRetryExecutionRejectsAppendTask(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	task.Config = models.JSONMap{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"target":  map[string]interface{}{"policy": map[string]interface{}{"apply_mode": "append"}},
	}
	if err := db.Save(&task).Error; err != nil {
		t.Fatalf("update task config: %v", err)
	}
	oldExecution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusFailed)
	queue := &fakeTaskQueue{}
	service := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	service.SetTaskQueue(queue)

	_, err := service.RetryExecution(ctx, uint(oldExecution.ID), uint(task.TenantID), 9)
	if err == nil {
		t.Fatal("RetryExecution succeeded, want append rejection")
	}
	if queue.enqueued {
		t.Fatal("append retry was enqueued")
	}

	var executions []commonExecution.TaskExecution
	if err := db.Find(&executions).Error; err != nil {
		t.Fatalf("load executions: %v", err)
	}
	if len(executions) != 1 {
		t.Fatalf("execution count = %d, want no new retry execution", len(executions))
	}
}

func TestRetryExecutionRejectsSchemaBlockedCDC(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	task.Config = validPostgreSQLCDCTaskConfig()
	task.Status = models.TaskStatusBlocked
	task.DesiredState = models.TaskDesiredStateRunning
	if err := db.Save(&task).Error; err != nil {
		t.Fatal(err)
	}
	oldExecution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusFailed)
	queue := &fakeTaskQueue{}
	service := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	service.SetTaskQueue(queue)
	if _, err := service.RetryExecution(ctx, uint(oldExecution.ID), uint(task.TenantID), 9); !errors.Is(err, ErrCDCSchemaChangeBlocked) {
		t.Fatalf("RetryExecution() error = %v, want ErrCDCSchemaChangeBlocked", err)
	}
	if queue.enqueued {
		t.Fatal("schema-blocked CDC retry was enqueued")
	}
}

func TestUpdateExecutionMergesMetadataAndDTOExposesIt(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	execution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusRunning)
	execution.Metadata = commonModels.JSONMap{"existing": "kept"}
	if err := db.Save(&execution).Error; err != nil {
		t.Fatalf("save execution metadata: %v", err)
	}
	service := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))

	err := service.UpdateExecution(ctx, uint(execution.ID), map[string]interface{}{
		"metadata": map[string]interface{}{
			"target_refs": []map[string]interface{}{
				{
					"path":      "tenant_7/export/20260621/session/roads.shp",
					"role":      "main",
					"required":  true,
					"primary":   true,
					"extension": ".shp",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateExecution() error = %v", err)
	}

	var stored commonExecution.TaskExecution
	if err := db.First(&stored, execution.ID).Error; err != nil {
		t.Fatalf("load execution: %v", err)
	}
	if stored.Metadata["existing"] != "kept" {
		t.Fatalf("stored metadata = %#v, want existing key preserved", stored.Metadata)
	}
	refs, ok := stored.Metadata["target_refs"].([]interface{})
	if !ok || len(refs) != 1 {
		t.Fatalf("stored target_refs = %#v, want one ref", stored.Metadata["target_refs"])
	}

	dto, err := service.GetExecutionByExecutionID(ctx, execution.ExecutionID, uint(task.TenantID))
	if err != nil {
		t.Fatalf("GetExecutionByExecutionID() error = %v", err)
	}
	if dto.Metadata["existing"] != "kept" {
		t.Fatalf("dto metadata = %#v, want existing key", dto.Metadata)
	}
	dtoRefs, ok := dto.Metadata["target_refs"].([]interface{})
	if !ok || len(dtoRefs) != 1 {
		t.Fatalf("dto target_refs = %#v, want one ref", dto.Metadata["target_refs"])
	}
}

func TestFinishErrorDetailsPreservesLogsOnSuccess(t *testing.T) {
	details, changed := finishErrorDetails(commonModels.JSONMap{
		"logs":    "batch=1\n",
		"message": "old error",
	}, models.ExecutionStatusSuccess, "")

	if !changed {
		t.Fatal("finishErrorDetails changed = false, want true")
	}
	if got := details["logs"]; got != "batch=1\n" {
		t.Fatalf("logs = %#v, want preserved logs", got)
	}
	if _, ok := details["message"]; ok {
		t.Fatalf("message still exists in details: %#v", details)
	}
}

func TestFinishErrorDetailsPreservesLogsOnFailure(t *testing.T) {
	details, changed := finishErrorDetails(commonModels.JSONMap{
		"logs": "batch=1\n",
	}, models.ExecutionStatusFailed, "failed to write target")

	if !changed {
		t.Fatal("finishErrorDetails changed = false, want true")
	}
	if got := details["logs"]; got != "batch=1\n" {
		t.Fatalf("logs = %#v, want preserved logs", got)
	}
	if got := details["message"]; got != "failed to write target" {
		t.Fatalf("message = %#v, want failure message", got)
	}
}

func TestTableProgressCallbackStoresResumeAndCommitMarkers(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	execution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusRunning)
	executionService := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	engineService := &ExecutionEngineService{
		taskRepo:         repositoryForExecutionServiceTest(db),
		executionService: executionService,
	}

	callback := engineService.tableProgressCallback(&task, uint(execution.ID))
	err := callback(ctx, executor.TableProgressEvent{
		BatchIndex:     1,
		SourceOffset:   0,
		BatchRows:      2,
		RecordsRead:    2,
		RecordsWritten: 2,
		ResumeMarker: &resume.Marker{
			Version:      resume.MarkerVersionV1,
			Provider:     "test.source",
			PositionUnit: "row",
			ReadPosition: map[string]interface{}{"row_offset": int64(2)},
		},
		CommitMarker: &resume.Marker{
			Version:        resume.MarkerVersionV1,
			Provider:       "test.target",
			PositionUnit:   "session_commit",
			CommitPosition: map[string]interface{}{"rows_committed": int64(2)},
		},
		Final: true,
	})
	if err != nil {
		t.Fatalf("progress callback failed: %v", err)
	}

	var stored commonExecution.TaskExecution
	if err := db.First(&stored, execution.ID).Error; err != nil {
		t.Fatalf("load execution: %v", err)
	}
	state, ok := stored.Metadata["checkpoint_state"].(map[string]interface{})
	if !ok {
		t.Fatalf("checkpoint_state = %#v, want map", stored.Metadata["checkpoint_state"])
	}
	if state["final"] != true {
		t.Fatalf("checkpoint final = %#v, want true", state["final"])
	}
	resumeMarker, ok := state["resume_marker"].(map[string]interface{})
	if !ok || resumeMarker["provider"] != "test.source" {
		t.Fatalf("resume marker = %#v, want test.source marker", state["resume_marker"])
	}
	commitMarker, ok := state["commit_marker"].(map[string]interface{})
	if !ok || commitMarker["provider"] != "test.target" {
		t.Fatalf("commit marker = %#v, want test.target marker", state["commit_marker"])
	}
}

func TestRawCopyProgressCallbackStoresByteMetricsAndContentCheckpoint(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	execution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusRunning)
	executionService := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	engineService := &ExecutionEngineService{
		taskRepo:         repositoryForExecutionServiceTest(db),
		executionService: executionService,
	}

	callback := engineService.rawCopyProgressCallback(&task, uint(execution.ID))
	err := callback(ctx, executor.RawCopyProgressEvent{
		RecordsRead:    1,
		RecordsWritten: 1,
		BytesRead:      9,
		BytesWritten:   9,
		Final:          true,
	})
	if err != nil {
		t.Fatalf("raw copy progress callback failed: %v", err)
	}

	var stored commonExecution.TaskExecution
	if err := db.First(&stored, execution.ID).Error; err != nil {
		t.Fatalf("load execution: %v", err)
	}
	if stored.RecordsRead == nil || *stored.RecordsRead != 1 {
		t.Fatalf("records_read = %#v, want 1", stored.RecordsRead)
	}
	if stored.RecordsWritten == nil || *stored.RecordsWritten != 1 {
		t.Fatalf("records_written = %#v, want 1", stored.RecordsWritten)
	}
	if stored.BytesRead == nil || *stored.BytesRead != 9 {
		t.Fatalf("bytes_read = %#v, want 9", stored.BytesRead)
	}
	if stored.BytesWritten == nil || *stored.BytesWritten != 9 {
		t.Fatalf("bytes_written = %#v, want 9", stored.BytesWritten)
	}
	if got := stored.Metadata["checkpoint_offset"]; got != float64(1) {
		t.Fatalf("checkpoint_offset = %#v, want 1", got)
	}
	state, ok := stored.Metadata["checkpoint_state"].(map[string]interface{})
	if !ok {
		t.Fatalf("checkpoint_state = %#v, want map", stored.Metadata["checkpoint_state"])
	}
	if state["final"] != true || state["target_committed"] != true {
		t.Fatalf("checkpoint state = %#v, want final target_committed", state)
	}
	if state["bytes_read"] != float64(9) || state["bytes_written"] != float64(9) {
		t.Fatalf("checkpoint bytes = %#v, want 9/9", state)
	}

	var storedTask models.TransferTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load task: %v", err)
	}
	if storedTask.Progress != 100 {
		t.Fatalf("task progress = %v, want 100", storedTask.Progress)
	}
}

func repositoryForExecutionServiceTest(db *gorm.DB) *repository.TaskRepository {
	return repository.NewTaskRepository(db)
}

func newExecutionServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS transfer").Error; err != nil {
		t.Fatalf("attach transfer schema: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	createExecutionServiceTestTables(t, db)
	return db
}

func createExecutionServiceTestTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE transfer.transfer_tasks (
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
		)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
}

func createExecutionServiceTestTask(t *testing.T, db *gorm.DB) models.TransferTask {
	t.Helper()
	task := models.TransferTask{
		TenantID:  7,
		Name:      "retry source",
		TaskType:  commonExecution.TaskTypeSync,
		Config:    models.JSONMap{"runtime": map[string]interface{}{"boundary": "bounded"}, "load": map[string]interface{}{"mode": "snapshot"}, "target": map[string]interface{}{"policy": map[string]interface{}{"apply_mode": "replace"}}},
		BatchSize: 100,
		Status:    models.TaskStatusIdle,
		Progress:  88,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func createExecutionServiceTestExecution(t *testing.T, db *gorm.DB, task models.TransferTask, status string) commonExecution.TaskExecution {
	t.Helper()
	taskName := task.Name
	execution := commonExecution.TaskExecution{
		TenantID:       int(task.TenantID),
		ExecutionID:    fmt.Sprintf("execution-%s", status),
		Module:         commonExecution.ModuleTransfer,
		TaskType:       commonExecution.TaskTypeSync,
		Source:         commonExecution.ModuleTransfer,
		SourceTaskID:   commonExecution.NewSourceTaskIDFromUint(task.ID),
		SourceTaskName: &taskName,
		Status:         status,
		Progress:       100,
		TriggerType:    "manual",
	}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
	return execution
}
