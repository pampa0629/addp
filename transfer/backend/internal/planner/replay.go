package planner

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

var ErrReplayTargetExists = errors.New("bounded replay target already exists")
var ErrReplayRangeUnavailable = errors.New("bounded replay range is outside current retention")

type ReplayOffsetRange struct {
	Partition   string `json:"partition"`
	StartOffset int64  `json:"start_offset"`
	EndOffset   int64  `json:"end_offset"`
}

type ReplayRetentionSnapshot struct {
	Partition      string `json:"partition"`
	EarliestOffset int64  `json:"earliest_offset"`
	LatestOffset   int64  `json:"latest_offset"`
}

type ReplayProgress struct {
	Positions      map[string]int64
	RecordsRead    int64
	RecordsWritten int64
}

type ReplayResult struct {
	Positions      map[string]int64
	RecordsRead    int64
	RecordsWritten int64
}

// ReplayTargetSpec 只表达 bounded replay 允许覆盖的新目标位置。
// replay 的 source、mapping、key、数据类型和写入策略全部继承 owner task。
type ReplayTargetSpec struct {
	ParentLocator string `json:"parent_locator"`
	Name          string `json:"name"`
}

func NormalizeReplayOffsetRanges(ranges []ReplayOffsetRange) (map[string]ReplayOffsetRange, []string, error) {
	if len(ranges) == 0 {
		return nil, nil, fmt.Errorf("bounded replay requires at least one partition range")
	}
	result := make(map[string]ReplayOffsetRange, len(ranges))
	partitions := make([]string, 0, len(ranges))
	for _, replayRange := range ranges {
		partition := strings.TrimSpace(replayRange.Partition)
		if partition == "" || replayRange.StartOffset < 0 || replayRange.EndOffset <= replayRange.StartOffset {
			return nil, nil, fmt.Errorf("invalid bounded replay range for partition %q", replayRange.Partition)
		}
		if _, err := strconv.ParseInt(partition, 10, 32); err != nil {
			return nil, nil, fmt.Errorf("bounded replay partition %q must be a Kafka partition number", partition)
		}
		if _, exists := result[partition]; exists {
			return nil, nil, fmt.Errorf("bounded replay partition %q is repeated", partition)
		}
		replayRange.Partition = partition
		result[partition] = replayRange
		partitions = append(partitions, partition)
	}
	sort.Strings(partitions)
	return result, partitions, nil
}

func ValidateReplayOffsetRanges(ranges []ReplayOffsetRange) error {
	_, _, err := NormalizeReplayOffsetRanges(ranges)
	return err
}

// BuildReplayContinuousPlan 从 owner business Kafka continuous task 构建隔离 replay plan。
// 本函数不会修改原任务 spec，也不会接受除目标父节点和名称以外的覆盖。
func BuildReplayContinuousPlan(original ContinuousTaskSpec, target ReplayTargetSpec, resolver EngineResolver) (*ContinuousPlan, error) {
	if original.Runtime.RecordFailure.Mode != RecordFailureModeBlock {
		return nil, fmt.Errorf("bounded replay requires owner task runtime.record_failure.mode=%q", RecordFailureModeBlock)
	}
	if strings.TrimSpace(target.ParentLocator) == "" || strings.TrimSpace(target.Name) == "" {
		return nil, fmt.Errorf("bounded replay target requires parent_locator and name")
	}
	if sameContinuousTarget(original.Target, target) {
		return nil, fmt.Errorf("bounded replay target must differ from the owner task target")
	}

	replaySpec := ContinuousTaskSpec{
		Runtime: original.Runtime,
		Load:    original.Load,
		Source: ContinuousSourceSpec{
			Locator:        original.Source.Locator,
			Representation: original.Source.Representation,
			ChangeStream: ChangeStreamSpec{
				Envelope: original.Source.ChangeStream.Envelope,
				Encoding: original.Source.ChangeStream.Encoding,
				Key: ChangeStreamKeySpec{
					Source: original.Source.ChangeStream.Key.Source,
					Fields: append([]string(nil), original.Source.ChangeStream.Key.Fields...),
				},
				Start:         original.Source.ChangeStream.Start,
				PollBatchSize: original.Source.ChangeStream.PollBatchSize,
			},
		},
		Target: EndpointSpec{
			ParentLocator:  strings.TrimSpace(target.ParentLocator),
			Name:           strings.TrimSpace(target.Name),
			DataType:       original.Target.DataType,
			Representation: original.Target.Representation,
			Policy:         cloneReplayMap(original.Target.Policy),
		},
		Transforms: cloneReplayTransforms(original.Transforms),
	}
	return BuildContinuousPlan(replaySpec, resolver)
}

func sameContinuousTarget(original EndpointSpec, replay ReplayTargetSpec) bool {
	originalParent, originalErr := original.ParentResourceLocator()
	replayEndpoint := EndpointSpec{ParentLocator: strings.TrimSpace(replay.ParentLocator)}
	replayParent, replayErr := replayEndpoint.ParentResourceLocator()
	if originalErr != nil || replayErr != nil {
		return strings.TrimSpace(original.ParentLocator) == strings.TrimSpace(replay.ParentLocator) &&
			strings.TrimSpace(original.Name) == strings.TrimSpace(replay.Name)
	}
	return originalParent.EngineID == replayParent.EngineID && originalParent.Type == replayParent.Type &&
		reflect.DeepEqual(originalParent.Path, replayParent.Path) && strings.TrimSpace(original.Name) == strings.TrimSpace(replay.Name)
}

func cloneReplayTransforms(source []TransformSpec) []TransformSpec {
	result := make([]TransformSpec, len(source))
	for i, transform := range source {
		result[i] = transform
		result[i].Config = cloneReplayMap(transform.Config)
		result[i].Fields = append([]FieldMappingSpec(nil), transform.Fields...)
	}
	return result
}

func cloneReplayMap(source map[string]interface{}) map[string]interface{} {
	if source == nil {
		return nil
	}
	result := make(map[string]interface{}, len(source))
	for key, value := range source {
		switch typed := value.(type) {
		case map[string]interface{}:
			result[key] = cloneReplayMap(typed)
		case []interface{}:
			result[key] = append([]interface{}(nil), typed...)
		case []string:
			result[key] = append([]string(nil), typed...)
		default:
			result[key] = value
		}
	}
	return result
}
