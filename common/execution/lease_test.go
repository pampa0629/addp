package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	execution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestClaimNextUsesAttemptScopedLeaseToken(t *testing.T) {
	db := newLeaseTestDB(t)
	now := time.Now().UTC()
	item := execution.TaskExecution{TenantID: 7, ExecutionID: "claim-token", Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, Source: execution.ModuleMeta, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}

	claimed, lease, err := execution.ClaimNext(context.Background(), db, execution.ClaimOptions{Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, WorkerID: "meta-worker-1", Now: now, LeaseDuration: time.Minute})
	if err != nil || claimed == nil || lease == nil {
		t.Fatalf("ClaimNext = %#v %#v, %v", claimed, lease, err)
	}
	if lease.Token == "" || lease.Attempt != 1 || claimed.LeaseToken == nil || *claimed.LeaseToken != lease.Token {
		t.Fatalf("claim lease = %#v, execution = %#v", lease, claimed)
	}

	wrong := *lease
	wrong.Token = "00000000-0000-0000-0000-000000000000"
	if err := execution.RenewLease(context.Background(), db, wrong, now.Add(2*time.Minute)); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("wrong-token renew error = %v, want conflict", err)
	}
	if err := execution.UpdateWithLease(context.Background(), db, *lease, map[string]interface{}{"progress": 50}); err != nil {
		t.Fatalf("UpdateWithLease: %v", err)
	}
	if err := execution.UpdateWithLease(context.Background(), db, *lease, map[string]interface{}{"status": execution.ExecutionStatusSuccess}); err == nil {
		t.Fatal("UpdateWithLease status error = nil, want protected-field rejection")
	}
	terminal, err := execution.AttemptIsTerminal(context.Background(), db, *lease)
	if err != nil || terminal {
		t.Fatalf("AttemptIsTerminal before completion = %v, %v", terminal, err)
	}
	if err := execution.CompleteWithLease(context.Background(), db, *lease, execution.ExecutionStatusSuccess, now.Add(time.Minute), nil); err != nil {
		t.Fatalf("CompleteWithLease: %v", err)
	}
	terminal, err = execution.AttemptIsTerminal(context.Background(), db, *lease)
	if err != nil || !terminal {
		t.Fatalf("AttemptIsTerminal after completion = %v, %v", terminal, err)
	}
}

func TestClaimNextDoesNotClaimContinuousExecution(t *testing.T) {
	db := newLeaseTestDB(t)
	now := time.Now().UTC()
	item := execution.TaskExecution{TenantID: 7, ExecutionID: "continuous", Module: execution.ModuleTransfer, TaskType: execution.TaskTypeSync, Source: execution.ModuleTransfer, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryContinuous, TriggerType: execution.TriggerTypeManual, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
	claimed, lease, err := execution.ClaimNext(context.Background(), db, execution.ClaimOptions{Module: execution.ModuleTransfer, TaskType: execution.TaskTypeSync, WorkerID: "bounded-worker", Now: now, LeaseDuration: time.Minute})
	if err != nil || claimed != nil || lease != nil {
		t.Fatalf("ClaimNext continuous = %#v %#v, %v", claimed, lease, err)
	}
}

func TestExpiredAttemptRejectsLateCompletionAfterRetry(t *testing.T) {
	db := newLeaseTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	item := execution.TaskExecution{TenantID: 7, ExecutionID: "expired", Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, Source: execution.ModuleMeta, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
	_, lease, err := execution.ClaimNext(context.Background(), db, execution.ClaimOptions{Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, WorkerID: "meta-worker-1", Now: now, LeaseDuration: time.Second})
	if err != nil || lease == nil {
		t.Fatalf("claim execution: %#v, %v", lease, err)
	}
	expiredAt := now.Add(2 * time.Second)
	items, err := execution.FindExpiredForUpdate(context.Background(), db, execution.ExpiredOptions{Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, Now: expiredAt})
	if err != nil || len(items) != 1 {
		t.Fatalf("FindExpiredForUpdate = %#v, %v", items, err)
	}
	if err := execution.RetryExpired(context.Background(), db, *lease, expiredAt, "recovering"); err != nil {
		t.Fatalf("RetryExpired: %v", err)
	}
	if err := execution.CompleteWithLease(context.Background(), db, *lease, execution.ExecutionStatusSuccess, expiredAt, nil); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("late completion error = %v, want conflict", err)
	}
}

func TestExpiredAttemptRejectsOwnerWritesBeforeRecovery(t *testing.T) {
	db := newLeaseTestDB(t)
	now := time.Now().UTC()
	item := execution.TaskExecution{TenantID: 7, ExecutionID: "expired-owner", Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, Source: execution.ModuleMeta, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
	_, lease, err := execution.ClaimNext(context.Background(), db, execution.ClaimOptions{Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, WorkerID: "meta-worker-1", Now: now.Add(-time.Minute), LeaseDuration: time.Second})
	if err != nil || lease == nil {
		t.Fatalf("claim execution: %#v, %v", lease, err)
	}
	if err := execution.UpdateWithLease(context.Background(), db, *lease, map[string]interface{}{"progress": 50}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("expired progress error = %v, want conflict", err)
	}
	if err := execution.RenewLease(context.Background(), db, *lease, now.Add(time.Minute)); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("expired renew error = %v, want conflict", err)
	}
	if err := execution.CompleteWithLease(context.Background(), db, *lease, execution.ExecutionStatusSuccess, now, nil); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("expired completion error = %v, want conflict", err)
	}
}

func newLeaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure execution store: %v", err)
	}
	return db
}
