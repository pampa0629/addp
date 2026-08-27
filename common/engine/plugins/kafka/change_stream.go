package kafka

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

type kafkaChangeStreamReader struct {
	client      *kgo.Client
	topic       string
	pollTimeout time.Duration
	mu          sync.RWMutex
	assignments map[int32]struct{}
	blocked     bool
}

func (p *KafkaPlugin) OpenChangeStream(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ChangeStreamReadOptions) (plugin.ChangeStreamReader, error) {
	topic, err := kafkaTopicFromPath(path)
	if err != nil {
		return nil, err
	}
	if opts.ConsumerGroup == "" {
		return nil, fmt.Errorf("kafka change stream requires consumer group")
	}
	if opts.InitialPosition != plugin.ChangeStreamInitialEarliest && opts.InitialPosition != plugin.ChangeStreamInitialLatest {
		return nil, fmt.Errorf("kafka change stream initial position must be earliest or latest")
	}
	for partition, position := range opts.CommittedPositions {
		if _, err := kafkaPositionNextOffset(position, partition); err != nil {
			return nil, fmt.Errorf("invalid committed position for partition %q: %w", partition, err)
		}
	}
	reader := &kafkaChangeStreamReader{topic: topic, pollTimeout: opts.PollTimeout, assignments: map[int32]struct{}{}}
	if reader.pollTimeout <= 0 {
		reader.pollTimeout = 5 * time.Second
	}
	consumerOpts := []kgo.Opt{
		kgo.ConsumerGroup(opts.ConsumerGroup),
		kgo.ConsumeTopics(topic),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
			reader.mu.Lock()
			defer reader.mu.Unlock()
			for assignedTopic, partitions := range assigned {
				if assignedTopic != topic {
					continue
				}
				for _, partition := range partitions {
					reader.assignments[partition] = struct{}{}
				}
			}
		}),
		kgo.AdjustFetchOffsetsFn(func(_ context.Context, assigned map[string]map[int32]kgo.Offset) (map[string]map[int32]kgo.Offset, error) {
			return adjustKafkaFetchOffsets(topic, opts.CommittedPositions, opts.InitialPosition, assigned)
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, revoked map[string][]int32) {
			reader.mu.Lock()
			defer reader.mu.Unlock()
			for revokedTopic, partitions := range revoked {
				if revokedTopic != topic {
					continue
				}
				for _, partition := range partitions {
					delete(reader.assignments, partition)
				}
			}
		}),
	}
	if opts.MaxBytes > 0 {
		consumerOpts = append(consumerOpts, kgo.FetchMaxBytes(int32(opts.MaxBytes)))
	}
	client, err := newKafkaClient(connInfo, consumerOpts...)
	if err != nil {
		return nil, err
	}
	reader.client = client
	if err := client.Ping(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("open kafka change stream: %w", err)
	}
	if err := validateKafkaCommittedPositions(ctx, client, topic, opts.CommittedPositions); err != nil {
		client.Close()
		return nil, err
	}
	return reader, nil
}

func adjustKafkaFetchOffsets(topic string, committedPositions map[string]plugin.ChangeStreamPosition, initialPosition string, assigned map[string]map[int32]kgo.Offset) (map[string]map[int32]kgo.Offset, error) {
	for partition := range assigned[topic] {
		partitionText := strconv.FormatInt(int64(partition), 10)
		if committed, ok := committedPositions[partitionText]; ok {
			nextOffset, err := kafkaPositionNextOffset(committed, partitionText)
			if err != nil {
				return nil, fmt.Errorf("adjust committed position for partition %q: %w", partitionText, err)
			}
			assigned[topic][partition] = kgo.NewOffset().At(nextOffset).WithEpoch(-1)
			continue
		}
		if initialPosition == plugin.ChangeStreamInitialEarliest {
			assigned[topic][partition] = kgo.NewOffset().AtStart()
		} else {
			assigned[topic][partition] = kgo.NewOffset().AtEnd()
		}
	}
	return assigned, nil
}

