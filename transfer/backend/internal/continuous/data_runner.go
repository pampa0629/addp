package continuous

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/google/uuid"
)

type ContinuousStateStore interface {
	List(ctx context.Context, taskID uint, sourceIdentity string) ([]models.SyncState, error)
	ClaimContinuousPartition(ctx context.Context, taskID uint, sourceIdentity, partition, positionType, positionVersion, owner string, fencingToken uint64) (*models.SyncState, error)
	CommitContinuousPosition(ctx context.Context, id, taskID uint, expectedVersion, fencingToken uint64, owner string, position models.JSONMap, executionID string) error
}

type ContinuousProgressStore interface {
	RecordProgress(ctx context.Context, claim repository.RuntimeLeaseClaim, progress repository.ContinuousProgress) error
}

type PluginGetter func(engineType string) (engineplugin.EnginePlugin, error)

type DataSessionRunner struct {
	Resolver    planner.EngineResolver
	States      ContinuousStateStore
	Progress    ContinuousProgressStore
	GetPlugin   PluginGetter
	PollTimeout time.Duration
	MaxBytes    int
}

func (r *DataSessionRunner) Run(ctx context.Context, claim repository.RuntimeLeaseClaim) error {
	if r == nil || r.Resolver == nil || r.States == nil || r.Progress == nil {
		return fmt.Errorf("continuous data runner dependencies are required")
	}
	spec, err := planner.ParseContinuousTaskSpec(claim.Task.Config)
	if err != nil {
		return fmt.Errorf("parse continuous task: %w", err)
	}
	plan, err := planner.BuildContinuousPlan(spec, r.Resolver)
	if err != nil {
		return fmt.Errorf("build continuous plan: %w", err)
	}
	getPlugin := r.GetPlugin
	if getPlugin == nil {
		getPlugin = engineplugin.Get
	}
	sourcePlugin, err := getPlugin(plan.SourceType)
	if err != nil {
		return err
	}
	source, ok := sourcePlugin.(engineplugin.ChangeStreamReaderProvider)
	if !ok {
		return fmt.Errorf("source engine %q does not implement ChangeStreamReaderProvider", plan.SourceType)
	}
	targetPlugin, err := getPlugin(plan.TargetType)
	if err != nil {
		return err
	}
	target, ok := targetPlugin.(engineplugin.PartitionedTableChangeApplyProvider)
	if !ok {
		return fmt.Errorf("target engine %q does not implement PartitionedTableChangeApplyProvider", plan.TargetType)
	}
	applyOptions := engineplugin.PartitionedTableChangeApplyOptions{
		ApplyIdentity: claim.Task.ApplyIdentity, SourceIdentity: plan.Source.SourceIdentity,
		Fields: plan.Target.Fields, Keys: plan.Target.Keys,
	}
	if err := target.PreparePartitionedTableChangeApply(ctx, plan.Target.ConnInfo, plan.Target.Path, applyOptions); err != nil {
		return fmt.Errorf("prepare continuous target: %w", err)
	}
	committed, err := r.committedPositions(ctx, claim.Task.ID, plan.Source.SourceIdentity)
	if err != nil {
		return err
	}
	pollTimeout := r.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 5 * time.Second
	}
	reader, err := source.OpenChangeStream(ctx, plan.Source.ConnInfo, plan.Source.Path, engineplugin.ChangeStreamReadOptions{
		ConsumerGroup: "addp-transfer-" + claim.Task.ApplyIdentity, CommittedPositions: committed,
		InitialPosition: plan.Source.InitialPosition, PollTimeout: pollTimeout, MaxBytes: r.MaxBytes,
	})
	if err != nil {
		return fmt.Errorf("open continuous source: %w", err)
	}
	defer reader.Close(context.Background())

	partitionStates := map[string]*models.SyncState{}
	var recordsRead, recordsWritten int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, err := reader.Poll(ctx, plan.Source.PollBatchSize)
		if err != nil {
			return fmt.Errorf("poll continuous source: %w", err)
		}
		if batch == nil || len(batch.Records) == 0 {
			continue
		}
		groups := groupRecordsByPartition(batch.Records)
		for _, group := range groups {
			if err := ctx.Err(); err != nil {
				return err
			}
			recordsRead += int64(len(group.records))
			state := partitionStates[group.partition]
			if state == nil {
				state, err = r.States.ClaimContinuousPartition(
					ctx, claim.Task.ID, plan.Source.SourceIdentity, group.partition,
					engineplugin.ChangeStreamPositionTypeKafkaOffset, engineplugin.ChangeStreamPositionVersionV1,
					claim.Lease.OwnerInstanceID, claim.Lease.FencingToken,
				)
				if err != nil {
					return fmt.Errorf("claim continuous partition %q: %w", group.partition, err)
				}
				partitionStates[group.partition] = state
			}
			start, hasStart, err := syncStatePosition(state)
			if err != nil {
				return err
			}
			if !hasStart {
				start = kafkaOffsetPosition(group.partition, group.records[0].Offset)
			}
			changes, err := mapPartitionRecords(group.records, start, plan)
			if err != nil {
				return fmt.Errorf("map continuous partition %q: %w", group.partition, err)
			}
			if len(changes) == 0 {
				if err := r.Progress.RecordProgress(ctx, claim, repository.ContinuousProgress{
					RecordsRead: recordsRead, RecordsWritten: recordsWritten, Partition: group.partition,
					Position: positionJSON(start), CommittedAt: time.Now(),
				}); err != nil {
					return fmt.Errorf("record continuous progress: %w", err)
				}
				continue
			}
			changeBatch := &engineplugin.PartitionedTableChangeBatch{
				Partition: group.partition, StartPosition: start,
				EndPosition: changes[len(changes)-1].Position, Changes: changes,
			}
			result, err := target.ApplyPartitionedTableChanges(ctx, plan.Target.ConnInfo, plan.Target.Path, changeBatch, applyOptions)
			if err != nil {
				return fmt.Errorf("apply continuous partition %q: %w", group.partition, err)
			}
			if result == nil {
				return fmt.Errorf("apply continuous partition %q returned nil result", group.partition)
			}
			if err := r.States.CommitContinuousPosition(
				ctx, state.ID, claim.Task.ID, state.StateVersion, claim.Lease.FencingToken,
				claim.Lease.OwnerInstanceID, positionJSON(result.Position), claim.Execution.ExecutionID,
			); err != nil {
				return fmt.Errorf("commit continuous partition %q: %w", group.partition, err)
			}
			state.StateVersion++
			state.Position = positionJSON(result.Position)
			recordsWritten += int64(result.AppliedRecords)
			if err := r.Progress.RecordProgress(ctx, claim, repository.ContinuousProgress{
				RecordsRead: recordsRead, RecordsWritten: recordsWritten, Partition: group.partition,
				Position: state.Position, CommittedAt: time.Now(),
			}); err != nil {
				return fmt.Errorf("record continuous progress: %w", err)
			}
		}
	}
}

