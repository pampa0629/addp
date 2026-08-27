package continuous

import (
	"context"
	"fmt"
	"strings"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/planner"
)

var ErrReplayTargetExists = planner.ErrReplayTargetExists
var ErrReplayRangeUnavailable = planner.ErrReplayRangeUnavailable

type ReplayOffsetRange = planner.ReplayOffsetRange
type ReplayProgress = planner.ReplayProgress
type ReplayRetentionSnapshot = planner.ReplayRetentionSnapshot
type ReplayResult = planner.ReplayResult

type BoundedReplayRunner struct {
	GetPlugin          PluginGetter
	PollTimeout        time.Duration
	MaxBytes           int
	AssertTargetAbsent func(context.Context, *planner.ContinuousTargetPlan) error
	RecordProgress     func(context.Context, ReplayProgress) error
}

// SnapshotRetention 记录请求时源 topic 的真实 retention 边界，并验证全部请求范围。
func (r *BoundedReplayRunner) SnapshotRetention(
	ctx context.Context,
	plan *planner.ContinuousPlan,
	ranges []ReplayOffsetRange,
	executionApplyIdentity string,
) ([]ReplayRetentionSnapshot, error) {
	if plan == nil || plan.Envelope != planner.ContinuousEnvelopeRecord || plan.RecordFailureMode != planner.RecordFailureModeBlock {
		return nil, fmt.Errorf("bounded replay requires a business Kafka record/json plan with blocking record policy")
	}
	rangesByPartition, orderedPartitions, err := planner.NormalizeReplayOffsetRanges(ranges)
	if err != nil {
		return nil, err
	}
	reader, err := r.openReplaySource(ctx, plan, rangesByPartition, executionApplyIdentity)
	if err != nil {
		return nil, err
	}
	defer reader.Close(context.Background())
	retained, err := reader.PositionRanges(ctx)
	if err != nil {
		return nil, fmt.Errorf("read bounded replay retention ranges: %w", err)
	}
	if err := validateReplayRetention(rangesByPartition, retained); err != nil {
		return nil, err
	}
	retainedByPartition := make(map[string]engineplugin.ChangeStreamPositionRange, len(retained))
	for _, value := range retained {
		retainedByPartition[value.Partition] = value
	}
	snapshot := make([]ReplayRetentionSnapshot, 0, len(orderedPartitions))
	for _, partition := range orderedPartitions {
		value := retainedByPartition[partition]
		earliest, err := kafkaNextOffset(value.Earliest)
		if err != nil {
			return nil, err
		}
		latest, err := kafkaNextOffset(value.Latest)
		if err != nil {
			return nil, err
		}
		snapshot = append(snapshot, ReplayRetentionSnapshot{Partition: partition, EarliestOffset: earliest, LatestOffset: latest})
	}
	return snapshot, nil
}

