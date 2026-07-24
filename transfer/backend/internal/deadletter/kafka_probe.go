package deadletter

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/models"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

type KafkaPayloadProbeConfig struct {
	ConnectionInfo engineplugin.ConnectionInfo
	FetchMaxBytes  int
}

// KafkaPayloadAvailabilityProbe 只确认 exact payload reference 是否仍存在，不解码或返回 value。
type KafkaPayloadAvailabilityProbe struct {
	connectionInfo engineplugin.ConnectionInfo
	fetchMaxBytes  int
}

func NewKafkaPayloadAvailabilityProbe(config KafkaPayloadProbeConfig) (*KafkaPayloadAvailabilityProbe, error) {
	if config.FetchMaxBytes <= 0 || int64(config.FetchMaxBytes) > math.MaxInt32 {
		return nil, fmt.Errorf("dead-letter availability fetch max bytes must be within int32 range")
	}
	client, err := newKafkaClient(config.ConnectionInfo)
	if err != nil {
		return nil, err
	}
	// 配置校验创建的 client 尚未发起网络请求，立即释放即可。
	client.Close()
	return &KafkaPayloadAvailabilityProbe{connectionInfo: config.ConnectionInfo, fetchMaxBytes: config.FetchMaxBytes}, nil
}

// Probe 返回已确认 identity 的 availability；运行错误对应的 identity 不进入结果。
func (p *KafkaPayloadAvailabilityProbe) Probe(ctx context.Context, references []models.DeadLetterPayloadReference) (map[string]bool, error) {
	results := make(map[string]bool, len(references))
	groups, topics, err := groupPayloadReferences(references)
	if err != nil || len(groups) == 0 {
		return results, err
	}

	metadataClient, err := newKafkaClient(p.connectionInfo)
	if err != nil {
		return results, err
	}
	admin := kadm.NewClient(metadataClient)
	details, metadataErr := admin.ListTopics(ctx, topics...)
	if metadataErr != nil {
		metadataClient.Close()
		return results, fmt.Errorf("describe Infra Kafka dead-letter topics: %w", metadataErr)
	}

	activeTopics := make(map[string]struct{}, len(topics))
	var operationalErrors []error
	for key, refs := range groups {
		detail, ok := details[key.topic]
		if !ok || errors.Is(detail.Err, kerr.UnknownTopicOrPartition) {
			resolvePayloadReferences(results, refs, false)
			delete(groups, key)
			continue
		}
		if detail.Err != nil {
			operationalErrors = append(operationalErrors, fmt.Errorf("describe dead-letter topic %q: %w", key.topic, detail.Err))
			delete(groups, key)
			continue
		}
		partition, ok := detail.Partitions[key.partition]
		if !ok {
			resolvePayloadReferences(results, refs, false)
			delete(groups, key)
			continue
		}
		if partition.Err != nil {
			operationalErrors = append(operationalErrors, fmt.Errorf("describe dead-letter topic %q partition %d: %w", key.topic, key.partition, partition.Err))
			delete(groups, key)
			continue
		}
		activeTopics[key.topic] = struct{}{}
	}

	if len(groups) == 0 {
		metadataClient.Close()
		return results, errors.Join(operationalErrors...)
	}
	activeTopicNames := sortedTopicNames(activeTopics)
	startOffsets, startErr := admin.ListStartOffsets(ctx, activeTopicNames...)
	endOffsets, endErr := admin.ListEndOffsets(ctx, activeTopicNames...)
	metadataClient.Close()
	if startErr != nil || endErr != nil {
		operationalErrors = append(operationalErrors, fmt.Errorf("list dead-letter retention offsets: %w", errors.Join(startErr, endErr)))
		return results, errors.Join(operationalErrors...)
	}

	assignments := make(map[string]map[int32]kgo.Offset)
	for key, refs := range groups {
		start, startOK := startOffsets.Lookup(key.topic, key.partition)
		end, endOK := endOffsets.Lookup(key.topic, key.partition)
		if !startOK || !endOK || start.Err != nil || end.Err != nil {
			operationalErrors = append(operationalErrors, fmt.Errorf("read dead-letter retention boundary for %s/%d: %w",
				key.topic, key.partition, errors.Join(start.Err, end.Err)))
			delete(groups, key)
			continue
		}
		inRange := refs[:0]
		for _, reference := range refs {
			if reference.Offset < start.Offset || reference.Offset >= end.Offset {
				results[reference.Identity] = false
				continue
			}
			inRange = append(inRange, reference)
		}
		if len(inRange) == 0 {
			delete(groups, key)
			continue
		}
		groups[key] = inRange
		if assignments[key.topic] == nil {
			assignments[key.topic] = make(map[int32]kgo.Offset)
		}
		assignments[key.topic][key.partition] = kgo.NewOffset().At(inRange[0].Offset)
	}
	if len(groups) == 0 {
		return results, errors.Join(operationalErrors...)
	}

	consumer, err := newKafkaClient(p.connectionInfo,
		kgo.ConsumePartitions(assignments),
		kgo.FetchMaxBytes(int32(p.fetchMaxBytes)),
		kgo.FetchMaxPartitionBytes(int32(p.fetchMaxBytes)),
		kgo.FetchMaxWait(250*time.Millisecond),
	)
	if err != nil {
		operationalErrors = append(operationalErrors, err)
		return results, errors.Join(operationalErrors...)
	}
	defer consumer.Close()

	for len(groups) > 0 {
		fetches := consumer.PollFetches(ctx)
		if ctx.Err() != nil {
			operationalErrors = append(operationalErrors, ctx.Err())
			break
		}
		progressed := false
		fetches.EachPartition(func(partition kgo.FetchTopicPartition) {
			key := payloadPartitionKey{topic: partition.Topic, partition: partition.Partition}
			refs, ok := groups[key]
			if !ok {
				return
			}
			if partition.Err != nil {
				if errors.Is(partition.Err, kerr.OffsetOutOfRange) || errors.Is(partition.Err, kerr.UnknownTopicOrPartition) {
					resolvePayloadReferences(results, refs, false)
					delete(groups, key)
					progressed = true
					return
				}
				operationalErrors = append(operationalErrors, fmt.Errorf("fetch dead-letter payload %s/%d: %w", key.topic, key.partition, partition.Err))
				delete(groups, key)
				return
			}
			remaining := resolveFetchedPayloadReferences(results, refs, partition)
			if len(remaining) != len(refs) {
				progressed = true
			}
			if len(remaining) == 0 {
				delete(groups, key)
			} else {
				groups[key] = remaining
			}
		})
		if len(operationalErrors) > 0 && !progressed {
			break
		}
	}
	return results, errors.Join(operationalErrors...)
}