func (r *DataSessionRunner) committedPositions(ctx context.Context, taskID uint, sourceIdentity string) (map[string]engineplugin.ChangeStreamPosition, error) {
	states, err := r.States.List(ctx, taskID, sourceIdentity)
	if err != nil {
		return nil, fmt.Errorf("list continuous committed positions: %w", err)
	}
	positions := make(map[string]engineplugin.ChangeStreamPosition, len(states))
	for i := range states {
		position, ok, err := syncStatePosition(&states[i])
		if err != nil {
			return nil, err
		}
		if ok {
			positions[states[i].Partition] = position
		}
	}
	return positions, nil
}

type partitionRecordGroup struct {
	partition string
	records   []engineplugin.ChangeRecord
}

func groupRecordsByPartition(records []engineplugin.ChangeRecord) []partitionRecordGroup {
	groups := make([]partitionRecordGroup, 0)
	indexes := map[string]int{}
	for _, record := range records {
		index, ok := indexes[record.Partition]
		if !ok {
			index = len(groups)
			indexes[record.Partition] = index
			groups = append(groups, partitionRecordGroup{partition: record.Partition})
		}
		groups[index].records = append(groups[index].records, record)
	}
	return groups
}

func mapPartitionRecords(records []engineplugin.ChangeRecord, start engineplugin.ChangeStreamPosition, plan *planner.ContinuousPlan) ([]engineplugin.PartitionedTableChange, error) {
	if len(records) == 0 {
		return nil, nil
	}
	startOffset, err := kafkaNextOffset(start)
	if err != nil {
		return nil, err
	}
	changes := make([]engineplugin.PartitionedTableChange, 0, len(records))
	for _, record := range records {
		nextOffset, err := kafkaNextOffset(record.Position)
		if err != nil {
			return nil, err
		}
		if nextOffset <= startOffset {
			continue
		}
		row, err := decodeAndMapRecord(record, plan)
		if err != nil {
			return nil, fmt.Errorf("offset %d: %w", record.Offset, err)
		}
		changes = append(changes, engineplugin.PartitionedTableChange{
			Operation: engineplugin.TableChangeOperationUpsert, Position: record.Position, Row: row,
		})
	}
	return changes, nil
}

