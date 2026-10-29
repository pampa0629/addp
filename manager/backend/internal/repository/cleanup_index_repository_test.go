package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func indexCleanupLease(t *testing.T, db *gorm.DB) (context.Context, execution.Lease) {
	t.Helper()
	if db.Dialector.Name() == "sqlite" {
		if err := executiontest.EnsureSQLiteStore(db); err != nil {
			t.Fatal(err)
		}
	} else if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "7", []string{"system.cleanup.execute"}))
	parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
	if err := execution.ApplyUserOrigin(ctx, &parent); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: 7, RequestedBy: 7, Action: "execute", CleanupMode: events.CleanupModeLogical, BasedOnScan: uuid.NewString(), Confirmation: events.CleanupConfirmation{Confirmed: true, RiskLevel: "low", ImpactDigest: strings.Repeat("a", 64)}, TriggerType: "manual", ExpectedModules: []string{"manager"}, RequestedAt: now, Deadline: now.Add(time.Minute)}
	if err := input.Seal(); err != nil {
		t.Fatal(err)
	}
	children, err := execution.CreateCleanupExecutions(ctx, db, &parent, input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ids := []string{parent.ExecutionID, children[0].ExecutionID}
		if db.Dialector.Name() == "postgres" {
			if err := db.Where("execution_id IN ?", ids).Delete(&execution.Event{}).Error; err != nil {
				t.Error(err)
			}
		}
		if err := db.Where("execution_id IN ?", ids).Delete(&execution.TaskExecution{}).Error; err != nil {
			t.Error(err)
		}
	})
	if _, _, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: parent.ExecutionID, TenantID: 7, Module: "system", TaskType: "cleanup", Source: "system"}, execution.ClaimOptions{Module: "system", TaskType: "cleanup", Source: "system", WorkerID: "parent", LeaseDuration: time.Minute}, nil); err != nil {
		t.Fatal(err)
	}
	_, lease, err := execution.ClaimCleanupChild(ctx, db, execution.PendingIdentity{ExecutionID: children[0].ExecutionID, TenantID: 7, Module: "manager", TaskType: "cleanup_executor", Source: "system"}, execution.ClaimOptions{Module: "manager", TaskType: "cleanup_executor", Source: "system", WorkerID: "cleanup-owner", LeaseDuration: time.Minute}, nil)
	if err != nil || lease == nil {
		t.Fatal("claim", err)
	}
	return ctx, *lease
}

func TestCleanupIndexHoldAndLateRegistration(t *testing.T) {
	for _, scenario := range []string{"restart", "completed", "failed", "stale_lease"} {
		t.Run(scenario, func(t *testing.T) {
			db := newContentDeliveryTestDB(t)
			repo := NewContentIndexDeliveryRepository(db, "content")
			epoch := openActiveContentIndex(t, repo)
			ctx, lease := indexCleanupLease(t, db)
			guard, err := repo.ObserveInventoryGuard(ctx, epoch)
			if err != nil {
				t.Fatal(err)
			}
			var operation *models.CleanupIndexOperation
			if err := db.Transaction(func(tx *gorm.DB) error {
				var e error
				operation, e = repo.BeginCleanupIndex(ctx, tx, lease, guard, strings.Repeat("b", 64), []uint{9})
				return e
			}); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(repo.ActivateIfSettled(ctx, epoch), ErrContentIndexIsolated) {
				t.Fatal("recovery released hold")
			}
			if !errors.Is(repo.RequireNoCleanupHold(ctx, epoch), ErrContentIndexIsolated) {
				t.Fatal("initialization ignored hold")
			}
			if _, err := repo.QueueDelete(ctx, epoch, 7, "tenant_id = 7"); !errors.Is(err, ErrContentIndexIsolated) {
				t.Fatal("generic delete bypassed hold", err)
			}
			if scenario == "restart" {
				next, err := repo.Open(ctx, "endpoint", true)
				if err != nil {
					t.Fatal(err)
				}
				if !errors.Is(repo.ActivateIfSettled(ctx, next), ErrContentIndexIsolated) {
					t.Fatal("restart cleared hold")
				}
				return
			}
			useLease := lease
			if scenario == "stale_lease" {
				useLease.Token = "old-token"
			}
			var delivery *models.ContentIndexDelivery
			err = db.Transaction(func(tx *gorm.DB) error {
				var e error
				delivery, e = repo.QueueCleanupDelete(ctx, tx, useLease, operation.ID, 9)
				return e
			})
			if scenario == "stale_lease" {
				if err == nil {
					t.Fatal("stale owner dispatched")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if claimed, err := repo.ClaimMaintenance(ctx, epoch, delivery); err == nil || claimed {
				t.Fatal("generic worker claimed Cleanup delivery")
			}
			if err := repo.Receipt(ctx, delivery, 1, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			// Receipt records a separate durable read fact before terminal verification.
			delivery, err = repo.Get(ctx, 7, delivery.ID)
			if err != nil {
				t.Fatal(err)
			}
			status := IndexDeliverySucceeded
			if scenario == "failed" {
				status = IndexDeliveryFailed
			}
			if err := repo.Finish(ctx, delivery, status); err != nil {
				t.Fatal(err)
			}
			err = db.Transaction(func(tx *gorm.DB) error { return repo.CompleteCleanupIndex(ctx, tx, lease, operation.ID) })
			if scenario == "failed" {
				if err == nil {
					t.Fatal("failed deletion cleared hold")
				}
				var count int64
				db.Model(&models.ContentIndexDelivery{}).Count(&count)
				if count != 1 {
					t.Fatal("failed Cleanup auto-replayed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.ActivateIfSettled(ctx, epoch); err != nil {
				t.Fatal(err)
			}
			late := &models.ContentIndexDelivery{TenantID: 7, EngineIDs: []uint{9}, DocumentIDs: []string{"late"}, Kind: IndexDeliveryWrite}
			if err := db.Transaction(func(tx *gorm.DB) error { return repo.Register(ctx, tx, epoch, late) }); !errors.Is(err, ErrContentIndexIsolated) {
				t.Fatal("late write revived target", err)
			}
			registerContentWrite(t, db, repo, epoch, 8, "another-tenant")
		})
	}
}