type payloadPartitionKey struct {
	topic     string
	partition int32
}

func groupPayloadReferences(references []models.DeadLetterPayloadReference) (map[payloadPartitionKey][]models.DeadLetterPayloadReference, []string, error) {
	groups := make(map[payloadPartitionKey][]models.DeadLetterPayloadReference)
	topicSet := make(map[string]struct{})
	identities := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if strings.TrimSpace(reference.Identity) == "" || !strings.HasPrefix(reference.Topic, topicPrefix) || reference.Partition < 0 || reference.Offset < 0 {
			return nil, nil, fmt.Errorf("invalid dead-letter payload reference")
		}
		if _, exists := identities[reference.Identity]; exists {
			return nil, nil, fmt.Errorf("duplicate dead-letter payload identity %q", reference.Identity)
		}
		identities[reference.Identity] = struct{}{}
		key := payloadPartitionKey{topic: reference.Topic, partition: reference.Partition}
		groups[key] = append(groups[key], reference)
		topicSet[reference.Topic] = struct{}{}
	}
	for key := range groups {
		sort.Slice(groups[key], func(i, j int) bool { return groups[key][i].Offset < groups[key][j].Offset })
	}
	return groups, sortedTopicNames(topicSet), nil
}

func sortedTopicNames(topics map[string]struct{}) []string {
	names := make([]string, 0, len(topics))
	for topic := range topics {
		names = append(names, topic)
	}
	sort.Strings(names)
	return names
}

func resolvePayloadReferences(results map[string]bool, references []models.DeadLetterPayloadReference, available bool) {
	for _, reference := range references {
		results[reference.Identity] = available
	}
}

func resolveFetchedPayloadReferences(results map[string]bool, references []models.DeadLetterPayloadReference, partition kgo.FetchTopicPartition) []models.DeadLetterPayloadReference {
	remaining := make([]models.DeadLetterPayloadReference, 0, len(references))
	for _, reference := range references {
		resolved := false
		for _, record := range partition.Records {
			if record.Offset < reference.Offset {
				continue
			}
			if record.Offset == reference.Offset {
				results[reference.Identity] = validPayloadRecord(record, reference.Identity)
			} else {
				results[reference.Identity] = false
			}
			resolved = true
			break
		}
		if !resolved && len(partition.Records) == 0 && (partition.LogStartOffset > reference.Offset || partition.HighWatermark > reference.Offset) {
			results[reference.Identity] = false
			resolved = true
		}
		if !resolved {
			remaining = append(remaining, reference)
		}
	}
	return remaining
}

func validPayloadRecord(record *kgo.Record, identity string) bool {
	if record == nil || string(record.Key) != identity || len(record.Value) == 0 {
		return false
	}
	for _, header := range record.Headers {
		if header.Key == "addp-schema" && string(header.Value) == EnvelopeSchemaV1 {
			return true
		}
	}
	return false
}
