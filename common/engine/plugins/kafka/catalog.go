package kafka

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/twmb/franz-go/pkg/kadm"
)

func (p *KafkaPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	if !plugin.IsCatalogRootPath(parent) || parent.Segments[0].Term != plugin.CatalogTermService {
		return nil, fmt.Errorf("kafka catalog children require service root path")
	}
	client, err := newKafkaClient(connInfo)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	topics, err := kadm.NewClient(client).ListTopics(ctx)
	if err != nil {
		return nil, fmt.Errorf("list kafka topics: %w", err)
	}
	entries := make([]plugin.CatalogEntry, 0, len(topics))
	for _, detail := range topics.Sorted() {
		if detail.Err != nil {
			return nil, fmt.Errorf("describe kafka topic %q: %w", detail.Topic, detail.Err)
		}
		if detail.IsInternal {
			continue
		}
		entries = append(entries, kafkaTopicEntry(parent, detail.Topic))
	}
	start := opts.Offset
	if start < 0 {
		start = 0
	}
	if start >= len(entries) {
		return []plugin.CatalogEntry{}, nil
	}
	end := len(entries)
	if opts.Limit > 0 && start+opts.Limit < end {
		end = start + opts.Limit
	}
	return entries[start:end], nil
}

func (p *KafkaPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	if plugin.IsCatalogRootPath(path) {
		entry := plugin.CatalogRootEntry(p.CatalogModel(), path.EngineID, "")
		return &entry, nil
	}
	topic, err := kafkaTopicFromPath(path)
	if err != nil {
		return nil, err
	}
	client, err := newKafkaClient(connInfo)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	details, err := kadm.NewClient(client).ListTopics(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("resolve kafka topic %q: %w", topic, err)
	}
	detail, ok := details[topic]
	if !ok || detail.Err != nil {
		if ok {
			return nil, fmt.Errorf("resolve kafka topic %q: %w", topic, detail.Err)
		}
		return nil, fmt.Errorf("kafka topic %q not found", topic)
	}
	entry := kafkaTopicEntry(plugin.CatalogRootPath(p.CatalogModel(), path.EngineID), topic)
	return &entry, nil
}

func (p *KafkaPlugin) DescribeCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	topic, err := kafkaTopicFromPath(path)
	if err != nil {
		return nil, err
	}
	client, err := newKafkaClient(connInfo)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	details, err := admin.ListTopics(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("describe kafka topic %q: %w", topic, err)
	}
	detail, ok := details[topic]
	if !ok || detail.Err != nil {
		if ok {
			return nil, fmt.Errorf("describe kafka topic %q: %w", topic, detail.Err)
		}
		return nil, fmt.Errorf("kafka topic %q not found", topic)
	}
	starts, err := admin.ListStartOffsets(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("list kafka topic start offsets: %w", err)
	}
	ends, err := admin.ListEndOffsets(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("list kafka topic end offsets: %w", err)
	}
	partitionIDs := make([]int, 0, len(detail.Partitions))
	for partition := range detail.Partitions {
		partitionIDs = append(partitionIDs, int(partition))
	}
	sort.Ints(partitionIDs)
	facts := &plugin.TopicFacts{PartitionCount: len(partitionIDs)}
	for _, value := range partitionIDs {
		partition := int32(value)
		part := detail.Partitions[partition]
		if part.Err != nil {
			return nil, fmt.Errorf("describe kafka topic %q partition %d: %w", topic, partition, part.Err)
		}
		start, startOK := starts.Lookup(topic, partition)
		end, endOK := ends.Lookup(topic, partition)
		if !startOK || !endOK || start.Err != nil || end.Err != nil {
			return nil, fmt.Errorf("read kafka topic %q partition %d offsets", topic, partition)
		}
		if facts.ReplicationFactor == 0 {
			facts.ReplicationFactor = len(part.Replicas)
		}
		facts.Partitions = append(facts.Partitions, plugin.TopicPartitionFacts{
			Partition: partition, Leader: part.Leader,
			Replicas: append([]int32(nil), part.Replicas...), ISR: append([]int32(nil), part.ISR...),
			EarliestOffset: start.Offset, LatestOffset: end.Offset,
		})
	}
	return &plugin.CatalogFacts{Path: path, Kind: CatalogKindTopic, Topic: facts}, nil
}

func kafkaTopicEntry(root plugin.CatalogPath, topic string) plugin.CatalogEntry {
	path := root
	path.Segments = append(append([]plugin.CatalogSegment(nil), root.Segments...), plugin.CatalogSegment{Term: CatalogTermTopic, Kind: CatalogKindTopic, Name: topic})
	return plugin.CatalogEntry{Name: topic, Path: path, Term: CatalogTermTopic, Kind: CatalogKindTopic, Role: plugin.CatalogRoleLeaf}
}

func kafkaTopicFromPath(path plugin.CatalogPath) (string, error) {
	business := plugin.CatalogPathWithoutRoot(path)
	if len(business.Segments) != 1 {
		return "", fmt.Errorf("kafka catalog path requires exactly one topic segment")
	}
	segment := business.Segments[0]
	if segment.Term != CatalogTermTopic || segment.Kind != CatalogKindTopic || strings.TrimSpace(segment.Name) == "" {
		return "", fmt.Errorf("kafka catalog path requires topic leaf")
	}
	return segment.Name, nil
}
