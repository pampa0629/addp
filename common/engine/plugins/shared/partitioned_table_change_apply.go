package shared

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/addp/common/engine/plugin"
)

type PositionedTableChange struct {
	NextOffset int64
	Operation  string
	Key        string
	Row        map[string]interface{}
}

func FilterAndCoalesceTableChanges(batch *plugin.PartitionedTableChangeBatch, keys []string, startOffset, ledgerOffset, endOffset int64) ([]PositionedTableChange, int, error) {
	if endOffset > startOffset && len(batch.Changes) == 0 {
		return nil, 0, fmt.Errorf("table change batch cannot advance from %d to %d without changes", startOffset, endOffset)
	}
	byKey := make(map[string]PositionedTableChange)
	skipped := 0
	previousOffset := int64(-1)
	for index, change := range batch.Changes {
		if change.Operation != plugin.TableChangeOperationUpsert && change.Operation != plugin.TableChangeOperationDelete && change.Operation != plugin.TableChangeOperationSkip {
			return nil, 0, fmt.Errorf("unsupported table change operation %q at index %d", change.Operation, index)
		}
		if change.Operation == plugin.TableChangeOperationSkip && len(change.Row) != 0 {
			return nil, 0, fmt.Errorf("skip operation must not contain a row at index %d", index)
		}
		nextOffset, err := KafkaNextOffset(change.Position, batch.Partition)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid change position at index %d: %w", index, err)
		}
		if previousOffset >= 0 && nextOffset <= previousOffset {
			return nil, 0, fmt.Errorf("table change positions must be strictly increasing: %d after %d", nextOffset, previousOffset)
		}
		previousOffset = nextOffset
		if nextOffset <= startOffset {
			return nil, 0, fmt.Errorf("table change next_offset %d must be after batch start %d", nextOffset, startOffset)
		}
		if nextOffset > endOffset {
			return nil, 0, fmt.Errorf("table change next_offset %d exceeds batch end %d", nextOffset, endOffset)
		}
		if nextOffset <= ledgerOffset {
			skipped++
			continue
		}
		if change.Operation == plugin.TableChangeOperationSkip {
			skipped++
			continue
		}
		key, err := TableChangeKey(change.Row, keys)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid table change key at index %d: %w", index, err)
		}
		if _, exists := byKey[key]; exists {
			skipped++
		}
		byKey[key] = PositionedTableChange{NextOffset: nextOffset, Operation: change.Operation, Key: key, Row: change.Row}
	}
	if len(batch.Changes) > 0 && previousOffset != endOffset {
		return nil, 0, fmt.Errorf("last table change next_offset %d does not match batch end %d", previousOffset, endOffset)
	}
	changes := make([]PositionedTableChange, 0, len(byKey))
	for _, change := range byKey {
		changes = append(changes, change)
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].NextOffset == changes[j].NextOffset {
			return changes[i].Key < changes[j].Key
		}
		return changes[i].NextOffset < changes[j].NextOffset
	})
	return changes, skipped, nil
}

func TableChangeKey(row map[string]interface{}, keys []string) (string, error) {
	values := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			return "", fmt.Errorf("missing non-null key field %q", key)
		}
		values = append(values, value)
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode key: %w", err)
	}
	return string(encoded), nil
}

func KafkaNextOffset(position plugin.ChangeStreamPosition, partition string) (int64, error) {
	if position.Type != plugin.ChangeStreamPositionTypeKafkaOffset || position.Version != plugin.ChangeStreamPositionVersionV1 {
		return 0, fmt.Errorf("unsupported position %s/%s", position.Type, position.Version)
	}
	if position.Partition != partition {
		return 0, fmt.Errorf("position partition %q does not match batch partition %q", position.Partition, partition)
	}
	raw, ok := position.Values["next_offset"]
	if !ok {
		return 0, fmt.Errorf("position requires next_offset")
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid next_offset %q", raw)
	}
	return value, nil
}

func KafkaOffsetPosition(partition string, nextOffset int64) plugin.ChangeStreamPosition {
	return plugin.ChangeStreamPosition{
		Type:      plugin.ChangeStreamPositionTypeKafkaOffset,
		Version:   plugin.ChangeStreamPositionVersionV1,
		Partition: partition,
		Values:    map[string]string{"next_offset": strconv.FormatInt(nextOffset, 10)},
	}
}
