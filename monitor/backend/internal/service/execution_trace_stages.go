package service

import (
	"context"
	"errors"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/common/execution/observability"
	"github.com/addp/monitor/internal/tracequery"
)

// ExecutionTraceLoader resolves only the requested execution's current Owner
// and returns its authorized, authoritative read snapshot.
type ExecutionTraceLoader func(context.Context) (*execution.TaskExecution, error)

type ExecutionTraceStagesResponse struct {
	ExecutionID string                `json:"execution_id"`
	Supported   bool                  `json:"supported"`
	Stages      []ExecutionTraceStage `json:"stages"`
}

type ExecutionTraceStage struct {
	Slot       string     `json:"slot"`
	State      string     `json:"state"`
	Phase      string     `json:"phase,omitempty"`
	Attempt    *int       `json:"attempt,omitempty"`
	Outcome    string     `json:"outcome,omitempty"`
	Start      *time.Time `json:"start,omitempty"`
	End        *time.Time `json:"end,omitempty"`
	DurationMS *float64   `json:"duration_ms,omitempty"`
}

// ExecutionStages shares admission and upstream capacity with platform Trace
// queries. One deadline begins before Owner authorization, without per-slot resets.
func (s *TraceService) ExecutionStages(ctx context.Context, principal string, load ExecutionTraceLoader) (ExecutionTraceStagesResponse, error) {
	if principal == "" || load == nil {
		return ExecutionTraceStagesResponse{}, tracequery.ErrInvalid
	}
	if s == nil {
		return ExecutionTraceStagesResponse{}, tracequery.ErrUpstream
	}
	release, err := s.acquire(principal)
	if err != nil {
		return ExecutionTraceStagesResponse{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var item *execution.TaskExecution
	err = s.callUpstream(ctx, func() error {
		var err error
		item, err = load(ctx)
		return err
	})
	if err != nil {
		return ExecutionTraceStagesResponse{}, err
	}
	if err = ctx.Err(); err != nil {
		return ExecutionTraceStagesResponse{}, err
	}
	if item == nil {
		return ExecutionTraceStagesResponse{}, tracequery.ErrUpstream
	}
	out := ExecutionTraceStagesResponse{ExecutionID: item.ExecutionID, Stages: []ExecutionTraceStage{}}
	out.Supported = item.ExecutionBoundary == execution.ExecutionBoundaryBounded &&
		(item.Module == execution.ModuleOrchestrator && item.TaskType == execution.TaskTypeOrchestration || item.Module == execution.ModuleDevelop && item.TaskType == execution.TaskTypeQuery)
	if !out.Supported {
		return out, nil
	}
	enqueue, err := execution.EnqueueTrace(item)
	if err != nil {
		return ExecutionTraceStagesResponse{}, tracequery.ErrUpstream
	}
	latest, err := execution.LatestTrace(item)
	if err != nil {
		return ExecutionTraceStagesResponse{}, tracequery.ErrUpstream
	}
	now := time.Now().UTC()
	budget := &traceSampleBudget{}
	details := map[observability.TraceReference]tracequery.Detail{}
	missing := map[observability.TraceReference]bool{}
	for i, ref := range []*observability.TraceReference{enqueue, latest} {
		slot := []string{"enqueue", "latest"}[i]
		stage := ExecutionTraceStage{Slot: slot, State: "not_recorded"}
		if ref == nil {
			out.Stages = append(out.Stages, stage)
			continue
		}
		// These audited producers always associate their own backend root.
		if ref.ModuleName != item.Module || ref.Role != "backend" {
			return ExecutionTraceStagesResponse{}, tracequery.ErrUpstream
		}
		if !ref.StartedAt.Before(now) || ref.StartedAt.Before(now.Add(-time.Duration(tracequery.Definition().RetentionHours)*time.Hour)) {
			stage.State = "outside_window"
			out.Stages = append(out.Stages, stage)
			continue
		}
		end := ref.StartedAt.Add(time.Hour)
		if end.After(now) {
			end = now
		}
		detail, exists := details[*ref]
		if !exists && !missing[*ref] {
			if s.backend == nil {
				return ExecutionTraceStagesResponse{}, tracequery.ErrUpstream
			}
			err = s.callUpstream(ctx, func() error {
				var err error
				detail, err = s.backend.Trace(ctx, ref.TraceID, ref.StartedAt, end)
				return err
			})
			if errors.Is(err, tracequery.ErrNotFound) {
				missing[*ref] = true
			} else if err != nil {
				return ExecutionTraceStagesResponse{}, err
			} else {
				if err = budget.consume(detail); err != nil {
					return ExecutionTraceStagesResponse{}, err
				}
				details[*ref] = detail
			}
		}
		if missing[*ref] {
			stage.State = "not_found"
		} else {
			value, err := tracequery.ProjectExecutionStage(detail, *ref, slot)
			if err != nil {
				return ExecutionTraceStagesResponse{}, err
			}
			stage.State, stage.Phase, stage.Outcome = "available", value.Phase, value.Outcome
			stage.Attempt, stage.Start, stage.End, stage.DurationMS = &value.Attempt, &value.Start, &value.End, &value.DurationMS
		}
		out.Stages = append(out.Stages, stage)
	}
	if err = ctx.Err(); err != nil {
		return ExecutionTraceStagesResponse{}, err
	}
	return out, nil
}