// Run 从原业务 Kafka 的显式半开 offset ranges 读取，并写入 execution-scoped 新目标。
// 本运行器不接收 task sync state、runtime lease 或主 apply identity，因此无法读写主水位。
func (r *BoundedReplayRunner) Run(
	ctx context.Context,
	plan *planner.ContinuousPlan,
	ranges []ReplayOffsetRange,
	executionApplyIdentity string,
) (*ReplayResult, error) {
	if plan == nil || plan.Envelope != planner.ContinuousEnvelopeRecord || plan.RecordFailureMode != planner.RecordFailureModeBlock {
		return nil, fmt.Errorf("bounded replay requires a business Kafka record/json plan with blocking record policy")
	}
	rangesByPartition, orderedPartitions, err := planner.NormalizeReplayOffsetRanges(ranges)
	if err != nil {
		return nil, err
	}
	getPlugin := r.pluginGetter()
	targetPlugin, err := getPlugin(plan.TargetType)
	if err != nil {
		return nil, err
	}
	target, ok := targetPlugin.(engineplugin.PartitionedTableChangeApplyProvider)
	if !ok {
		return nil, fmt.Errorf("replay target engine %q does not implement PartitionedTableChangeApplyProvider", plan.TargetType)
	}

	positions := make(map[string]int64, len(rangesByPartition))
	for partition, replayRange := range rangesByPartition {
		positions[partition] = replayRange.StartOffset
	}
	reader, err := r.openReplaySource(ctx, plan, rangesByPartition, executionApplyIdentity)
	if err != nil {
		return nil, fmt.Errorf("open bounded replay source: %w", err)
	}
	defer reader.Close(context.Background())
	retained, err := reader.PositionRanges(ctx)
	if err != nil {
		return nil, fmt.Errorf("read bounded replay retention ranges: %w", err)
	}
	if err := validateReplayRetention(rangesByPartition, retained); err != nil {
		return nil, err
	}
	if r.AssertTargetAbsent == nil {
		return nil, fmt.Errorf("bounded replay target absence validator is required")
	}
	if err := r.AssertTargetAbsent(ctx, &plan.Target); err != nil {
		return nil, err
	}
	applyOptions := engineplugin.PartitionedTableChangeApplyOptions{
		ApplyIdentity: executionApplyIdentity, SourceIdentity: plan.Source.SourceIdentity,
		Fields: plan.Target.Fields, SpatialInfo: plan.Target.SpatialInfo, Keys: plan.Target.Keys,
		RequireTargetAbsent: true,
	}
	if err := target.PreparePartitionedTableChangeApply(ctx, plan.Target.ConnInfo, plan.Target.Path, applyOptions); err != nil {
		return nil, fmt.Errorf("prepare bounded replay target: %w", err)
	}

	result := &ReplayResult{Positions: positions}
	for !replayComplete(result.Positions, rangesByPartition) {
		batch, err := reader.Poll(ctx, plan.Source.PollBatchSize)
		if err != nil {
			return nil, fmt.Errorf("poll bounded replay source: %w", err)
		}
		if batch == nil || len(batch.Records) == 0 {
			assigned := make(map[string]bool)
			for _, partition := range reader.Assignments() {
				assigned[partition] = true
			}
			for _, retainedRange := range retained {
				replayRange, requested := rangesByPartition[retainedRange.Partition]
				if !requested || !assigned[retainedRange.Partition] || result.Positions[retainedRange.Partition] >= replayRange.EndOffset {
					continue
				}
				latest, rangeErr := kafkaNextOffset(retainedRange.Latest)
				if rangeErr == nil && latest >= replayRange.EndOffset {
					result.Positions[retainedRange.Partition] = replayRange.EndOffset
				}
			}
			continue
		}
		groups := groupRecordsByPartition(batch.Records)
		for _, group := range groups {
			replayRange, requested := rangesByPartition[group.partition]
			if !requested || result.Positions[group.partition] >= replayRange.EndOffset {
				continue
			}
			selected := make([]engineplugin.ChangeRecord, 0, len(group.records))
			reachedEnd := false
			for _, record := range group.records {
				if record.Offset < result.Positions[group.partition] {
					continue
				}
				if record.Offset >= replayRange.EndOffset {
					reachedEnd = true
					break
				}
				selected = append(selected, record)
			}
			if len(selected) == 0 {
				if reachedEnd {
					result.Positions[group.partition] = replayRange.EndOffset
				}
				continue
			}
			start := kafkaOffsetPosition(group.partition, result.Positions[group.partition])
			changes, err := mapPartitionRecords(selected, start, plan)
			if err != nil {
				return nil, fmt.Errorf("map bounded replay partition %q: %w", group.partition, err)
			}
			result.RecordsRead += int64(len(selected))
			if len(changes) == 0 {
				continue
			}
			changeBatch := &engineplugin.PartitionedTableChangeBatch{
				Partition: group.partition, StartPosition: start,
				EndPosition: changes[len(changes)-1].Position, Changes: changes,
			}
			applied, err := target.ApplyPartitionedTableChanges(ctx, plan.Target.ConnInfo, plan.Target.Path, changeBatch, applyOptions)
			if err != nil {
				return nil, fmt.Errorf("apply bounded replay partition %q: %w", group.partition, err)
			}
			if applied == nil {
				return nil, fmt.Errorf("apply bounded replay partition %q returned nil result", group.partition)
			}
			nextOffset, err := kafkaNextOffset(applied.Position)
			if err != nil {
				return nil, err
			}
			result.Positions[group.partition] = nextOffset
			if reachedEnd {
				result.Positions[group.partition] = replayRange.EndOffset
			}
			result.RecordsWritten += int64(applied.AppliedRecords)
			if r.RecordProgress != nil {
				if err := r.RecordProgress(ctx, ReplayProgress{
					Positions: cloneReplayPositions(result.Positions), RecordsRead: result.RecordsRead, RecordsWritten: result.RecordsWritten,
				}); err != nil {
					return nil, fmt.Errorf("record bounded replay progress: %w", err)
				}
			}
		}
	}
	for _, partition := range orderedPartitions {
		result.Positions[partition] = rangesByPartition[partition].EndOffset
	}
	return result, nil
}

