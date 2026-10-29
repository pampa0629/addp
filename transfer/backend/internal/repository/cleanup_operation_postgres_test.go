package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Called by the registered continuous fencing PG gate, using its owned database.
func testCleanupControlPostgres(t *testing.T, db *gorm.DB) {
	task := models.TransferTask{TenantID: 999998, Name: "cleanup-control-" + uuid.NewString(), TaskType: "sync", Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped, Config: models.JSONMap{"runtime": map[string]any{"boundary": "continuous"}}}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, model := range []any{&models.CleanupOperation{}, &models.RuntimeSession{}, &models.RuntimeLease{}} {
			if err := db.Where("task_id=?", task.ID).Delete(model).Error; err != nil {
				t.Error(err)
			}
		}
		if err := db.Unscoped().Delete(&task).Error; err != nil {
			t.Error(err)
		}
	})
	now := time.Now().UTC()
	oldID, currentID := uuid.NewString(), uuid.NewString()
	for i, id := range []string{oldID, currentID} {
		if err := db.Create(&models.RuntimeSession{TaskID: task.ID, TenantID: task.TenantID, ExecutionID: id, OwnerInstanceID: id, FencingToken: uint64(i + 1), StartedAt: now.Add(-time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.RuntimeLease{TaskID: task.ID, ExecutionID: currentID, OwnerInstanceID: currentID, FencingToken: 2, LeaseUntil: now.Add(-time.Minute), HeartbeatAt: now.Add(-time.Minute), ClaimedAt: now.Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewRuntimeLeaseRepository(db, testContinuousRecoveryPolicy())
	claim := func(id string, token uint64) RuntimeLeaseClaim {
		return RuntimeLeaseClaim{Task: task, Execution: commonExecution.TaskExecution{ExecutionID: id}, Lease: models.RuntimeLease{TaskID: task.ID, ExecutionID: id, OwnerInstanceID: id, FencingToken: token}}
	}
	if err := RequireRuntimeStopped(t.Context(), db, task.TenantID, task.ID); !errors.Is(err, ErrTaskDeletionRuntimeActive) {
		t.Fatal("expired lease allowed cleanup", err)
	}
	if err := repo.AcknowledgeStopped(t.Context(), claim(currentID, 2)); err != nil {
		t.Fatal(err)
	}
	if err := RequireRuntimeStopped(t.Context(), db, task.TenantID, task.ID); !errors.Is(err, ErrTaskDeletionRuntimeActive) {
		t.Fatal("lost old invocation ignored", err)
	}
	if err := repo.AcknowledgeStopped(t.Context(), claim(oldID, 1)); err != nil {
		t.Fatal(err)
	}
	if err := RequireRuntimeStopped(t.Context(), db, task.TenantID, task.ID); err != nil {
		t.Fatal(err)
	}
	calls := 0
	invoke := func(ctx context.Context) error {
		calls++
		// Provider work can take the same task lock through a different transaction.
		// NOWAIT makes a retained prepare lock a deterministic failure, not a hang.
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var got models.TransferTask
			return tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).First(&got, task.ID).Error
		}); err != nil {
			t.Fatal("provider called under PG lock", err)
		}
		return context.DeadlineExceeded
	}
	target := struct{ Topic string }{"cleanup-fixture-" + uuid.NewString()}
	if err := ExecuteCleanupOperation(t.Context(), db, task.TenantID, task.ID, "dead_letter_topic", nil, target, invoke); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := ExecuteCleanupOperation(t.Context(), db, task.TenantID, task.ID, "dead_letter_topic", nil, target, invoke); !errors.Is(err, ErrCleanupOperationUnresolved) || calls != 1 {
		t.Fatal("unknown operation replayed", calls, err)
	}
	attempt := claimTestExecution(task, uuid.NewString())
	if _, err := NewTaskRepository(db).StartContinuousExecution(t.Context(), task.ID, task.TenantID, &attempt); !errors.Is(err, ErrCleanupOperationUnresolved) {
		t.Fatal("late producer started", err)
	}
	var count int64
	if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id=?", attempt.ExecutionID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("rejected producer left execution", count, err)
	}
}