func readKafkaPositionRanges(ctx context.Context, admin *kadm.Client, topic string) ([]plugin.ChangeStreamPositionRange, error) {
	starts, err := admin.ListStartOffsets(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("list kafka topic start offsets: %w", err)
	}
	ends, err := admin.ListEndOffsets(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("list kafka topic end offsets: %w", err)
	}
	partitionEnds := ends[topic]
	if len(partitionEnds) == 0 {
		return nil, fmt.Errorf("read Kafka retained ranges for topic %q: no partitions", topic)
	}
	partitionIDs := make([]int, 0, len(partitionEnds))
	for partition := range partitionEnds {
		partitionIDs = append(partitionIDs, int(partition))
	}
	sort.Ints(partitionIDs)
	ranges := make([]plugin.ChangeStreamPositionRange, 0, len(partitionIDs))
	for _, value := range partitionIDs {
		partition := int32(value)
		start, startOK := starts.Lookup(topic, partition)
		end, endOK := ends.Lookup(topic, partition)
		if !startOK || !endOK || start.Err != nil || end.Err != nil {
			return nil, fmt.Errorf("read Kafka retained range for topic %q partition %d", topic, partition)
		}
		partitionText := strconv.FormatInt(int64(partition), 10)
		ranges = append(ranges, plugin.ChangeStreamPositionRange{
			Partition: partitionText,
			Earliest:  kafkaOffsetPosition(partitionText, start.Offset),
			Latest:    kafkaOffsetPosition(partitionText, end.Offset),
		})
	}
	return ranges, nil
}

func (r *kafkaChangeStreamReader) PositionRanges(ctx context.Context) ([]plugin.ChangeStreamPositionRange, error) {
	return readKafkaPositionRanges(ctx, kadm.NewClient(r.client), r.topic)
}

func validateKafkaCommittedPositions(ctx context.Context, client *kgo.Client, topic string, positions map[string]plugin.ChangeStreamPosition) error {
	if len(positions) == 0 {
		return nil
	}
	ranges, err := readKafkaPositionRanges(ctx, kadm.NewClient(client), topic)
	if err != nil {
		return fmt.Errorf("read kafka retained ranges before resume: %w", err)
	}
	rangesByPartition := make(map[string]plugin.ChangeStreamPositionRange, len(ranges))
	for _, positionRange := range ranges {
		rangesByPartition[positionRange.Partition] = positionRange
	}
	for partitionText, position := range positions {
		if partitionValue, err := strconv.ParseInt(partitionText, 10, 32); err != nil || partitionValue < 0 {
			return fmt.Errorf("invalid committed Kafka partition %q", partitionText)
		}
		nextOffset, err := kafkaPositionNextOffset(position, partitionText)
		if err != nil {
			return fmt.Errorf("invalid committed position for partition %q: %w", partitionText, err)
		}
		positionRange, ok := rangesByPartition[partitionText]
		if !ok {
			return fmt.Errorf("read Kafka retained range for topic %q partition %s", topic, partitionText)
		}
		earliest, _ := kafkaPositionNextOffset(positionRange.Earliest, partitionText)
		latest, _ := kafkaPositionNextOffset(positionRange.Latest, partitionText)
		if nextOffset < earliest {
			return fmt.Errorf("Kafka committed position is no longer retained for topic %q partition %s: next_offset=%d earliest=%d", topic, partitionText, nextOffset, earliest)
		}
		if nextOffset > latest {
			return fmt.Errorf("Kafka committed position is ahead of topic end for topic %q partition %s: next_offset=%d latest=%d", topic, partitionText, nextOffset, latest)
		}
	}
	return nil
}

