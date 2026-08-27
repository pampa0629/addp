package preview

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
)

const (
	eventStreamTopicPreviewKind     = "event_stream_topic"
	eventStreamTopicPreviewMaxBytes = 4 * 1024 * 1024
	eventStreamTopicPollTimeout     = 2 * time.Second
)

var eventStreamTopicPreviewColumns = []string{"partition", "offset", "timestamp", "key", "value", "headers"}

// EventStreamTopicPreviewProvider returns a bounded tail sample without
// committing provider positions or sharing a Transfer consumer group.
type EventStreamTopicPreviewProvider struct{}

func NewEventStreamTopicPreviewProvider() PreviewProvider {
	return &EventStreamTopicPreviewProvider{}
}

func (p *EventStreamTopicPreviewProvider) Name() string { return "builtin:event-stream-topic" }

func (p *EventStreamTopicPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	if req == nil || req.Engine == nil {
		return nil, fmt.Errorf("invalid preview request")
	}
	enginePlugin, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}
	factsProvider, ok := enginePlugin.(plugin.EngineCatalogFactsProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement EngineCatalogFactsProvider", req.Engine.EngineType)
	}
	readerProvider, ok := enginePlugin.(plugin.ChangeStreamReaderProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement ChangeStreamReaderProvider", req.Engine.EngineType)
	}

	facts, err := factsProvider.DescribeEngineCatalogFacts(
		ctx,
		plugin.ConnectionInfo(req.Engine.ConnectionInfo),
		req.ProviderPath,
		plugin.EngineCatalogFactsOptions{IncludeStatistics: true},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to describe event stream topic: %w", err)
	}
	if facts == nil || facts.Topic == nil {
		return nil, fmt.Errorf("event stream topic facts are missing")
	}

	pageSize := normalizeEventStreamTopicPreviewSize(req.PageSize)
	reader, err := readerProvider.OpenChangeStream(ctx, plugin.ConnectionInfo(req.Engine.ConnectionInfo), req.ProviderPath, plugin.ChangeStreamReadOptions{
		ConsumerGroup:      "addp-manager-preview-" + uuid.NewString(),
		CommittedPositions: eventStreamTopicTailPositions(facts.Topic, pageSize),
		InitialPosition:    plugin.ChangeStreamInitialEarliest,
		PollTimeout:        eventStreamTopicPollTimeout,
		MaxBytes:           eventStreamTopicPreviewMaxBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open event stream topic preview: %w", err)
	}
	defer func() { _ = reader.Close(context.Background()) }()

	batch, err := reader.Poll(ctx, pageSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read event stream topic preview: %w", err)
	}
	rows := eventStreamTopicPreviewRows(batch)
	return &models.TablePreview{
		Mode:            PreviewModeTable,
		PreviewKind:     eventStreamTopicPreviewKind,
		Columns:         append([]string(nil), eventStreamTopicPreviewColumns...),
		Rows:            rows,
		Total:           len(rows),
		Page:            1,
		PageSize:        pageSize,
		GeometryColumns: []string{},
		EngineID:        req.Engine.ID,
		Table:           eventStreamTopicName(req.ProviderPath),
		EngineType:      req.Engine.EngineType,
	}, nil
}

func normalizeEventStreamTopicPreviewSize(size int) int {
	if size <= 0 {
		return 20
	}
	if size > 100 {
		return 100
	}
	return size
}

func eventStreamTopicTailPositions(facts *plugin.TopicFacts, sampleSize int) map[string]plugin.ChangeStreamPosition {
	if facts == nil || len(facts.Partitions) == 0 {
		return nil
	}
	windowSize := (sampleSize + len(facts.Partitions) - 1) / len(facts.Partitions)
	positions := make(map[string]plugin.ChangeStreamPosition, len(facts.Partitions))
	for _, partition := range facts.Partitions {
		start := partition.LatestOffset - int64(windowSize)
		if start < partition.EarliestOffset {
			start = partition.EarliestOffset
		}
		partitionID := strconv.FormatInt(int64(partition.Partition), 10)
		positions[partitionID] = plugin.ChangeStreamPosition{
			Type:      plugin.ChangeStreamPositionTypeKafkaOffset,
			Version:   plugin.ChangeStreamPositionVersionV1,
			Partition: partitionID,
			Values:    map[string]string{"next_offset": strconv.FormatInt(start, 10)},
		}
	}
	return positions
}

func eventStreamTopicPreviewRows(batch *plugin.ChangeRecordBatch) []map[string]interface{} {
	if batch == nil || len(batch.Records) == 0 {
		return []map[string]interface{}{}
	}
	rows := make([]map[string]interface{}, 0, len(batch.Records))
	for _, record := range batch.Records {
		headers := make([]map[string]interface{}, 0, len(record.Headers))
		for _, header := range record.Headers {
			headers = append(headers, map[string]interface{}{
				"key":   header.Key,
				"value": eventStreamPreviewBytes(header.Value),
			})
		}
		timestamp := ""
		if !record.Timestamp.IsZero() {
			timestamp = record.Timestamp.UTC().Format(time.RFC3339Nano)
		}
		rows = append(rows, map[string]interface{}{
			"partition": record.Partition,
			"offset":    record.Offset,
			"timestamp": timestamp,
			"key":       eventStreamPreviewBytes(record.Key),
			"value":     eventStreamPreviewBytes(record.Value),
			"headers":   headers,
		})
	}
	return rows
}

func eventStreamPreviewBytes(value []byte) interface{} {
	if len(value) == 0 {
		return ""
	}
	var decoded interface{}
	if json.Valid(value) && json.Unmarshal(value, &decoded) == nil {
		return decoded
	}
	if utf8.Valid(value) {
		return string(value)
	}
	return map[string]interface{}{
		"encoding": "base64",
		"data":     base64.StdEncoding.EncodeToString(value),
	}
}

func eventStreamTopicName(path plugin.EngineCatalogPath) string {
	if len(path.Segments) == 0 {
		return ""
	}
	return path.Segments[len(path.Segments)-1].Name
}
