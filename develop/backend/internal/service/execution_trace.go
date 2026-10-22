package service

import (
	"context"

	execution "github.com/addp/common/execution"
	"github.com/addp/common/httptrace"
	"github.com/addp/common/models"
)

// createDevelopExecution strips the reserved namespace from caller-owned config
// before freezing only a real local enqueue phase.
func createDevelopExecution(ctx context.Context, repo *execution.TaskExecutionRepository, item *execution.TaskExecution) error {
	config := make(models.JSONMap, len(item.ExecutionConfig))
	for key, value := range item.ExecutionConfig {
		if key != execution.ObservabilityKey {
			config[key] = value
		}
	}
	item.ExecutionConfig = config
	if item.TaskType != execution.TaskTypeQuery {
		return repo.Create(ctx, item)
	}
	stageCtx, stage := httptrace.StartExecutionStage(ctx, httptrace.PhaseEnqueue, 0, nil, nil)
	defer stage.End(httptrace.OutcomeRejected)
	if ref := stage.Reference(); ref != nil {
		_ = execution.FreezeEnqueueTrace(item, ref)
	}
	if err := repo.Create(stageCtx, item); err != nil {
		return err
	}
	stage.End(httptrace.OutcomeSuccess)
	return nil
}
