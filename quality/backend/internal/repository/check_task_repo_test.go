package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
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
	if err := repo.AttachExecutionAuthorization(context.Background(), task.TenantID, exec.ExecutionID, map[string]interface{}{"execution_authorization_id": int64(1)}); err != nil {
		t.Fatalf("AttachExecutionAuthorization: %v", err)
	}
	runningExecution, _, err := repo.ClaimPendingExecution(context.Background(), "worker-lifecycle", startedAt, 10*time.Minute)
	if err != nil || runningExecution == nil {
		t.Fatalf("ClaimPendingExecution: %v", err)
	}
	var runningTask models.CheckTask
	if err := db.First(&runningTask, task.ID).Error; err != nil {
		t.Fatalf("load running task: %v", err)
	}
	if runningTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning || runningTask.LastRunAt == nil || !runningTask.LastRunAt.Equal(startedAt) {
		t.Fatalf("running task summary status=%s last_run_at=%v", runningTask.LastExecutionStatus, runningTask.LastRunAt)
	}

	completedAt := startedAt.Add(2 * time.Minute)
	if err := repo.CompleteExecutionWithLease(context.Background(), task.ID, task.TenantID, exec.ExecutionID, "worker-lifecycle", map[string]interface{}{
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

func TestClaimExecutionSnapshotsOnlyEnabledRuleApplications(t *testing.T) {
	db := newCheckTaskRepositoryTestDB(t)
	repo := NewCheckTaskRepository(db)
	task := createCheckTaskRepositoryTestTask(t, db, 17)
	createCheckTaskRepositoryTestRuleApplication(t, db, models.RuleApplication{
		TenantID: 17, ElementID: 101, EngineID: task.EngineID, SchemaName: task.SchemaName, Table: task.Table,
		ColumnName: "enabled_column", Enabled: true, CreatedBy: 1,
	})
	createCheckTaskRepositoryTestRuleApplication(t, db, models.RuleApplication{
		TenantID: 17, ElementID: 102, EngineID: task.EngineID, SchemaName: task.SchemaName, Table: task.Table,
		ColumnName: "disabled_column", Enabled: false, CreatedBy: 1,
	})

	execution := newQualityRepositoryTestExecution("quality-enabled-snapshot", 17, time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC))
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, execution); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}

	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load execution: %v", err)
	}
	raw, ok := stored.ExecutionConfig["rule_applications"]
	if !ok {
		t.Fatal("execution config missing rule_applications")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal rule snapshot: %v", err)
	}
	var snapshots []struct {
		ID         int64  `json:"id"`
		ColumnName string `json:"column_name"`
	}
	if err := json.Unmarshal(encoded, &snapshots); err != nil {
		t.Fatalf("decode rule snapshot: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].ColumnName != "enabled_column" {
		t.Fatalf("rule application snapshot = %#v, want only enabled application", snapshots)
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

	_, _, err := repo.ClaimPendingExecution(context.Background(), "worker-rollback", createdAt.Add(time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("ClaimPendingExecution error = %v", err)
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load execution after rollback: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusPending || stored.StartedAt != nil {
		t.Fatalf("execution changed despite owner rollback: status=%s started_at=%v", stored.Status, stored.StartedAt)
	}
}

func TestClaimPendingExecutionRequiresAuthorizationAndLeaseOwner(t *testing.T) {
	db := newCheckTaskRepositoryTestDB(t)
	repo := NewCheckTaskRepository(db)
	task := createCheckTaskRepositoryTestTask(t, db, 9)
	createdAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	exec := newQualityRepositoryTestExecution("quality-worker-claim", 9, createdAt)
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}

	claimed, claimedTask, err := repo.ClaimPendingExecution(context.Background(), "worker-a", createdAt.Add(time.Minute), 10*time.Minute)
	if err != nil {
		t.Fatalf("ClaimPendingExecution without authorization: %v", err)
	}
	if claimed != nil || claimedTask != nil {
		t.Fatalf("unauthorized execution was claimed: execution=%#v task=%#v", claimed, claimedTask)
	}

	if err := repo.AttachExecutionAuthorization(context.Background(), task.TenantID, exec.ExecutionID, map[string]interface{}{
		"execution_authorization_id": int64(41),
	}); err != nil {
		t.Fatalf("AttachExecutionAuthorization: %v", err)
	}
	if err := repo.AttachExecutionAuthorization(context.Background(), task.TenantID, exec.ExecutionID, map[string]interface{}{
		"execution_authorization_id": int64(99),
	}); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("second AttachExecutionAuthorization error = %v, want conflict", err)
	}
	startedAt := createdAt.Add(2 * time.Minute)
	claimed, claimedTask, err = repo.ClaimPendingExecution(context.Background(), "worker-a", startedAt, 10*time.Minute)
	if err != nil {
		t.Fatalf("ClaimPendingExecution: %v", err)
	}
	if claimed == nil || claimedTask == nil {
		t.Fatal("authorized execution was not claimed")
	}
	if claimed.Status != commonExecution.ExecutionStatusRunning || claimed.Attempt != 1 || claimed.StartedAt == nil || !claimed.StartedAt.Equal(startedAt) {
		t.Fatalf("claimed execution = %#v", claimed)
	}
	if claimed.LeaseOwner == nil || *claimed.LeaseOwner != "worker-a" || claimed.LeaseExpiresAt == nil || !claimed.LeaseExpiresAt.Equal(startedAt.Add(10*time.Minute)) {
		t.Fatalf("claimed lease = owner %v expires %v", claimed.LeaseOwner, claimed.LeaseExpiresAt)
	}
	renewedUntil := startedAt.Add(20 * time.Minute)
	if err := repo.RenewLease(context.Background(), exec.ExecutionID, task.TenantID, "worker-b", renewedUntil); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("wrong-owner RenewLease error = %v, want conflict", err)
	}
	if err := repo.RenewLease(context.Background(), exec.ExecutionID, task.TenantID, "worker-a", renewedUntil); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}

	completedAt := startedAt.Add(time.Minute)
	fields := map[string]interface{}{
		"status": commonExecution.ExecutionStatusSuccess, "progress": 100,
		"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(),
	}
	if err := repo.CompleteExecutionWithLease(context.Background(), task.ID, task.TenantID, exec.ExecutionID, "worker-b", fields, completedAt); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("wrong-owner completion error = %v, want conflict", err)
	}
	if err := repo.CompleteExecutionWithLease(context.Background(), task.ID, task.TenantID, exec.ExecutionID, "worker-a", fields, completedAt); err != nil {
		t.Fatalf("CompleteExecutionWithLease: %v", err)
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load completed execution: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusSuccess || stored.ExecutionTimeMs == nil || *stored.ExecutionTimeMs != 60000 || stored.LeaseOwner != nil || stored.LeaseExpiresAt != nil {
		t.Fatalf("completed execution = %#v", stored)
	}
}