func decodeAndMapRecord(record engineplugin.ChangeRecord, plan *planner.ContinuousPlan) (map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(record.Value))
	decoder.UseNumber()
	var source map[string]interface{}
	if err := decoder.Decode(&source); err != nil {
		return nil, fmt.Errorf("value must be a JSON object: %w", err)
	}
	if source == nil {
		return nil, fmt.Errorf("value must be a JSON object")
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("value must contain exactly one JSON object")
	}
	allowed := make(map[string]bool, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		allowed[mapping.Source] = true
	}
	for field := range source {
		if !allowed[field] {
			return nil, fmt.Errorf("unknown source field %q", field)
		}
	}
	for _, key := range plan.SourceKeys {
		value, ok := source[key]
		if !ok || value == nil {
			return nil, fmt.Errorf("missing non-null source key field %q", key)
		}
	}
	row := make(map[string]interface{}, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		value, ok := source[mapping.Source]
		if !ok || value == nil {
			if mapping.Default != nil {
				value = mapping.Default
			} else if mapping.Nullable {
				row[mapping.Target] = nil
				continue
			} else {
				return nil, fmt.Errorf("missing required source field %q", mapping.Source)
			}
		}
		converted, err := coerceContinuousValue(value, mapping.Type)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", mapping.Source, err)
		}
		row[mapping.Target] = converted
	}
	for _, key := range plan.Target.Keys {
		if value, ok := row[key]; !ok || value == nil {
			return nil, fmt.Errorf("mapped target key field %q is null", key)
		}
	}
	return row, nil
}

func coerceContinuousValue(value interface{}, fieldType datatype.FieldType) (interface{}, error) {
	switch fieldType {
	case datatype.FieldTypeString:
		if typed, ok := value.(string); ok {
			return typed, nil
		}
	case datatype.FieldTypeBool:
		if typed, ok := value.(bool); ok {
			return typed, nil
		}
	case datatype.FieldTypeInt:
		integer, err := continuousInt64(value)
		if err == nil && integer >= math.MinInt32 && integer <= math.MaxInt32 {
			return int32(integer), nil
		}
		if err == nil {
			err = fmt.Errorf("integer is outside int32 range")
		}
		return nil, err
	case datatype.FieldTypeBigInt:
		return continuousInt64(value)
	case datatype.FieldTypeFloat, datatype.FieldTypeDouble:
		return continuousFloat64(value)
	case datatype.FieldTypeDecimal:
		switch typed := value.(type) {
		case json.Number:
			if _, err := strconv.ParseFloat(typed.String(), 64); err == nil {
				return typed.String(), nil
			}
		case float64:
			if !math.IsNaN(typed) && !math.IsInf(typed, 0) {
				return strconv.FormatFloat(typed, 'g', -1, 64), nil
			}
		}
	case datatype.FieldTypeDate:
		if typed, ok := value.(string); ok {
			if _, err := time.Parse("2006-01-02", typed); err == nil {
				return typed, nil
			}
		}
	case datatype.FieldTypeTime:
		if typed, ok := value.(string); ok {
			for _, layout := range []string{"15:04:05", "15:04:05.999999999"} {
				if _, err := time.Parse(layout, typed); err == nil {
					return typed, nil
				}
			}
		}
	case datatype.FieldTypeTimestamp:
		if typed, ok := value.(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
				return parsed, nil
			}
		}
	case datatype.FieldTypeUUID:
		if typed, ok := value.(string); ok {
			if _, err := uuid.Parse(typed); err == nil {
				return typed, nil
			}
		}
	case datatype.FieldTypeJSON:
		encoded, err := json.Marshal(value)
		if err == nil {
			return string(encoded), nil
		}
	}
	return nil, fmt.Errorf("value %T is incompatible with target_type %q", value, fieldType)
}

