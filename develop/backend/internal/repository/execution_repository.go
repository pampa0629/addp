package repository

import (
	"context"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/httptrace"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExecutionRepository owns the single durable queue for bounded Develop tasks.
// Owner state and closed events commit with the same fenced transaction.
type ExecutionRepository struct {
	db *gorm.DB
}

var executionTaskTypes = []string{commonExecution.TaskTypeQuery, commonExecution.TaskTypeWorkflow, commonExecution.TaskTypeScript}

func (r *ExecutionRepository) scoped() *gorm.DB {
	return r.db.Where("module = ? AND task_type IN ?", commonExecution.ModuleDevelop, executionTaskTypes)
}

// RecoverUnleased closes running Develop executions that predate or violate the
// lease protocol. A valid claim always writes the full lease identity in the
// same transaction as pending -> running.
func (r *ExecutionRepository) RecoverUnleased(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	count := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where(
			"module = ? AND task_type IN ? AND execution_boundary = ? AND status = ? AND (attempt <= 0 OR lease_token IS NULL OR lease_owner IS NULL OR lease_expires_at IS NULL)",
			commonExecution.ModuleDevelop, executionTaskTypes, commonExecution.ExecutionBoundaryBounded, commonExecution.ExecutionStatusRunning,
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
			fields := map[string]interface{}{"progress": 100, "current_step": nil, "error_details": commonModels.JSONMap{"code": "develop." + item.TaskType + ".lease_missing", "message": "Develop execution has no valid lease; remote effects are not replayed"}}
			if err := commonExecution.FailUnleased(ctx, tx, item.ExecutionID, item.TenantID, item.Module, item.TaskType, fields); err != nil {
				return err
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

func NewExecutionRepository(db *gorm.DB) *ExecutionRepository {
	return &ExecutionRepository{db: db}
}

func (r *ExecutionRepository) ClaimNext(
	taskType string,
	ctx context.Context,
	workerID string,
	now time.Time,
	leaseDuration time.Duration,
	excludedEngineIDs []uint,
) (*commonExecution.TaskExecution, *commonExecution.Lease, error) {
	if !validExecutionTaskType(taskType) {
		return nil, nil, fmt.Errorf("unsupported Develop execution task type")
	}
	var execution *commonExecution.TaskExecution
	var lease *commonExecution.Lease
	var stage *httptrace.ExecutionStage
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if taskType != commonExecution.TaskTypeQuery && len(excludedEngineIDs) != 0 {
			return fmt.Errorf("engine concurrency filter is query-only")
		}
		claimDB, filterErr := excludeQueryEngines(tx, excludedEngineIDs)
		if filterErr != nil {
			return filterErr
		}
		var err error
		execution, lease, err = commonExecution.ClaimNext(ctx, claimDB, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleDevelop, TaskType: taskType,
			WorkerID: workerID, LeaseDuration: leaseDuration,
		})
		if err != nil || execution == nil {
			return err
		}
		stageCtx, started := queryExecutionStage(ctx, execution, httptrace.PhaseClaim)
		stage = started
		if stage.Reference() != nil {
			metadata := queryStageMetadata(execution.Metadata, stage)
			if err := commonExecution.UpdateWithLease(stageCtx, tx, *lease, map[string]interface{}{"metadata": metadata}); err != nil {
				return err
			}
			execution.Metadata = metadata
		}
		if err := commonExecution.AppendBoundedEvent(ctx, tx, *lease, commonExecution.EventInput{Kind: "started"}); err != nil {
			return err
		}
		return updateDevTaskExecutionSummary(tx, execution, commonExecution.ExecutionStatusRunning, now)
	})
	if execution != nil {
		endQueryExecutionStage(stage, execution.ExecutionID, commonExecution.ExecutionStatusSuccess, err)
	}
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

func (r *ExecutionRepository) FailClaimed(
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
	return r.CompleteWithLease(ctx, execution, lease, commonExecution.ExecutionStatusFailed, completedAt, fields)
}

func (r *ExecutionRepository) Renew(ctx context.Context, lease commonExecution.Lease, duration time.Duration) error {
	return commonExecution.RenewLease(ctx, r.scoped(), lease, duration)
}

func (r *ExecutionRepository) AttemptIsTerminal(ctx context.Context, lease commonExecution.Lease) (bool, error) {
	return commonExecution.AttemptIsTerminal(ctx, r.scoped(), lease)
}

func (r *ExecutionRepository) AttachAuthorizationWithLease(ctx context.Context, lease commonExecution.Lease, authorizationID int64, expiresAt time.Time) error {
	return commonExecution.AttachAuthorizationWithLease(ctx, r.scoped(), lease, authorizationID, expiresAt)
}

func (r *ExecutionRepository) UpdateProgressWithLease(ctx context.Context, lease commonExecution.Lease, progress int, currentStep string) error {
	if progress < 0 || progress > 100 {
		return fmt.Errorf("Develop execution progress must be between 0 and 100")
	}
	return commonExecution.UpdateWithEvent(ctx, r.scoped(), lease,
		map[string]interface{}{"progress": progress, "current_step": currentStep},
		commonExecution.EventInput{Kind: "progress", Counters: map[string]int64{"progress": int64(progress)}})
}

func (r *ExecutionRepository) CompleteWithLease(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	status string,
	completedAt time.Time,
	fields map[string]interface{},
) error {
	if execution == nil || execution.ExecutionID != lease.ExecutionID || execution.TenantID != lease.TenantID ||
		execution.Module != commonExecution.ModuleDevelop || !validExecutionTaskType(execution.TaskType) {
		return fmt.Errorf("Develop execution completion must match its execution lease")
	}
	var stage *httptrace.ExecutionStage
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := commonExecution.LockActiveLease(ctx, tx, lease); err != nil {
			return err
		}
		stageCtx := ctx
		if execution.TaskType == commonExecution.TaskTypeQuery {
			current, err := ownedQueryExecution(ctx, tx, lease)
			if err != nil {
				return err
			}
			stageCtx, stage = queryExecutionStage(ctx, current, httptrace.PhaseTerminal)
			fields, err = queryCompletionFields(current.Metadata, fields, stage)
			if err != nil {
				return err
			}
		}
		if err := updateDevTaskExecutionSummary(tx, execution, status, completedAt); err != nil {
			return err
		}
		return commonExecution.CompleteWithEvent(stageCtx, tx.Where("module = ? AND task_type = ?", commonExecution.ModuleDevelop, execution.TaskType), lease, status, fields)
	})
	endQueryExecutionStage(stage, execution.ExecutionID, status, err)
	return err
}