func TestRecoverExpiredExecutionRetriesThenFailsAtAttemptLimit(t *testing.T) {
	db := newCheckTaskRepositoryTestDB(t)
	repo := NewCheckTaskRepository(db)
	task := createCheckTaskRepositoryTestTask(t, db, 10)
	createdAt := time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC)
	exec := newQualityRepositoryTestExecution("quality-worker-recovery", 10, createdAt)
	exec.MaxAttempts = 2
	if _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, exec); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if err := repo.AttachExecutionAuthorization(context.Background(), task.TenantID, exec.ExecutionID, map[string]interface{}{
		"execution_authorization_id": int64(42),
	}); err != nil {
		t.Fatalf("AttachExecutionAuthorization: %v", err)
	}

	firstStart := createdAt.Add(time.Minute)
	if claimed, _, err := repo.ClaimPendingExecution(context.Background(), "worker-a", firstStart, time.Minute); err != nil || claimed == nil {
		t.Fatalf("first ClaimPendingExecution = %#v, %v", claimed, err)
	}
	// A restarted process owns a new repository instance and must recover the
	// expired database lease without any in-memory state from worker-a.
	restartedRepo := NewCheckTaskRepository(db)
	if err := restartedRepo.RecoverExpiredExecutions(context.Background(), firstStart.Add(2*time.Minute)); err != nil {
		t.Fatalf("first RecoverExpiredExecutions: %v", err)
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load retried execution: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusPending || stored.Attempt != 1 || stored.LeaseOwner != nil || stored.LeaseExpiresAt != nil {
		t.Fatalf("retried execution = %#v", stored)
	}

	secondStart := firstStart.Add(3 * time.Minute)
	if claimed, _, err := restartedRepo.ClaimPendingExecution(context.Background(), "worker-b", secondStart, time.Minute); err != nil || claimed == nil || claimed.Attempt != 2 {
		t.Fatalf("second ClaimPendingExecution = %#v, %v", claimed, err)
	}
	failedAt := secondStart.Add(2 * time.Minute)
	if err := restartedRepo.RecoverExpiredExecutions(context.Background(), failedAt); err != nil {
		t.Fatalf("second RecoverExpiredExecutions: %v", err)
	}
	if err := db.Where("execution_id = ?", exec.ExecutionID).First(&stored).Error; err != nil {
		t.Fatalf("load failed execution: %v", err)
	}
	if stored.Status != commonExecution.ExecutionStatusFailed || stored.CompletedAt == nil || stored.ExecutionTimeMs == nil || stored.Attempt != 2 {
		t.Fatalf("failed execution = %#v", stored)
	}
	var storedTask models.CheckTask
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load failed task: %v", err)
	}
	if storedTask.LastExecutionStatus != commonExecution.ExecutionStatusFailed {
		t.Fatalf("task status = %s, want failed", storedTask.LastExecutionStatus)
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
	if err := db.Exec("ATTACH DATABASE ':memory:' AS quality").Error; err != nil {
		t.Fatalf("attach quality schema: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	if err := db.Exec(`CREATE TABLE quality.check_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		engine_id INTEGER NOT NULL,
		schema_name TEXT,
		table_name TEXT,
		created_by INTEGER NOT NULL,
		updated_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		last_run_at DATETIME,
		last_execution_id TEXT,
		last_execution_status TEXT
		)`).Error; err != nil {
		t.Fatalf("create quality check task test table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE quality.rule_applications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		element_id INTEGER NOT NULL,
		engine_id INTEGER NOT NULL,
		schema_name TEXT,
		table_name TEXT NOT NULL,
		column_name TEXT NOT NULL,
		rule_config JSON NOT NULL,
		enabled BOOLEAN,
		created_by INTEGER NOT NULL,
		updated_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create quality rule application test table: %v", err)
	}
	return db
}

func createCheckTaskRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID int64) models.CheckTask {
	t.Helper()
	task := models.CheckTask{
		TenantID: tenantID, Name: "quality-check", EngineID: 12, SchemaName: "public", Table: "orders",
		CreatedBy: 1,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create check task: %v", err)
	}
	return task
}

func createCheckTaskRepositoryTestRuleApplication(t *testing.T, db *gorm.DB, application models.RuleApplication) models.RuleApplication {
	t.Helper()
	enabled := application.Enabled
	if len(application.RuleConfig) == 0 {
		application.RuleConfig = []byte(`{"schema_version":"addp.quality.rules/v1","rules":[{"type":"not_null","enabled":true,"severity":"error","message":"","params":{}}]}`)
	}
	if err := db.Create(&application).Error; err != nil {
		t.Fatalf("create rule application: %v", err)
	}
	if !enabled {
		if err := db.Exec("UPDATE quality.rule_applications SET enabled = ? WHERE id = ?", false, application.ID).Error; err != nil {
			t.Fatalf("persist disabled rule application: %v", err)
		}
	}
	return application
}

func newQualityRepositoryTestExecution(executionID string, tenantID int, createdAt time.Time) *commonExecution.TaskExecution {
	return &commonExecution.TaskExecution{
		ExecutionID: executionID, TenantID: tenantID, Module: commonExecution.ModuleQuality,
		TaskType: commonExecution.TaskTypeQualityCheck, Source: commonExecution.ModuleQuality,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}
