package continuous

import (
	"context"
	"fmt"

	"github.com/addp/transfer/internal/planner"
)

// ReplayRuntime 把 bounded replay 数据面作为可注入运行时暴露给 service 层。
// service 只依赖 planner 契约，不反向依赖 continuous 包。
type ReplayRuntime struct {
	Runner BoundedReplayRunner
}

func NewReplayRuntime(runner BoundedReplayRunner) *ReplayRuntime {
	return &ReplayRuntime{Runner: runner}
}

func (r *ReplayRuntime) Prepare(
	ctx context.Context,
	plan *planner.ContinuousPlan,
	ranges []planner.ReplayOffsetRange,
	executionApplyIdentity string,
) ([]planner.ReplayRetentionSnapshot, error) {
	snapshot, err := r.Runner.SnapshotRetention(ctx, plan, ranges, executionApplyIdentity)
	if err != nil {
		return nil, err
	}
	if r.Runner.AssertTargetAbsent == nil {
		return nil, fmt.Errorf("bounded replay target absence validator is required")
	}
	if err := r.Runner.AssertTargetAbsent(ctx, &plan.Target); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (r *ReplayRuntime) Run(
	ctx context.Context,
	plan *planner.ContinuousPlan,
	ranges []planner.ReplayOffsetRange,
	executionApplyIdentity string,
	recordProgress func(context.Context, planner.ReplayProgress) error,
) (*planner.ReplayResult, error) {
	runner := r.Runner
	runner.RecordProgress = recordProgress
	return runner.Run(ctx, plan, ranges, executionApplyIdentity)
}
