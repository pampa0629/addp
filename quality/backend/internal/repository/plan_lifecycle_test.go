package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/quality/internal/testsupport"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPlanExecutionLifecycleIsAtomic(t *testing.T) {
	db := newPlanRepositoryTestDB(t)
	repo := NewPlanRepository(db)
	task := createPlanRepositoryTestTask(t, db, 7)
	createdAt := time.Now().UTC()
	exec := newQualityRepositoryTestExecution("quality-atomic-1", 7, createdAt)

	claimed, err := repo.CreateExecution(context.Background(), task.ID, task.TenantID, exec, models.PlanRunRequest{})
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
	if _, err := repo.CreateExecution(context.Background(), task.ID, task.TenantID, duplicate, models.PlanRunRequest{}); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("duplicate ClaimExecution error = %v, want conflict", err)
	}

	startedAt := createdAt.Add(time.Minute)
	if err := repo.AttachPendingAuthorization(context.Background(), task.TenantID, exec.ExecutionID, map[string]interface{}{"execution_authorization_id": int64(1)}); err != nil {
		t.Fatalf("AttachExecutionAuthorization: %v", err)
	}
	runningExecution, _, err := repo.ClaimPendingExecution(context.Background(), "worker-lifecycle", startedAt, 10*time.Minute)
	if err != nil || runningExecution == nil {
		t.Fatalf("ClaimPendingExecution: %v", err)
	}
	var runningTask models.QualityPlan
	if err := db.First(&runningTask, task.ID).Error; err != nil {
		t.Fatalf("load running task: %v", err)
	}
	if runningTask.LastExecutionStatus != commonExecution.ExecutionStatusRunning || runningTask.LastRunAt == nil || !runningTask.LastRunAt.Equal(startedAt) {
		t.Fatalf("running task summary status=%s last_run_at=%v", runningTask.LastExecutionStatus, runningTask.LastRunAt)
	}

	completedAt := startedAt.Add(2 * time.Minute)
	lease, err := commonExecution.LeaseFromExecution(*runningExecution)
	if err != nil {
		t.Fatalf("LeaseFromExecution: %v", err)
	}
	if err := repo.CompleteExecutionWithLease(context.Background(), task.ID, task.TenantID, lease, commonExecution.ExecutionStatusSuccess, map[string]interface{}{

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

func TestPlanStartRollsBackWhenOwnerSummaryCannotAdvance(t *testing.T) {
	db := newPlanRepositoryTestDB(t)
	repo := NewPlanRepository(db)
	task := createPlanRepositoryTestTask(t, db, 8)
	createdAt := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	exec := newQualityRepositoryTestExecution("quality-start-rollback", 8, createdAt)
	if _, err := repo.CreateExecution(context.Background(), task.ID, task.TenantID, exec, models.PlanRunRequest{}); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if err := db.Delete(&models.QualityPlan{}, task.ID).Error; err != nil {
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
	db := newPlanRepositoryTestDB(t)
	repo := NewPlanRepository(db)
	task := createPlanRepositoryTestTask(t, db, 9)
	createdAt := time.Now().UTC()
	exec := newQualityRepositoryTestExecution("quality-worker-claim", 9, createdAt)
	if _, err := repo.CreateExecution(context.Background(), task.ID, task.TenantID, exec, models.PlanRunRequest{}); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}

	claimed, claimedTask, err := repo.ClaimPendingExecution(context.Background(), "worker-a", createdAt.Add(time.Minute), 10*time.Minute)
	if err != nil {
		t.Fatalf("ClaimPendingExecution without authorization: %v", err)
	}
	if claimed != nil || claimedTask != nil {
		t.Fatalf("unauthorized execution was claimed: execution=%#v task=%#v", claimed, claimedTask)
	}

	if err := repo.AttachPendingAuthorization(context.Background(), task.TenantID, exec.ExecutionID, map[string]interface{}{
		"execution_authorization_id": int64(41),
	}); err != nil {
		t.Fatalf("AttachExecutionAuthorization: %v", err)
	}
	if err := repo.AttachPendingAuthorization(context.Background(), task.TenantID, exec.ExecutionID, map[string]interface{}{
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
	lease, err := commonExecution.LeaseFromExecution(*claimed)
	if err != nil {
		t.Fatalf("LeaseFromExecution: %v", err)
	}
	wrongLease := lease
	wrongLease.Token = "00000000-0000-0000-0000-000000000000"
	renewedUntil := startedAt.Add(20 * time.Minute)
	if err := repo.RenewLease(context.Background(), wrongLease, renewedUntil); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("wrong-owner RenewLease error = %v, want conflict", err)
	}
	if err := repo.RenewLease(context.Background(), lease, renewedUntil); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}

	completedAt := startedAt.Add(time.Minute)
	fields := map[string]interface{}{
		"progress":          100,
		"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(),
	}
	if err := repo.CompleteExecutionWithLease(context.Background(), task.ID, task.TenantID, wrongLease, commonExecution.ExecutionStatusSuccess, fields, completedAt); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("wrong-owner completion error = %v, want conflict", err)
	}
	if err := repo.CompleteExecutionWithLease(context.Background(), task.ID, task.TenantID, lease, commonExecution.ExecutionStatusSuccess, fields, completedAt); err != nil {
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
	db := newPlanRepositoryTestDB(t)
	repo := NewPlanRepository(db)
	task := createPlanRepositoryTestTask(t, db, 10)
	createdAt := time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC)
	exec := newQualityRepositoryTestExecution("quality-worker-recovery", 10, createdAt)
	exec.MaxAttempts = 2
	if _, err := repo.CreateExecution(context.Background(), task.ID, task.TenantID, exec, models.PlanRunRequest{}); err != nil {
		t.Fatalf("ClaimExecution: %v", err)
	}
	if err := repo.AttachPendingAuthorization(context.Background(), task.TenantID, exec.ExecutionID, map[string]interface{}{
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
	restartedRepo := NewPlanRepository(db)
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
	var storedTask models.QualityPlan
	if err := db.First(&storedTask, task.ID).Error; err != nil {
		t.Fatalf("load failed task: %v", err)
	}
	if storedTask.LastExecutionStatus != commonExecution.ExecutionStatusFailed {
		t.Fatalf("task status = %s, want failed", storedTask.LastExecutionStatus)
	}
}

func newPlanRepositoryTestDB(t *testing.T) *gorm.DB {
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
	if err := db.Exec(`CREATE TABLE quality.plans (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		owner_domain_id INTEGER,
		code TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT NOT NULL,
		version INTEGER NOT NULL,
		table_bindings JSON NOT NULL,
		created_by INTEGER NOT NULL,
		updated_by INTEGER NOT NULL,
		created_at DATETIME,
		updated_at DATETIME,
		last_run_at DATETIME,
		last_execution_id TEXT,
		last_execution_status TEXT
	)`).Error; err != nil {
		t.Fatalf("create quality quality plan task test table: %v", err)
	}
	testsupport.EnsureRuleTables(t, db)
	return db
}

func createPlanRepositoryTestTask(t *testing.T, db *gorm.DB, tenantID int64) models.QualityPlan {
	t.Helper()
	task := models.QualityPlan{TenantID: tenantID, Code: "quality_check", Name: "quality-check", Version: 1, CreatedBy: 1, UpdatedBy: 1,
		TableBindings: []byte(`[{"alias":"orders","locator":"addp://engine/12/path/public/orders?type=table"}]`),
		Rules:         []byte(`{"schema_version":"addp.quality.plan-rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","severity":"error","params":{"table":"orders","column":"id"}}]}`)}
	testsupport.SeedPlanRules(t, db, &task)
	if err := NewPlanRepository(db).Create(context.Background(), &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func newQualityRepositoryTestExecution(executionID string, tenantID int, createdAt time.Time) *commonExecution.TaskExecution {
	return &commonExecution.TaskExecution{
		ExecutionID: executionID, TenantID: tenantID, Module: commonExecution.ModuleQuality,
		TaskType: commonExecution.TaskTypeQualityPlan, Source: commonExecution.ModuleQuality,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		Status:            commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		CreatedAt: createdAt, UpdatedAt: createdAt,
		ExecutionConfig: commonModels.JSONMap{
			"schema_version":   "addp.quality.plan-execution-config/v2",
			"check_timeout_ms": int64((30 * time.Minute).Milliseconds()),
		},
	}
}