func continuousInt64(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Int64()
	case float64:
		if math.Trunc(typed) == typed && typed >= math.MinInt64 && typed <= math.MaxInt64 {
			return int64(typed), nil
		}
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	}
	return 0, fmt.Errorf("value %T is not an integer", value)
}

func continuousFloat64(value interface{}) (float64, error) {
	var result float64
	var err error
	switch typed := value.(type) {
	case json.Number:
		result, err = typed.Float64()
	case float64:
		result = typed
	case int:
		result = float64(typed)
	case int64:
		result = float64(typed)
	default:
		err = fmt.Errorf("value %T is not numeric", value)
	}
	if err != nil {
		return 0, err
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, fmt.Errorf("numeric value must be finite")
	}
	return result, nil
}

func syncStatePosition(state *models.SyncState) (engineplugin.ChangeStreamPosition, bool, error) {
	if state == nil || len(state.Position) == 0 {
		return engineplugin.ChangeStreamPosition{}, false, nil
	}
	encoded, err := json.Marshal(state.Position)
	if err != nil {
		return engineplugin.ChangeStreamPosition{}, false, err
	}
	var position engineplugin.ChangeStreamPosition
	if err := json.Unmarshal(encoded, &position); err != nil {
		return engineplugin.ChangeStreamPosition{}, false, fmt.Errorf("decode sync state partition %q position: %w", state.Partition, err)
	}
	if _, err := kafkaNextOffset(position); err != nil {
		return engineplugin.ChangeStreamPosition{}, false, fmt.Errorf("invalid sync state partition %q position: %w", state.Partition, err)
	}
	if position.Partition != state.Partition {
		return engineplugin.ChangeStreamPosition{}, false, fmt.Errorf("sync state position partition %q does not match row partition %q", position.Partition, state.Partition)
	}
	return position, true, nil
}

func positionJSON(position engineplugin.ChangeStreamPosition) models.JSONMap {
	values := make(map[string]string, len(position.Values))
	for key, value := range position.Values {
		values[key] = value
	}
	return models.JSONMap{
		"type": position.Type, "version": position.Version, "partition": position.Partition, "values": values,
	}
}

func kafkaNextOffset(position engineplugin.ChangeStreamPosition) (int64, error) {
	if position.Type != engineplugin.ChangeStreamPositionTypeKafkaOffset || position.Version != engineplugin.ChangeStreamPositionVersionV1 {
		return 0, fmt.Errorf("unsupported position %s/%s", position.Type, position.Version)
	}
	if strings.TrimSpace(position.Partition) == "" {
		return 0, fmt.Errorf("position partition is required")
	}
	next, err := strconv.ParseInt(position.Values["next_offset"], 10, 64)
	if err != nil || next < 0 {
		return 0, fmt.Errorf("position requires non-negative next_offset")
	}
	return next, nil
}

func kafkaOffsetPosition(partition string, nextOffset int64) engineplugin.ChangeStreamPosition {
	return engineplugin.ChangeStreamPosition{
		Type: engineplugin.ChangeStreamPositionTypeKafkaOffset, Version: engineplugin.ChangeStreamPositionVersionV1,
		Partition: partition, Values: map[string]string{"next_offset": strconv.FormatInt(nextOffset, 10)},
	}
}
