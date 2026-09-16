package repository

import (
	"context"
	"fmt"
	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
	"time"
)

func (r *PlanRepository) AttachPendingAuthorization(ctx context.Context, tenantID int64, executionID string, fields map[string]interface{}) error {
	fields["updated_at"] = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&commonExecution.TaskExecution{}).
		Where("tenant_id = ? AND execution_id = ? AND status = ? AND execution_authorization_id IS NULL", tenantID, executionID, commonExecution.ExecutionStatusPending).
		Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: quality execution %s cannot attach authorization", commonAPI.ErrConflict, executionID)
	}
	return nil
}

func (r *PlanRepository) FailPendingExecution(ctx context.Context, taskID, tenantID int64, executionID, errorCode string, completedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&commonExecution.TaskExecution{}).
			Where("tenant_id = ? AND execution_id = ? AND status = ?", tenantID, executionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"status": commonExecution.ExecutionStatusFailed, "completed_at": completedAt, "updated_at": completedAt,
				"error_details": commonModels.JSONMap{"code": errorCode, "message": "quality execution authorization could not be prepared"},
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: quality execution %s is not pending", commonAPI.ErrConflict, executionID)
		}
		return tx.Model(&models.QualityPlan{}).
			Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, tenantID, executionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{"last_run_at": completedAt, "last_execution_status": commonExecution.ExecutionStatusFailed}).Error
	})
}
