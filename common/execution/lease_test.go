package execution_test

import (
	"context"
	"errors"
	"fmt"
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

	claimed, lease, err := execution.ClaimNext(context.Background(), db, execution.ClaimOptions{Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, WorkerID: "meta-worker-1", LeaseDuration: time.Minute})
	if err != nil || claimed == nil || lease == nil {
		t.Fatalf("ClaimNext = %#v %#v, %v", claimed, lease, err)
	}
	if lease.Token == "" || lease.Attempt != 1 || claimed.LeaseToken == nil || *claimed.LeaseToken != lease.Token {
		t.Fatalf("claim lease = %#v, execution = %#v", lease, claimed)
	}

	wrong := *lease
	wrong.Token = "00000000-0000-0000-0000-000000000000"
	if err := execution.RenewLease(context.Background(), db, wrong, 2*time.Minute); !errors.Is(err, commonapi.ErrConflict) {
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
	if err := execution.CompleteWithLease(context.Background(), db, *lease, execution.ExecutionStatusSuccess, nil); err != nil {
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
	claimed, lease, err := execution.ClaimNext(context.Background(), db, execution.ClaimOptions{Module: execution.ModuleTransfer, TaskType: execution.TaskTypeSync, WorkerID: "bounded-worker", LeaseDuration: time.Minute})
	if err != nil || claimed != nil || lease != nil {
		t.Fatalf("ClaimNext continuous = %#v %#v, %v", claimed, lease, err)
	}
}

func TestClaimNextFiltersTriggerSource(t *testing.T) {
	db := newLeaseTestDB(t)
	now := time.Now().UTC()
	for index, source := range []string{execution.ModuleDevelop, execution.ModuleOrchestrator} {
		item := execution.TaskExecution{
			TenantID: 1, ExecutionID: fmt.Sprintf("source-filter-%d", index), Module: execution.ModuleDevelop,
			TaskType: execution.TaskTypeQuery, Source: source, ExecutionBoundary: execution.ExecutionBoundaryBounded,
			Status: execution.ExecutionStatusPending, TriggerType: execution.TriggerTypeManual,
			CreatedAt: now.Add(time.Duration(index) * time.Second), UpdatedAt: now,
		}
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("create execution: %v", err)
		}
	}
	claimed, _, err := execution.ClaimNext(context.Background(), db, execution.ClaimOptions{
		Module: execution.ModuleDevelop, TaskType: execution.TaskTypeQuery, Source: execution.ModuleOrchestrator,
		WorkerID: "develop-query-supervisor", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil || claimed.Source != execution.ModuleOrchestrator {
		t.Fatalf("claimed = %#v, want orchestrator source", claimed)
	}
}

func TestClaimNextSelectsOldestExecutionAcrossTaskTypes(t *testing.T) {
	db := newLeaseTestDB(t)
	now := time.Now().UTC()
	items := []execution.TaskExecution{
		{TenantID: 7, ExecutionID: "not-managed", Module: execution.ModuleManager, TaskType: execution.TaskTypeDataProfiling, Source: execution.ModuleManager, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, CreatedAt: now.Add(-time.Minute), UpdatedAt: now},
		{TenantID: 7, ExecutionID: "older-managed", Module: execution.ModuleManager, TaskType: execution.TaskTypePPTXPDFGeneration, Source: execution.ModuleManager, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, CreatedAt: now.Add(-time.Second), UpdatedAt: now},
		{TenantID: 7, ExecutionID: "newer-managed", Module: execution.ModuleManager, TaskType: execution.TaskTypePointCloudCOPCGeneration, Source: execution.ModuleManager, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatalf("create executions: %v", err)
	}

	claimed, _, err := execution.ClaimNext(context.Background(), db, execution.ClaimOptions{
		Module: execution.ModuleManager,
		TaskTypes: []string{
			execution.TaskTypePointCloudCOPCGeneration,
			execution.TaskTypePPTXPDFGeneration,
		},
		WorkerID: "manager-coordinator", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil || claimed.ExecutionID != "older-managed" {
		t.Fatalf("claimed = %#v, want oldest execution in allowed task types", claimed)
	}
}

func TestExpiredAttemptResetsStartTimeAndFencesOldLease(t *testing.T) {
	db := newLeaseTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	item := execution.TaskExecution{TenantID: 7, ExecutionID: "expired", Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, Source: execution.ModuleMeta, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
	ctx := context.Background()
	options := execution.ClaimOptions{Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, WorkerID: "meta-worker-1", LeaseDuration: time.Second}
	first, lease, err := execution.ClaimNext(ctx, db, options)
	if err != nil || first == nil || lease == nil || first.StartedAt == nil || first.StartedAt.Before(now) || first.Attempt != 1 {
		t.Fatalf("first claim = %#v %#v, %v", first, lease, err)
	}
	if err := execution.RetryExpired(ctx, db, *lease, "recovering"); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("unexpired retry=%v", err)
	}
	if err := db.Model(&item).Update("lease_expires_at", now.Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	items, err := execution.FindExpiredForUpdate(ctx, db, execution.ExpiredOptions{Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan})
	if err != nil || len(items) != 1 {
		t.Fatalf("expired=%v err=%v", items, err)
	}
	wrong := *lease
	wrong.Token = "00000000-0000-0000-0000-000000000000"
	if err := execution.RetryExpired(ctx, db, wrong, "recovering"); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("wrong token retry=%v", err)
	}
	if err := execution.RetryExpired(ctx, db, *lease, "recovering"); err != nil {
		t.Fatalf("RetryExpired: %v", err)
	}
	var pending execution.TaskExecution
	if err := db.Where("execution_id = ?", item.ExecutionID).First(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending.Status != execution.ExecutionStatusPending || pending.StartedAt != nil || pending.CompletedAt != nil || pending.ExecutionTimeMs != nil || pending.Attempt != 1 || pending.LeaseToken != nil || pending.LeaseOwner != nil || pending.LeaseExpiresAt != nil || !pending.CreatedAt.Equal(item.CreatedAt) {
		t.Fatalf("recovered pending = %#v", pending)
	}
	assertStale := func() {
		t.Helper()
		if err := execution.UpdateWithLease(ctx, db, *lease, map[string]interface{}{"progress": 99}); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("late progress = %v, want conflict", err)
		}
		if err := execution.RenewLease(ctx, db, *lease, time.Hour); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("late renewal = %v, want conflict", err)
		}
		if err := execution.CompleteWithLease(ctx, db, *lease, execution.ExecutionStatusSuccess, nil); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("late completion = %v, want conflict", err)
		}
	}
	assertStale()
	options.LeaseDuration = time.Hour
	second, secondLease, err := execution.ClaimNext(ctx, db, options)
	if err != nil || second == nil || secondLease == nil {
		t.Fatalf("second claim = %#v %#v, %v", second, secondLease, err)
	}
	if second.ExecutionID != item.ExecutionID || !second.CreatedAt.Equal(item.CreatedAt) || second.StartedAt == nil || !second.StartedAt.After(*first.StartedAt) || second.Attempt != 2 || secondLease.Token == "" || secondLease.Token == lease.Token {
		t.Fatalf("second claim identity/time = %#v %#v", second, secondLease)
	}
	assertStale()
	if err := execution.UpdateWithLease(ctx, db, *secondLease, map[string]interface{}{"progress": 50}); err != nil {
		t.Fatal(err)
	}
	if err := execution.RenewLease(ctx, db, *secondLease, 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	var running execution.TaskExecution
	if err := db.Where("execution_id = ?", item.ExecutionID).First(&running).Error; err != nil {
		t.Fatal(err)
	}
	if running.StartedAt == nil || !running.StartedAt.Equal(*second.StartedAt) || running.Progress != 50 {
		t.Fatalf("heartbeat/progress changed attempt start: %#v", running)
	}
}

func TestExpiredAttemptRejectsOwnerWritesBeforeRecovery(t *testing.T) {
	db := newLeaseTestDB(t)
	now := time.Now().UTC()
	item := execution.TaskExecution{TenantID: 7, ExecutionID: "expired-owner", Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, Source: execution.ModuleMeta, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
	_, lease, err := execution.ClaimNext(context.Background(), db, execution.ClaimOptions{Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, WorkerID: "meta-worker-1", LeaseDuration: time.Second})
	if err != nil || lease == nil {
		t.Fatalf("claim execution: %#v, %v", lease, err)
	}
	if err := db.Model(&item).Update("lease_expires_at", now.Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := execution.UpdateWithLease(context.Background(), db, *lease, map[string]interface{}{"progress": 50}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("expired progress error = %v, want conflict", err)
	}
	if err := execution.RenewLease(context.Background(), db, *lease, time.Minute); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("expired renew error = %v, want conflict", err)
	}
	if err := execution.CompleteWithLease(context.Background(), db, *lease, execution.ExecutionStatusSuccess, nil); !errors.Is(err, commonapi.ErrConflict) {
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
