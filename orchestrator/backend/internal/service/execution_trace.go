package service

import (
	"context"

	execution "github.com/addp/common/execution"
	"github.com/addp/common/execution/observability"
	"github.com/addp/common/httptrace"
	"github.com/addp/common/models"
)

func startExecutionStage(ctx context.Context, item *execution.TaskExecution, phase httptrace.ExecutionPhase) (context.Context, *httptrace.ExecutionStage) {
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

func stageMetadata(metadata models.JSONMap, ref *observability.TraceReference) models.JSONMap {
	if ref == nil {
		return metadata
	}
	next, err := execution.LatestTraceMetadata(metadata, ref)
	if err != nil {
		return metadata
	}
	return next
}

func endExecutionStage(stage *httptrace.ExecutionStage, status string, err error) {
	if err != nil {
		stage.End(httptrace.OutcomeRejected)
		return
	}
	switch status {
	case execution.ExecutionStatusSuccess:
		stage.End(httptrace.OutcomeSuccess)
	case execution.ExecutionStatusFailed:
		stage.End(httptrace.OutcomeFailed)
	case execution.ExecutionStatusTimeout:
		stage.End(httptrace.OutcomeTimeout)
	case execution.ExecutionStatusCancelled:
		stage.End(httptrace.OutcomeCancelled)
	default:
		stage.End(httptrace.OutcomeRejected)
	}
}
