package repository

import (
	"context"
	"fmt"

	"github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
)

// AcknowledgeStopped may record an old invocation's genuine completion after
// fencing loss. It cannot update the current lease, execution, task or result.
func (r *RuntimeLeaseRepository) AcknowledgeStopped(ctx context.Context, claim RuntimeLeaseClaim) error {
	if claim.Task.ID == 0 || claim.Task.TenantID == 0 || claim.Execution.ExecutionID == "" || claim.Lease.ExecutionID != claim.Execution.ExecutionID || claim.Lease.OwnerInstanceID == "" || claim.Lease.FencingToken == 0 {
		return fmt.Errorf("complete runtime invocation identity required")
	}
	now, err := execution.DatabaseNow(r.db.WithContext(ctx))
	if err != nil {
		return err
	}
	query := r.db.WithContext(ctx).Model(&models.RuntimeSession{}).Where("task_id = ? AND tenant_id = ? AND execution_id = ? AND owner_instance_id = ? AND fencing_token = ?", claim.Task.ID, claim.Task.TenantID, claim.Execution.ExecutionID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken)
	result := query.Where("stopped_at IS NULL").Update("stopped_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.RuntimeSession{}).Where("task_id = ? AND tenant_id = ? AND execution_id = ? AND owner_instance_id = ? AND fencing_token = ? AND stopped_at IS NOT NULL", claim.Task.ID, claim.Task.TenantID, claim.Execution.ExecutionID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrRuntimeLeaseLost
	}
	return nil
}

// RequireRuntimeStopped checks every admitted invocation, including expired
// generations replaced by recovery. Missing historical evidence fails closed.
func RequireRuntimeStopped(ctx context.Context, db *gorm.DB, tenant, task uint) error {
	if tenant == 0 || task == 0 {
		return ErrTaskDeletionRuntimeActive
	}
	var pending int64
	if err := db.WithContext(ctx).Model(&models.RuntimeSession{}).Where("task_id = ? AND tenant_id = ? AND stopped_at IS NULL", task, tenant).Count(&pending).Error; err != nil {
		return err
	}
	if pending != 0 {
		return ErrTaskDeletionRuntimeActive
	}
	// A legacy or damaged lease with no matching invocation is not proof that
	// the old runtime stopped, even if its owner was cleared by recovery.
	if err := db.WithContext(ctx).Table("transfer.runtime_leases AS lease").Where("lease.task_id = ? AND lease.fencing_token > 0", task).Where(`NOT EXISTS (SELECT 1 FROM transfer.runtime_sessions session WHERE session.task_id = ? AND session.tenant_id = ? AND session.execution_id = lease.execution_id AND session.fencing_token = lease.fencing_token AND session.stopped_at IS NOT NULL)`, task, tenant).Count(&pending).Error; err != nil {
		return err
	}
	if pending != 0 {
		return ErrTaskDeletionRuntimeActive
	}
	return nil
}
