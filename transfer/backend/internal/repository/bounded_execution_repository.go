package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *TaskRepository) ClaimNextBoundedExecution(ctx context.Context, workerID string, leaseDuration time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, error) {
	var execution *commonExecution.TaskExecution
	var lease *commonExecution.Lease
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		execution, lease, err = commonExecution.ClaimNext(ctx, tx, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleTransfer, TaskType: commonExecution.TaskTypeSync,
			WorkerID: workerID, LeaseDuration: leaseDuration,
		})
		if err != nil || execution == nil {
			return err
		}
		if err := commonExecution.AppendBoundedEvent(ctx, tx, *lease, commonExecution.EventInput{Kind: "started"}); err != nil {
			return err
		}
		if execution.SourceTaskID == nil {
			return nil
		}
		taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
		if err != nil {
			return fmt.Errorf("transfer execution %s has invalid source task: %w", execution.ExecutionID, err)
		}
		var task models.TransferTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", taskID, execution.TenantID).First(&task).Error; err != nil {
			return err
		}
		if isReplayExecution(execution) {
			return commonExecution.UpdateWithLease(ctx, tx, *lease, nil)
		}
		result := tx.Model(&task).
			Where("last_execution_id = ? AND last_execution_status = ?", execution.ExecutionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"status": models.TaskStatusRunning, "last_run_at": execution.StartedAt,
				"last_execution_status": commonExecution.ExecutionStatusRunning,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("transfer task %d summary no longer matches execution %s", task.ID, execution.ExecutionID)
		}
		return commonExecution.UpdateWithLease(ctx, tx, *lease, nil)
	})
	if err != nil {
		return nil, nil, err
	}
	if execution == nil {
		return nil, nil, nil
	}
	return execution, lease, nil
}

// FailExpiredBoundedExecutions fails closed. A runtime-bound existing table may
// already contain partial writes, so a fresh orchestration execution must bind
// a fresh target instead of replaying the same lease.
func (r *TaskRepository) FailExpiredBoundedExecutions(ctx context.Context, limit int) (int, error) {
	count := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleTransfer, TaskType: commonExecution.TaskTypeSync, Limit: limit,
		})
		if err != nil {
			return err
		}
		for i := range items {
			item := items[i]
			lease, err := commonExecution.LeaseFromExecution(item)
			if err != nil {
				return err
			}
			fields := map[string]interface{}{
				"error_details": commonModels.JSONMap{
					"code":    "transfer.execution.lease_expired_recovery_required",
					"message": "transfer bounded execution lease expired; inspect the target state before retrying",
				},
			}
			if err := commonExecution.FailExpired(ctx, tx, lease, fields); err != nil {
				return err
			}
			if item.SourceTaskID != nil && !isReplayExecution(&item) {
				taskID, parseErr := strconv.ParseUint(*item.SourceTaskID, 10, 64)
				if parseErr != nil {
					return parseErr
				}
				result := tx.Model(&models.TransferTask{}).
					Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, item.TenantID, item.ExecutionID, commonExecution.ExecutionStatusRunning).
					Updates(map[string]interface{}{
						"status": models.TaskStatusIdle, "progress": 0,
						"last_execution_status": commonExecution.ExecutionStatusFailed,
					})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return fmt.Errorf("transfer task %d summary no longer matches expired execution %s", taskID, item.ExecutionID)
				}
			}
			count++
		}
		return nil
	})
	return count, err
}

func (r *TaskRepository) RenewBoundedExecutionLease(ctx context.Context, lease commonExecution.Lease, duration time.Duration) error {
	return commonExecution.RenewLease(ctx, r.db, lease, duration)
}

func (r *TaskRepository) BoundedExecutionAttemptIsTerminal(ctx context.Context, lease commonExecution.Lease) (bool, error) {
	return commonExecution.AttemptIsTerminal(ctx, r.db, lease)
}

func isReplayExecution(execution *commonExecution.TaskExecution) bool {
	if execution == nil {
		return false
	}
	if _, ok := execution.ExecutionConfig["replay"]; ok {
		return true
	}
	_, ok := execution.Metadata["replay"]
	return ok
}
