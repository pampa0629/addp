package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
)

func TestCleanupOperationNeverReplaysAndBlocksLateProducer(t *testing.T) {
	for _, outcome := range []string{"success", "unknown", "prepare_rollback"} {
		t.Run(outcome, func(t *testing.T) {
			db := newTaskResourceRepositoryTestDB(t)
			task := models.TransferTask{ID: 11, TenantID: 7, Name: "cleanup", Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped}
			if err := db.Table("transfer.transfer_tasks").Create(map[string]any{"id": task.ID, "tenant_id": task.TenantID, "config": "{}", "status": task.Status, "desired_state": task.DesiredState}).Error; err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if outcome == "prepare_rollback" {
				ctx = WithCleanupAdmission(ctx, func(ctx context.Context, prepare func(*gorm.DB) error) (context.Context, context.CancelFunc, error) {
					err := db.Transaction(func(tx *gorm.DB) error {
						if err := prepare(tx); err != nil {
							return err
						}
						return errors.New("registration failed")
					})
					return nil, nil, err
				})
			}
			calls := 0
			invoke := func(context.Context) error {
				calls++
				if outcome == "unknown" {
					return context.DeadlineExceeded
				}
				return nil
			}
			target := struct{ Topic string }{"exact-topic"}
			firstErr := ExecuteCleanupOperation(ctx, db, 7, 11, "dead_letter_topic", nil, target, invoke)
			if outcome == "success" && firstErr != nil || outcome != "success" && firstErr == nil {
				t.Fatal("wrong outcome", firstErr)
			}
			secondErr := ExecuteCleanupOperation(ctx, db, 7, 11, "dead_letter_topic", nil, target, invoke)
			expected := 1
			if outcome == "prepare_rollback" {
				expected = 0
			}
			if calls != expected {
				t.Fatal("operation replayed", calls)
			}
			if outcome == "unknown" && !errors.Is(secondErr, ErrCleanupOperationUnresolved) {
				t.Fatal("unknown operation accepted", secondErr)
			}
			if outcome != "prepare_rollback" {
				if err := RequireNoCleanupOperations(t.Context(), db, 7, 11); !errors.Is(err, ErrCleanupOperationUnresolved) {
					t.Fatal("new producer ignored operation", err)
				}
				if err := RequireNoCleanupOperations(t.Context(), db, 8, 11); err != nil {
					t.Fatal("other Tenant incorrectly fenced", err)
				}
				var operation models.CleanupOperation
				if err := db.First(&operation).Error; err != nil {
					t.Fatal(err)
				}
				if operation.Target != "{\"Topic\":\"exact-topic\"}" || operation.OperationID == "" || operation.CreatedAt.IsZero() {
					t.Fatal("target evidence incomplete", operation)
				}
			}
		})
	}
}

func TestRuntimeStopRequiresAllInvocationsNotLeaseExpiry(t *testing.T) {
	db := newTaskResourceRepositoryTestDB(t)
	task := models.TransferTask{ID: 11, TenantID: 7, Name: "stopping", Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped}
	if err := db.Table("transfer.transfer_tasks").Create(map[string]any{"id": task.ID, "tenant_id": task.TenantID, "config": "{}", "status": task.Status, "desired_state": task.DesiredState}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i, id := range []string{"old", "current"} {
		if err := db.Create(&models.RuntimeSession{TaskID: 11, TenantID: 7, ExecutionID: id, OwnerInstanceID: id, FencingToken: uint64(i + 1), StartedAt: now.Add(-time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Table("transfer.runtime_leases").Create(map[string]any{"task_id": 11, "execution_id": "current", "owner_instance_id": "current", "fencing_token": 2, "lease_until": now.Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewRuntimeLeaseRepository(db, ContinuousRecoveryPolicy{})
	claim := func(id string, token uint64) RuntimeLeaseClaim {
		return RuntimeLeaseClaim{Task: task, Execution: commonExecution.TaskExecution{ExecutionID: id}, Lease: models.RuntimeLease{TaskID: 11, ExecutionID: id, OwnerInstanceID: id, FencingToken: token}}
	}
	if err := RequireRuntimeStopped(t.Context(), db, 7, 11); !errors.Is(err, ErrTaskDeletionRuntimeActive) {
		t.Fatal("expiry became stop evidence", err)
	}
	if err := repo.AcknowledgeStopped(t.Context(), claim("current", 2)); err != nil {
		t.Fatal(err)
	}
	if err := RequireRuntimeStopped(t.Context(), db, 7, 11); !errors.Is(err, ErrTaskDeletionRuntimeActive) {
		t.Fatal("old lost invocation ignored", err)
	}
	if err := repo.AcknowledgeStopped(t.Context(), claim("old", 99)); !errors.Is(err, ErrRuntimeLeaseLost) {
		t.Fatal("wrong generation acknowledged", err)
	}
	if err := repo.AcknowledgeStopped(t.Context(), claim("old", 1)); err != nil {
		t.Fatal(err)
	}
	if err := RequireRuntimeStopped(t.Context(), db, 7, 11); err != nil {
		t.Fatal(err)
	}
	var lease models.RuntimeLease
	if err := db.First(&lease).Error; err != nil {
		t.Fatal(err)
	}
	if lease.ExecutionID != "current" || lease.FencingToken != 2 {
		t.Fatal("late acknowledgement rewrote current lease")
	}
}
