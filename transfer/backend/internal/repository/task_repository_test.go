package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGetStatisticsUsesSyncExecutionsAndStringSourceTaskID(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	repo := NewTaskRepository(db)

	taskA := createTaskRepositoryTestTask(t, db, 7, "task-a")
	taskB := createTaskRepositoryTestTask(t, db, 7, "task-b")
	createTaskRepositoryTestTask(t, db, 7, "task-c")

	createTaskRepositoryTestExecution(t, db, taskA, commonExecution.TaskTypeSync, commonExecution.ExecutionStatusSuccess, time.Now().Add(-time.Hour))
	createTaskRepositoryTestExecution(t, db, taskA, "transfer", commonExecution.ExecutionStatusFailed, time.Now())
	createTaskRepositoryTestExecution(t, db, taskB, commonExecution.TaskTypeSync, commonExecution.ExecutionStatusRunning, time.Now())

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

func TestClaimDueScheduledTaskAdvancesNextRunAt(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	repo := NewTaskRepository(db)

	dueAt := time.Now().Add(-time.Minute)
	nextRunAt := time.Now().Add(time.Hour)
	task := createTaskRepositoryTestTask(t, db, 7, "scheduled-task")
	if err := repo.UpdateFields(task.ID, map[string]interface{}{
		"schedule":    "0 */5 * * * *",
		"enabled":     true,
		"next_run_at": dueAt,
	}); err != nil {
		t.Fatalf("update task: %v", err)
	}

	ids, err := repo.ListDueScheduledTaskIDs(context.Background(), time.Now(), 100)
	if err != nil {
		t.Fatalf("ListDueScheduledTaskIDs() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != task.ID {
		t.Fatalf("due ids = %#v, want [%d]", ids, task.ID)
	}

	execution := claimTestExecution(task, "scheduled-claim")
	claimed, _, err := repo.ClaimDueScheduledExecution(context.Background(), task.ID, "0 */5 * * * *", time.Now(), &nextRunAt, &execution, "")
	if err != nil {
		t.Fatalf("ClaimDueScheduledTask() error = %v", err)
	}
	if claimed == nil || claimed.ID != task.ID {
		t.Fatalf("claimed = %#v, want task %d", claimed, task.ID)
	}

	refreshed, err := repo.GetByID(task.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if refreshed.NextRunAt == nil || !refreshed.NextRunAt.After(dueAt) {
		t.Fatalf("next_run_at = %#v, want after %s", refreshed.NextRunAt, dueAt)
	}
}

func TestClaimExecutionAtomicallyRejectsSecondActiveExecution(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	repo := NewTaskRepository(db)
	task := createTaskRepositoryTestTask(t, db, 7, "atomic-claim")
	first := claimTestExecution(task, "claim-first")
	if _, _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, &first, ""); err != nil {
		t.Fatalf("first ClaimExecution failed: %v", err)
	}
	second := claimTestExecution(task, "claim-second")
	if _, _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, &second, ""); err != ErrTaskAlreadyRunning {
		t.Fatalf("second ClaimExecution error = %v, want ErrTaskAlreadyRunning", err)
	}
	var count int64
	if err := db.Model(&commonExecution.TaskExecution{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("execution count = %d, want 1", count)
	}
}

func TestSyncStateCommitUsesStateVersionAndFencingToken(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	taskRepo := NewTaskRepository(db)
	task := createTaskRepositoryTestTask(t, db, 7, "watermark-state")
	execution := claimTestExecution(task, "watermark-claim")
	_, state, err := taskRepo.ClaimExecution(context.Background(), task.ID, task.TenantID, &execution, "addp://engine/1/path/public/orders?type=table")
	if err != nil {
		t.Fatalf("ClaimExecution failed: %v", err)
	}
	if state == nil || state.FencingToken != 1 {
		t.Fatalf("state = %#v, want fencing token 1", state)
	}
	stateRepo := NewSyncStateRepository(db)
	position := models.JSONMap{"type": "watermark", "version": "v1", "cursor": map[string]interface{}{"values": []string{"2026-07-12T00:00:00Z", "10"}}}
	if err := stateRepo.CommitPosition(context.Background(), state.ID, 0, 1, position, execution.ExecutionID); err != nil {
		t.Fatalf("CommitPosition failed: %v", err)
	}
	committedState, err := stateRepo.GetByID(context.Background(), state.ID)
	if err != nil || committedState.PositionCommittedAt == nil {
		t.Fatalf("committed state position time = %#v, error = %v", committedState, err)
	}
	if err := stateRepo.CommitPosition(context.Background(), state.ID, 0, 1, position, execution.ExecutionID); err != ErrSyncStateFenced {
		t.Fatalf("stale CommitPosition error = %v, want ErrSyncStateFenced", err)
	}
}

func TestFinalizeContinuousStopCancelsExecutionAfterLeaseExpiry(t *testing.T) {
	db := newTaskRepositoryTestDB(t)
	repo := NewTaskRepository(db)
	task := createTaskRepositoryTestTask(t, db, 7, "expired-stop")
	execution := claimTestExecution(task, "expired-stop-execution")
	execution.Status = commonExecution.ExecutionStatusRunning
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&task).Updates(map[string]interface{}{
		"config": models.JSONMap{"runtime": map[string]interface{}{"boundary": "continuous"}},
		"status": models.TaskStatusRunning, "desired_state": models.TaskDesiredStateStopped,
		"last_execution_id": execution.ExecutionID, "last_execution_status": commonExecution.ExecutionStatusRunning,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.RuntimeLease{
		TaskID: task.ID, ExecutionID: execution.ExecutionID, OwnerInstanceID: "expired-worker",
		LeaseUntil: time.Now().Add(-time.Minute), HeartbeatAt: time.Now().Add(-time.Minute), FencingToken: 1, ClaimedAt: time.Now().Add(-2 * time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := repo.FinalizeContinuousStop(context.Background(), task.ID, task.TenantID, "stopped", now); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&task, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != models.TaskStatusIdle || task.LastExecutionStatus == nil || *task.LastExecutionStatus != commonExecution.ExecutionStatusCancelled {
		t.Fatalf("finalized task=%#v", task)
	}
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&execution).Error; err != nil {
		t.Fatal(err)
	}
	if execution.Status != commonExecution.ExecutionStatusCancelled || execution.CompletedAt == nil || execution.Metadata["stop_reason"] != "stopped" {
		t.Fatalf("finalized execution=%#v", execution)
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
		`CREATE TABLE transfer.sync_states (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			source_identity TEXT NOT NULL,
			partition TEXT NOT NULL,
			position JSON,
			position_type TEXT NOT NULL,
			position_version TEXT NOT NULL,
			state_version INTEGER NOT NULL DEFAULT 0,
			fencing_token INTEGER NOT NULL DEFAULT 0,
			updated_execution_id TEXT,
			position_committed_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
				UNIQUE(task_id, source_identity, partition)
			)`,
		`CREATE TABLE transfer.runtime_leases (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id INTEGER NOT NULL UNIQUE,
				execution_id TEXT NOT NULL UNIQUE,
				owner_instance_id TEXT NOT NULL,
				lease_until DATETIME NOT NULL,
				heartbeat_at DATETIME NOT NULL,
				fencing_token INTEGER NOT NULL,
				claimed_at DATETIME NOT NULL,
				created_at DATETIME,
				updated_at DATETIME
			)`,
		`CREATE TABLE transfer.schema_change_requests (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id INTEGER NOT NULL,
				tenant_id INTEGER NOT NULL,
				status TEXT NOT NULL,
				detected_at DATETIME NOT NULL
			)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	return db
}

func claimTestExecution(task models.TransferTask, executionID string) commonExecution.TaskExecution {
	now := time.Now()
	return commonExecution.TaskExecution{
		TenantID: int(task.TenantID), ExecutionID: executionID, Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleTransfer,
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), Status: commonExecution.ExecutionStatusPending,
		TriggerType: commonExecution.TriggerTypeManual, CreatedAt: now, UpdatedAt: now,
	}
}

func createTaskRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID uint, name string) models.TransferTask {
	t.Helper()
	task := models.TransferTask{
		TenantID:  tenantID,
		Name:      name,
		TaskType:  commonExecution.TaskTypeSync,
		Config:    models.JSONMap{"runtime": map[string]interface{}{"boundary": "bounded"}, "load": map[string]interface{}{"mode": "snapshot"}},
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