// RecoverExpired fails closed. Relation-result queries may already have
// partially written their runtime target and must be rerun by Orchestrator with
// a fresh target instead of replaying the same execution.
func (r *ExecutionRepository) RecoverExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	count := 0
	var stages []struct {
		stage       *httptrace.ExecutionStage
		executionID string
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleDevelop, TaskTypes: executionTaskTypes, Limit: limit,
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
					"code":    "develop." + item.TaskType + ".lease_expired",
					"message": "Develop execution lease expired; remote effects are not replayed automatically",
				},
			}
			stageCtx, stage := queryExecutionStage(ctx, &item, httptrace.PhaseTerminal)
			stages = append(stages, struct {
				stage       *httptrace.ExecutionStage
				executionID string
			}{stage, item.ExecutionID})
			if stage.Reference() != nil {
				fields["metadata"] = queryStageMetadata(item.Metadata, stage)
			}
			if err := commonExecution.FailExpiredWithEvent(stageCtx, tx, lease, fields); err != nil {
				return err
			}
			if err := updateDevTaskExecutionSummary(tx, &item, commonExecution.ExecutionStatusFailed, now); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	for _, checkpoint := range stages {
		endQueryExecutionStage(checkpoint.stage, checkpoint.executionID, commonExecution.ExecutionStatusFailed, err)
	}
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

func validExecutionTaskType(value string) bool {
	return value == commonExecution.TaskTypeQuery || value == commonExecution.TaskTypeWorkflow || value == commonExecution.TaskTypeScript
}

// RecordWorkflowReceipt keeps only the remote identity in owner-only metadata.
// It is not a progress event and cannot bypass the active Workflow lease.
func (r *ExecutionRepository) RecordWorkflowReceipt(ctx context.Context, lease commonExecution.Lease, runtimeID string) error {
	if runtimeID == "" {
		return fmt.Errorf("workflow runtime receipt is empty")
	}
	return commonExecution.UpdateWithLease(ctx, r.db.Where("module = ? AND task_type = ?", commonExecution.ModuleDevelop, commonExecution.TaskTypeWorkflow), lease, map[string]interface{}{"metadata": commonModels.JSONMap{"runtime_execution_id": runtimeID}})
}
