package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCleanupOperationUnresolved = errors.New("transfer external cleanup operation is unresolved")

// ExternalAdmission composes C and current original-User authorization. Direct
// synchronous task commands retain their existing API authorization boundary.
type ExternalAdmission func(context.Context, func(*gorm.DB) error) (context.Context, context.CancelFunc, error)

type cleanupAdmissionKey struct{}

func WithCleanupAdmission(ctx context.Context, admission ExternalAdmission) context.Context {
	return context.WithValue(ctx, cleanupAdmissionKey{}, admission)
}

// ExecuteCleanupOperation persists dispatch intent before the external call.
// Only the original invocation may record its exact outcome after fencing loss.
func ExecuteCleanupOperation(ctx context.Context, db *gorm.DB, tenant, task uint, kind string, resource *models.CaptureResource, target any, invoke func(context.Context) error) error {
	if db == nil || tenant == 0 || task == 0 || kind == "" || invoke == nil {
		return fmt.Errorf("exact cleanup operation identity required")
	}
	encoded, err := json.Marshal(target)
	if err != nil || len(encoded) > 16384 {
		return fmt.Errorf("invalid external cleanup target")
	}
	identity, _ := json.Marshal([]any{tenant, task, kind, string(encoded)})
	digest := sha256.Sum256(identity)
	op := models.CleanupOperation{TargetKey: hex.EncodeToString(digest[:]), OperationID: uuid.NewString(), TenantID: tenant, TaskID: task, Kind: kind, Target: string(encoded), Status: "issued"}
	if lease, ok := execution.LeaseFromContext(ctx); ok {
		op.CleanupExecutionID, op.Attempt = lease.ExecutionID, lease.Attempt
	}
	if resource != nil {
		op.CaptureResourceID, op.ResourceVersion = resource.ID, resource.ResourceVersion
	}
	completed := false
	prepare := func(tx *gorm.DB) error {
		var current models.TransferTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", task, tenant).First(&current).Error; err != nil {
			return err
		}
		if current.DesiredState != models.TaskDesiredStateStopped {
			return ErrTaskDeletionRuntimeActive
		}
		if err := RequireRuntimeStopped(ctx, tx, tenant, task); err != nil {
			return err
		}
		if resource != nil {
			var count int64
			if err := tx.Model(&models.CaptureResource{}).Where("id = ? AND tenant_id = ? AND task_id = ? AND generation = ? AND source_connection_fingerprint = ? AND resource_version = ?", resource.ID, tenant, task, resource.Generation, resource.SourceConnectionFingerprint, resource.ResourceVersion).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return ErrCleanupOperationUnresolved
			}
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		op.CreatedAt = now
		// The target key is the unique dispatch fence, including direct commands.
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&op)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 1 {
			return nil
		}
		var existing models.CleanupOperation
		if err := tx.Where("target_key = ?", op.TargetKey).First(&existing).Error; err != nil {
			return err
		}
		if existing.TenantID != tenant || existing.TaskID != task || existing.Kind != kind || existing.Target != string(encoded) || existing.Status != "succeeded" {
			return ErrCleanupOperationUnresolved
		}
		completed = true
		return nil
	}
	callCtx := ctx
	cancel := func() {}
	if admit, ok := ctx.Value(cleanupAdmissionKey{}).(ExternalAdmission); ok {
		callCtx, cancel, err = admit(ctx, prepare)
	} else {
		err = db.WithContext(ctx).Transaction(prepare)
	}
	if err != nil {
		return err
	}
	defer cancel()
	if completed {
		return nil
	}
	if err := callCtx.Err(); err != nil {
		return err
	}
	err = invoke(callCtx)
	// This is a narrow control receipt, not a task, result or execution update.
	receiptCtx, receiptCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer receiptCancel()
	status := "succeeded"
	if err != nil {
		status = "unknown"
	}
	now, clockErr := execution.DatabaseNow(db.WithContext(receiptCtx))
	if clockErr != nil {
		return errors.Join(err, clockErr)
	}
	result := db.WithContext(receiptCtx).Model(&models.CleanupOperation{}).Where("target_key = ? AND operation_id = ? AND status = ?", op.TargetKey, op.OperationID, "issued").Updates(map[string]any{"status": status, "completed_at": now})
	if result.Error != nil {
		return errors.Join(err, result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.Join(err, ErrCleanupOperationUnresolved)
	}
	return err
}

// MutateCleanup composes a short Owner write with the caller's current C fence.
type CleanupMutation func(context.Context, func(*gorm.DB) error) error
type cleanupMutationKey struct{}

func WithCleanupMutation(ctx context.Context, mutate CleanupMutation) context.Context {
	return context.WithValue(ctx, cleanupMutationKey{}, mutate)
}
func MutateCleanup(ctx context.Context, db *gorm.DB, write func(*gorm.DB) error) error {
	if mutate, ok := ctx.Value(cleanupMutationKey{}).(CleanupMutation); ok {
		return mutate(ctx, write)
	}
	return db.WithContext(ctx).Transaction(write)
}

func (r *CaptureRepository) RunCleanupOperation(ctx context.Context, resource *models.CaptureResource, kind string, target any, invoke func(context.Context) error) error {
	if resource == nil {
		return ErrCleanupOperationUnresolved
	}
	return ExecuteCleanupOperation(ctx, r.db, resource.TenantID, resource.TaskID, kind, resource, target, invoke)
}

// RequireNoCleanupOperations prevents a new producer from resurrecting a target
// after destructive admission, including a successful effect whose final commit
// failed. Reopening such a task requires a separately defined domain operation.
func RequireNoCleanupOperations(ctx context.Context, db *gorm.DB, tenant, task uint) error {
	var count int64
	if err := db.WithContext(ctx).Model(&models.CleanupOperation{}).Where("tenant_id=? AND task_id=?", tenant, task).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrCleanupOperationUnresolved
	}
	return nil
}