func (r *BoundedReplayRunner) openReplaySource(ctx context.Context, plan *planner.ContinuousPlan, ranges map[string]ReplayOffsetRange, executionApplyIdentity string) (engineplugin.ChangeStreamReader, error) {
	if strings.TrimSpace(executionApplyIdentity) == "" {
		return nil, fmt.Errorf("bounded replay execution apply identity is required")
	}
	sourcePlugin, err := r.pluginGetter()(plan.SourceType)
	if err != nil {
		return nil, err
	}
	source, ok := sourcePlugin.(engineplugin.ChangeStreamReaderProvider)
	if !ok {
		return nil, fmt.Errorf("replay source engine %q does not implement ChangeStreamReaderProvider", plan.SourceType)
	}
	starts := make(map[string]engineplugin.ChangeStreamPosition, len(ranges))
	for partition, replayRange := range ranges {
		starts[partition] = kafkaOffsetPosition(partition, replayRange.StartOffset)
	}
	pollTimeout := r.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 5 * time.Second
	}
	reader, err := source.OpenChangeStream(ctx, plan.Source.ConnInfo, plan.Source.Path, engineplugin.ChangeStreamReadOptions{
		ConsumerGroup: "__addp_replay." + executionApplyIdentity, CommittedPositions: starts,
		InitialPosition: engineplugin.ChangeStreamInitialLatest, PollTimeout: pollTimeout, MaxBytes: r.MaxBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("open bounded replay source: %w", err)
	}
	return reader, nil
}

func (r *BoundedReplayRunner) pluginGetter() PluginGetter {
	if r.GetPlugin != nil {
		return r.GetPlugin
	}
	return engineplugin.Get
}

// NewReplayTargetAbsenceValidator 使用目标引擎真实 catalog 校验新表不存在。
// runner 会在 retention 验证后调用它；PostgreSQL prepare 还会以非 IF NOT EXISTS DDL 防住并发竞态。
func NewReplayTargetAbsenceValidator(getPlugin PluginGetter) func(context.Context, *planner.ContinuousTargetPlan) error {
	return func(ctx context.Context, target *planner.ContinuousTargetPlan) error {
		if target == nil || len(target.Path.Segments) < 3 {
			return fmt.Errorf("bounded replay target path must identify a PostgreSQL table")
		}
		if getPlugin == nil {
			getPlugin = engineplugin.Get
		}
		pluginValue, err := getPlugin("postgresql")
		if err != nil {
			return err
		}
		catalog, ok := pluginValue.(engineplugin.EngineCatalogProvider)
		if !ok {
			return fmt.Errorf("replay target PostgreSQL engine does not implement EngineCatalogProvider")
		}
		parent := target.Path
		parent.Segments = append([]engineplugin.EngineCatalogSegment(nil), target.Path.Segments[:len(target.Path.Segments)-1]...)
		tableName := target.Path.Segments[len(target.Path.Segments)-1].Name
		entries, err := catalog.ListChildren(ctx, target.ConnInfo, parent, engineplugin.ListOptions{})
		if err != nil {
			return fmt.Errorf("list bounded replay target namespace: %w", err)
		}
		for _, entry := range entries {
			if entry.Name == tableName {
				return fmt.Errorf("%w: %s", ErrReplayTargetExists, target.Path.StringPath())
			}
		}
		return nil
	}
}

func validateReplayRetention(requested map[string]ReplayOffsetRange, retained []engineplugin.ChangeStreamPositionRange) error {
	byPartition := make(map[string]engineplugin.ChangeStreamPositionRange, len(retained))
	for _, value := range retained {
		byPartition[value.Partition] = value
	}
	for partition, replayRange := range requested {
		available, ok := byPartition[partition]
		if !ok {
			return fmt.Errorf("%w: partition %q does not exist", planner.ErrReplayRangeUnavailable, partition)
		}
		earliest, err := kafkaNextOffset(available.Earliest)
		if err != nil {
			return err
		}
		latest, err := kafkaNextOffset(available.Latest)
		if err != nil {
			return err
		}
		if replayRange.StartOffset < earliest || replayRange.EndOffset > latest {
			return fmt.Errorf("%w: range [%d,%d) for partition %s is outside retained range [%d,%d]", planner.ErrReplayRangeUnavailable, replayRange.StartOffset, replayRange.EndOffset, partition, earliest, latest)
		}
	}
	return nil
}

func replayComplete(positions map[string]int64, ranges map[string]ReplayOffsetRange) bool {
	for partition, replayRange := range ranges {
		if positions[partition] < replayRange.EndOffset {
			return false
		}
	}
	return true
}

func cloneReplayPositions(source map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
