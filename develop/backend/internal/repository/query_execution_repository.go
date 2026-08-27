package repository

import (
	"context"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
)

// QueryExecutionRepository owns the durable Orchestrator query queue. Ad-hoc
// and Develop-triggered queries are intentionally outside this queue.
type QueryExecutionRepository struct {
	db *gorm.DB
}

func NewQueryExecutionRepository(db *gorm.DB) *QueryExecutionRepository {
	return &QueryExecutionRepository{db: db}
}

func (r *QueryExecutionRepository) ClaimNext(
	ctx context.Context,
	workerID string,
	now time.Time,
	leaseDuration time.Duration,
) (*commonExecution.TaskExecution, *commonExecution.Lease, error) {
	var execution *commonExecution.TaskExecution
	var lease *commonExecution.Lease
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		execution, lease, err = commonExecution.ClaimNext(ctx, tx, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleDevelop, TaskType: commonExecution.TaskTypeQuery,
			Source: commonExecution.ModuleOrchestrator, WorkerID: workerID,
			Now: now, LeaseDuration: leaseDuration,
		})
		if err != nil || execution == nil {
			return err
		}
		return updateDevTaskExecutionSummary(tx, execution, commonExecution.ExecutionStatusRunning, now)
	})
	return execution, lease, err
}

func (r *QueryExecutionRepository) Renew(ctx context.Context, lease commonExecution.Lease, expiresAt time.Time) error {
	return commonExecution.RenewLease(ctx, r.db, lease, expiresAt)
}

func (r *QueryExecutionRepository) AttemptIsTerminal(ctx context.Context, lease commonExecution.Lease) (bool, error) {
	return commonExecution.AttemptIsTerminal(ctx, r.db, lease)
}

func (r *QueryExecutionRepository) UpdateWithLease(ctx context.Context, lease commonExecution.Lease, fields map[string]interface{}) error {
	return commonExecution.UpdateWithLease(ctx, r.db, lease, fields)
}

func (r *QueryExecutionRepository) CompleteWithLease(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	status string,
	completedAt time.Time,
	fields map[string]interface{},
) error {
	if execution == nil {
		return fmt.Errorf("develop query execution is required")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := commonExecution.CompleteWithLease(ctx, tx, lease, status, completedAt, fields); err != nil {
			return err
		}
		return updateDevTaskExecutionSummary(tx, execution, status, completedAt)
	})
}

// RecoverExpired fails closed. Relation-result queries may already have
// partially written their runtime target and must be rerun by Orchestrator with
// a fresh target instead of replaying the same execution.
func (r *QueryExecutionRepository) RecoverExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	count := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleDevelop, TaskType: commonExecution.TaskTypeQuery,
			Source: commonExecution.ModuleOrchestrator, Now: now, Limit: limit,
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
					"code":    "develop.query.lease_expired",
					"message": "Develop query lease expired; query effects are not replayed automatically",
				},
			}
			if item.StartedAt != nil {
				fields["execution_time_ms"] = now.Sub(*item.StartedAt).Milliseconds()
			}
			if err := commonExecution.FailExpired(ctx, tx, lease, now, fields); err != nil {
				return err
			}
			if err := updateDevTaskExecutionSummary(tx, &item, commonExecution.ExecutionStatusFailed, now); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

func updateDevTaskExecutionSummary(tx *gorm.DB, execution *commonExecution.TaskExecution, status string, at time.Time) error {
	if execution == nil || execution.SourceTaskID == nil {
		return nil
	}
	taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
	if err != nil {
		return err
	}
	updates := map[string]interface{}{
		"last_execution_id":     execution.ExecutionID,
		"last_execution_status": status,
		"last_run_at":           at.UTC(),
	}
	query := tx.Model(&models.DevTask{}).Where("id = ? AND tenant_id = ?", taskID, execution.TenantID)
	if status != commonExecution.ExecutionStatusRunning {
		query = query.Where("last_execution_id = ?", execution.ExecutionID)
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("Develop task %d execution summary no longer matches %s", taskID, execution.ExecutionID)
	}
	return nil
}