func (r *kafkaChangeStreamReader) Poll(ctx context.Context, maxRecords int) (*plugin.ChangeRecordBatch, error) {
	if maxRecords <= 0 {
		return nil, fmt.Errorf("kafka change stream poll requires positive maxRecords")
	}
	r.mu.Lock()
	allowRebalance := r.blocked
	r.blocked = false
	r.mu.Unlock()
	if allowRebalance {
		r.client.AllowRebalance()
	}
	pollCtx, cancel := context.WithTimeout(ctx, r.pollTimeout)
	defer cancel()
	fetches := r.client.PollRecords(pollCtx, maxRecords)
	for _, fetchErr := range fetches.Errors() {
		if errors.Is(fetchErr.Err, context.DeadlineExceeded) || errors.Is(fetchErr.Err, context.Canceled) && ctx.Err() == nil {
			continue
		}
		r.client.AllowRebalance()
		return nil, fmt.Errorf("poll kafka topic %q partition %d: %w", fetchErr.Topic, fetchErr.Partition, fetchErr.Err)
	}
	batch := &plugin.ChangeRecordBatch{EndPositions: map[string]plugin.ChangeStreamPosition{}}
	fetches.EachRecord(func(record *kgo.Record) {
		partition := strconv.FormatInt(int64(record.Partition), 10)
		position := kafkaOffsetPosition(partition, record.Offset+1)
		headers := make([]plugin.ChangeRecordHeader, 0, len(record.Headers))
		for _, header := range record.Headers {
			headers = append(headers, plugin.ChangeRecordHeader{Key: header.Key, Value: append([]byte(nil), header.Value...)})
		}
		batch.Records = append(batch.Records, plugin.ChangeRecord{
			Topic: record.Topic, Partition: partition, Offset: record.Offset, Timestamp: record.Timestamp,
			Headers: headers, Key: append([]byte(nil), record.Key...), Value: append([]byte(nil), record.Value...), Position: position,
		})
		batch.EndPositions[partition] = position
	})
	if len(batch.Records) > 0 {
		r.mu.Lock()
		r.blocked = true
		r.mu.Unlock()
	} else {
		r.client.AllowRebalance()
	}
	return batch, nil
}

func (r *kafkaChangeStreamReader) Assignments() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]string, 0, len(r.assignments))
	for partition := range r.assignments {
		result = append(result, strconv.FormatInt(int64(partition), 10))
	}
	sort.Strings(result)
	return result
}

func (r *kafkaChangeStreamReader) Pause(_ context.Context, partitions []string) error {
	parsed, err := parseKafkaPartitions(partitions)
	if err != nil {
		return err
	}
	r.client.PauseFetchPartitions(map[string][]int32{r.topic: parsed})
	return nil
}

func (r *kafkaChangeStreamReader) Resume(_ context.Context, partitions []string) error {
	parsed, err := parseKafkaPartitions(partitions)
	if err != nil {
		return err
	}
	r.client.ResumeFetchPartitions(map[string][]int32{r.topic: parsed})
	return nil
}

func (r *kafkaChangeStreamReader) Close(context.Context) error {
	r.mu.Lock()
	allowRebalance := r.blocked
	r.blocked = false
	r.mu.Unlock()
	if allowRebalance {
		r.client.AllowRebalance()
	}
	r.client.Close()
	return nil
}

func parseKafkaPartitions(partitions []string) ([]int32, error) {
	result := make([]int32, 0, len(partitions))
	for _, partition := range partitions {
		value, err := strconv.ParseInt(partition, 10, 32)
		if err != nil || value < 0 {
			return nil, fmt.Errorf("invalid kafka partition %q", partition)
		}
		result = append(result, int32(value))
	}
	return result, nil
}

func kafkaPositionNextOffset(position plugin.ChangeStreamPosition, partition string) (int64, error) {
	if position.Type != plugin.ChangeStreamPositionTypeKafkaOffset || position.Version != plugin.ChangeStreamPositionVersionV1 || position.Partition != partition {
		return 0, fmt.Errorf("invalid kafka position identity")
	}
	value, err := strconv.ParseInt(position.Values["next_offset"], 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid kafka next_offset")
	}
	return value, nil
}

func kafkaOffsetPosition(partition string, nextOffset int64) plugin.ChangeStreamPosition {
	return plugin.ChangeStreamPosition{
		Type: plugin.ChangeStreamPositionTypeKafkaOffset, Version: plugin.ChangeStreamPositionVersionV1, Partition: partition,
		Values: map[string]string{"next_offset": strconv.FormatInt(nextOffset, 10)},
	}
}
