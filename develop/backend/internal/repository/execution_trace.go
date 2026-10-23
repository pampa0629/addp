package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	execution "github.com/addp/common/execution"
	"github.com/addp/common/httptrace"
	"github.com/addp/common/models"
	"gorm.io/gorm"
)

func queryExecutionStage(ctx context.Context, item *execution.TaskExecution, phase httptrace.ExecutionPhase) (context.Context, *httptrace.ExecutionStage) {
	if item.TaskType != execution.TaskTypeQuery {
		return ctx, nil
	}
	enqueue, err := execution.EnqueueTrace(item)
	if err != nil {
		return ctx, nil
	}
	latest, err := execution.LatestTrace(item)
	if err != nil {
		return ctx, nil
	}
	return httptrace.StartExecutionStage(ctx, phase, item.Attempt, enqueue, latest)
}

func endQueryExecutionStage(stage *httptrace.ExecutionStage, executionID, status string, err error) {
	if err != nil {
		stage.EndExecution(executionID, httptrace.OutcomeRejected)
		return
	}
	switch status {
	case execution.ExecutionStatusSuccess:
		stage.EndExecution(executionID, httptrace.OutcomeSuccess)
	case execution.ExecutionStatusFailed:
		stage.EndExecution(executionID, httptrace.OutcomeFailed)
	case execution.ExecutionStatusTimeout:
		stage.EndExecution(executionID, httptrace.OutcomeTimeout)
	case execution.ExecutionStatusCancelled:
		stage.EndExecution(executionID, httptrace.OutcomeCancelled)
	default:
		stage.EndExecution(executionID, httptrace.OutcomeRejected)
	}
}

func queryStageMetadata(metadata models.JSONMap, stage *httptrace.ExecutionStage) models.JSONMap {
	next, err := execution.LatestTraceMetadata(metadata, stage.Reference())
	if err != nil {
		return metadata
	}
	return next
}

func ownedQueryExecution(ctx context.Context, tx *gorm.DB, lease execution.Lease) (*execution.TaskExecution, error) {
	var item execution.TaskExecution
	err := tx.WithContext(ctx).Where("module = ? AND task_type = ? AND execution_id = ? AND tenant_id = ? AND status = ? AND execution_boundary = ? AND attempt = ? AND lease_token = ? AND lease_owner = ? AND lease_expires_at >= ?",
		execution.ModuleDevelop, execution.TaskTypeQuery, lease.ExecutionID, lease.TenantID, execution.ExecutionStatusRunning, execution.ExecutionBoundaryBounded, lease.Attempt, lease.Token, lease.Owner, time.Now().UTC()).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("%w: Develop query lease is no longer current", commonapi.ErrConflict)
	}
	return &item, err
}

// RecordQueryDispatch records the hand-off checkpoint and ends before the query
// starts. SQL, query waiting and execution duration never belong to this span.
func (r *ExecutionRepository) RecordQueryDispatch(ctx context.Context, item *execution.TaskExecution, lease execution.Lease) error {
	stageCtx, stage := queryExecutionStage(ctx, item, httptrace.PhaseDispatch)
	if stage.Reference() == nil {
		return nil
	}
	err := r.db.WithContext(stageCtx).Transaction(func(tx *gorm.DB) error {
		item, err := ownedQueryExecution(ctx, tx, lease)
		if err != nil {
			return err
		}
		return execution.UpdateWithLease(stageCtx, tx, lease, map[string]interface{}{"metadata": queryStageMetadata(item.Metadata, stage)})
	})
	endQueryExecutionStage(stage, lease.ExecutionID, execution.ExecutionStatusSuccess, err)
	// Optional checkpoint storage is not execution admission. The original
	// progress/authorization/heartbeat fences still govern query dispatch.
	// A positively identified lost lease or business cancellation remains fatal.
	if errors.Is(err, commonapi.ErrConflict) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func queryCompletionFields(current models.JSONMap, fields map[string]interface{}, stage *httptrace.ExecutionStage) (map[string]interface{}, error) {
	next := make(map[string]interface{}, len(fields)+1)
	for key, value := range fields {
		next[key] = value
	}
	source := current
	if value, present := fields["metadata"]; present {
		// An explicit terminal object replaces business metadata, including an
		// explicit nil/empty object clearing old results. Only the authoritative
		// private observation is carried across this existing replacement boundary.
		switch value := value.(type) {
		case nil:
			source = nil
		case models.JSONMap:
			source = value
		case map[string]interface{}:
			source = models.JSONMap(value)
		default:
			return nil, errors.New("Develop query completion metadata must be a JSON object or nil")
		}
	}
	var metadata models.JSONMap
	if source != nil {
		metadata = make(models.JSONMap, len(source))
	}
	for key, value := range source {
		if key != execution.ObservabilityKey {
			metadata[key] = value
		}
	}
	if private, present := current[execution.ObservabilityKey]; present {
		if metadata == nil {
			metadata = models.JSONMap{}
		}
		metadata[execution.ObservabilityKey] = private
	}
	next["metadata"] = queryStageMetadata(metadata, stage)
	return next, nil
}
