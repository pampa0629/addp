package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type taskExecutionClaimSpec struct {
	TaskModel               interface{}
	TaskType                string
	TaskLabel               string
	TaskName                func() string
	TaskConfig              func() commonModels.JSONMap
	CurrentResultModel      interface{}
	ExcludedResultStatuses  []string
	OverwriteExistingResult bool
	BeforeCreate            func(*gorm.DB) error
}

var ErrExistingResultActionRequired = errors.New("existing result action is required")

type taskExecutionCompletionSpec struct {
	TaskModel       interface{}
	ResultModel     interface{}
	ResultID        uint
	ResultFields    map[string]interface{}
	ExecutionFields map[string]interface{}
}

type taskExecutionLifecycle struct {
	db *gorm.DB
}

func newTaskExecutionLifecycle(db *gorm.DB) taskExecutionLifecycle {
	return taskExecutionLifecycle{db: db}
}

func (l taskExecutionLifecycle) Claim(
	ctx context.Context,
	taskID, tenantID uint,
	execution *commonExecution.TaskExecution,
	spec taskExecutionClaimSpec,
) error {
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", taskID, tenantID).
			First(spec.TaskModel).Error; err != nil {
			return err
		}

		sourceTaskID := commonExecution.NewSourceTaskIDFromUint(taskID)
		var activeCount int64
		if err := tx.Model(&commonExecution.TaskExecution{}).
			Where(
				"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ?",
				int(tenantID), commonExecution.ModuleManager, spec.TaskType, *sourceTaskID,
				[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning},
			).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return fmt.Errorf("%w: %s task %d already has an active execution", commonAPI.ErrConflict, spec.TaskLabel, taskID)
		}
		if spec.CurrentResultModel != nil && !spec.OverwriteExistingResult {
			var currentResultCount int64
			resultQuery := tx.Model(spec.CurrentResultModel).
				Where("tenant_id = ? AND task_id = ?", tenantID, taskID)
			if len(spec.ExcludedResultStatuses) > 0 {
				resultQuery = resultQuery.Where("status NOT IN ?", spec.ExcludedResultStatuses)
			}
			if err := resultQuery.Count(&currentResultCount).Error; err != nil {
				return err
			}
			if currentResultCount > 0 {
				return fmt.Errorf("%w: %s task %d", ErrExistingResultActionRequired, spec.TaskLabel, taskID)
			}
		}
		if spec.BeforeCreate != nil {
			if err := spec.BeforeCreate(tx); err != nil {
				return err
			}
		}

		execution.SourceTaskID = sourceTaskID
		taskName := spec.TaskName()
		execution.SourceTaskName = &taskName
		if len(execution.ExecutionConfig) == 0 {
			execution.ExecutionConfig = spec.TaskConfig().Clone()
			if execution.ExecutionConfig == nil {
				execution.ExecutionConfig = commonModels.JSONMap{}
			}
		}
		if err := tx.Create(execution).Error; err != nil {
			return err
		}

		result := tx.Model(spec.TaskModel).
			Where("id = ? AND tenant_id = ?", taskID, tenantID).
			Updates(map[string]interface{}{
				"last_execution_id": execution.ExecutionID, "last_execution_status": commonExecution.ExecutionStatusPending,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: %s task %d cannot record pending execution %s", commonAPI.ErrConflict, spec.TaskLabel, taskID, execution.ExecutionID)
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %s task %d", commonAPI.ErrNotFound, spec.TaskLabel, taskID)
	}
	return err
}

func (l taskExecutionLifecycle) Start(
	ctx context.Context,
	taskID, tenantID uint,
	executionID string,
	startedAt time.Time,
	taskModel interface{},
	taskLabel string,
) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != executionID || lease.TenantID != int(tenantID) {
		return fmt.Errorf("%w: %s execution requires the claimed lease for %s", commonAPI.ErrConflict, taskLabel, executionID)
	}
	// ClaimNext already advances both the execution and its owning task to
	// running atomically. Start remains only as a fenced ownership check at the
	// existing executor boundary.
	return commonExecution.UpdateWithLease(ctx, l.db, lease, map[string]interface{}{"updated_at": startedAt.UTC()})
}

func (l taskExecutionLifecycle) Complete(
	ctx context.Context,
	taskID, tenantID uint,
	executionID string,
	completedAt time.Time,
	spec taskExecutionCompletionSpec,
	taskLabel string,
) error {
	status, ok := spec.ExecutionFields["status"].(string)
	if !ok || status == "" {
		return fmt.Errorf("%s execution completion status is required", taskLabel)
	}
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != executionID || lease.TenantID != int(tenantID) {
		return fmt.Errorf("%w: %s execution requires the claimed lease for %s", commonAPI.ErrConflict, taskLabel, executionID)
	}

	return l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if spec.ResultID > 0 && len(spec.ResultFields) > 0 {
			result := tx.Model(spec.ResultModel).
				Where("id = ? AND tenant_id = ? AND last_execution_id = ?", spec.ResultID, tenantID, executionID).
				Updates(spec.ResultFields)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("%w: %s result %d is not owned by execution %s", commonAPI.ErrConflict, taskLabel, spec.ResultID, executionID)
			}
		}

		fields := make(map[string]interface{}, len(spec.ExecutionFields))
		for key, value := range spec.ExecutionFields {
			if key != "status" && key != "completed_at" && key != "updated_at" {
				fields[key] = value
			}
		}
		if err := commonExecution.CompleteWithLease(ctx, tx, lease, status, completedAt, fields); err != nil {
			return err
		}

		result := tx.Model(spec.TaskModel).
			Where(
				"id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?",
				taskID, tenantID, executionID, commonExecution.ExecutionStatusRunning,
			).
			Updates(map[string]interface{}{
				"last_execution_id": executionID, "last_execution_status": status, "updated_at": completedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: %s task %d is not running for execution %s", commonAPI.ErrConflict, taskLabel, taskID, executionID)
		}
		return nil
	})
}
