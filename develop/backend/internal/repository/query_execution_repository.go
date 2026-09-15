package repository

import (
	"context"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// QueryExecutionRepository owns the single durable queue for every Develop
// query, regardless of whether Develop or Orchestrator created it.
type QueryExecutionRepository struct {
	db *gorm.DB
}

// RecoverUnleased closes running query executions that predate or violate the
// lease protocol. A valid claim always writes the full lease identity in the
// same transaction as pending -> running.
func (r *QueryExecutionRepository) RecoverUnleased(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	count := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where(
			"module = ? AND task_type = ? AND execution_boundary = ? AND status = ? AND (attempt <= 0 OR lease_token IS NULL OR lease_owner IS NULL OR lease_expires_at IS NULL)",
			commonExecution.ModuleDevelop, commonExecution.TaskTypeQuery, commonExecution.ExecutionBoundaryBounded, commonExecution.ExecutionStatusRunning,
		).Order("created_at ASC, id ASC").Limit(limit)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		var items []commonExecution.TaskExecution
		if err := query.Find(&items).Error; err != nil {
			return err
		}
		for index := range items {
			item := &items[index]
			fields := map[string]interface{}{
				"status": commonExecution.ExecutionStatusFailed, "completed_at": now.UTC(), "updated_at": now.UTC(),
				"lease_owner": nil, "lease_token": nil, "lease_expires_at": nil, "progress": 100, "current_step": nil,
				"error_details": commonModels.JSONMap{"code": "develop.query.lease_missing", "message": "Develop query execution has no valid lease"},
			}
			if item.StartedAt != nil {
				fields["execution_time_ms"] = now.Sub(*item.StartedAt).Milliseconds()
			}
			result := tx.Model(&commonExecution.TaskExecution{}).
				Where("execution_id = ? AND tenant_id = ? AND status = ? AND (attempt <= 0 OR lease_token IS NULL OR lease_owner IS NULL OR lease_expires_at IS NULL)", item.ExecutionID, item.TenantID, commonExecution.ExecutionStatusRunning).
				Updates(fields)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("Develop query execution %s changed during unleased recovery", item.ExecutionID)
			}
			if err := updateDevTaskExecutionSummary(tx, item, commonExecution.ExecutionStatusFailed, now); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

func NewQueryExecutionRepository(db *gorm.DB) *QueryExecutionRepository {
	return &QueryExecutionRepository{db: db}
}

func (r *QueryExecutionRepository) ClaimNext(
	ctx context.Context,
	workerID string,
	now time.Time,
	leaseDuration time.Duration,
	excludedEngineIDs []uint,
) (*commonExecution.TaskExecution, *commonExecution.Lease, error) {
	var execution *commonExecution.TaskExecution
	var lease *commonExecution.Lease
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claimDB, filterErr := excludeQueryEngines(tx, excludedEngineIDs)
		if filterErr != nil {
			return filterErr
		}
		var err error
		execution, lease, err = commonExecution.ClaimNext(ctx, claimDB, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleDevelop, TaskType: commonExecution.TaskTypeQuery,
			WorkerID: workerID, Now: now, LeaseDuration: leaseDuration,
		})
		if err != nil || execution == nil {
			return err
		}
		return updateDevTaskExecutionSummary(tx, execution, commonExecution.ExecutionStatusRunning, now)
	})
	return execution, lease, err
}

func excludeQueryEngines(tx *gorm.DB, engineIDs []uint) (*gorm.DB, error) {
	if len(engineIDs) == 0 {
		return tx, nil
	}
	switch tx.Dialector.Name() {
	case "postgres":
		return tx.Where(`COALESCE(
			CASE
				WHEN jsonb_typeof(execution_config -> 'engine_id') = 'number'
				THEN (execution_config ->> 'engine_id')::numeric
			END,
			-1
		) NOT IN ?`, engineIDs), nil
	case "sqlite":
		return tx.Where(`COALESCE(
			CASE
				WHEN json_type(execution_config, '$.engine_id') IN ('integer', 'real')
				THEN CAST(json_extract(execution_config, '$.engine_id') AS INTEGER)
			END,
			-1
		) NOT IN ?`, engineIDs), nil
	default:
		return nil, fmt.Errorf("Develop query engine concurrency is unsupported for %s", tx.Dialector.Name())
	}
}

func (r *QueryExecutionRepository) FailClaimed(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	code string,
	message string,
	completedAt time.Time,
) error {
	fields := map[string]interface{}{
		"progress":     100,
		"current_step": nil,
		"error_details": commonModels.JSONMap{
			"code": code, "message": message,
		},
	}
	if execution != nil && execution.StartedAt != nil {
		fields["execution_time_ms"] = completedAt.Sub(*execution.StartedAt).Milliseconds()
	}
	return r.CompleteWithLease(ctx, execution, lease, commonExecution.ExecutionStatusFailed, completedAt, fields)
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
			Now: now, Limit: limit,
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
