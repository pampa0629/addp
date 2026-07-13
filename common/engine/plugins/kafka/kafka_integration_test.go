package kafka

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestIntegrationKafkaCatalogAndChangeStream(t *testing.T) {
	if os.Getenv("ADDP_KAFKA_INTEGRATION") != "1" {
		t.Skip("set ADDP_KAFKA_INTEGRATION=1 to run Kafka integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connInfo := kafkaIntegrationConnInfo()
	p := &KafkaPlugin{}
	if err := p.TestConnection(ctx, connInfo); err != nil {
		t.Skipf("Kafka is not available: %v", err)
	}
	client, err := newKafkaClient(connInfo)
	if err != nil {
		t.Fatalf("newKafkaClient failed: %v", err)
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	topic := fmt.Sprintf("addp-transfer-it-%d", time.Now().UnixNano())
	created, err := admin.CreateTopics(ctx, 2, 1, nil, topic)
	if err != nil || created.Error() != nil {
		t.Fatalf("create topic: response=%v error=%v", created.Error(), err)
	}
	defer admin.DeleteTopics(context.Background(), topic)

	produced := client.ProduceSync(ctx,
		&kgo.Record{Topic: topic, Key: []byte("1"), Value: []byte(`{"id":1,"name":"first"}`)},
		&kgo.Record{Topic: topic, Key: []byte("1"), Value: []byte(`{"id":1,"name":"latest"}`)},
	)
	if err := produced.FirstErr(); err != nil {
		t.Fatalf("produce records: %v", err)
	}

	root := plugin.CatalogRootPath(p.CatalogModel(), 30)
	entries, err := p.ListChildren(ctx, connInfo, root, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren failed: %v", err)
	}
	var topicPath plugin.CatalogPath
	for _, entry := range entries {
		if entry.Name == topic {
			topicPath = entry.Path
			break
		}
	}
	if len(topicPath.Segments) == 0 {
		t.Fatalf("topic %q not found in catalog", topic)
	}
	facts, err := p.DescribeCatalogFacts(ctx, connInfo, topicPath, plugin.CatalogFactsOptions{})
	if err != nil {
		t.Fatalf("DescribeCatalogFacts failed: %v", err)
	}
	if facts.Topic == nil || facts.Topic.PartitionCount != 2 {
		t.Fatalf("topic facts=%#v, want two partitions", facts.Topic)
	}

	reader, err := p.OpenChangeStream(ctx, connInfo, topicPath, plugin.ChangeStreamReadOptions{
		ConsumerGroup:   fmt.Sprintf("addp-transfer-it-%d", time.Now().UnixNano()),
		InitialPosition: plugin.ChangeStreamInitialEarliest,
		PollTimeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("OpenChangeStream failed: %v", err)
	}
	defer reader.Close(context.Background())
	ranges, err := reader.PositionRanges(ctx)
	if err != nil {
		t.Fatalf("PositionRanges failed: %v", err)
	}
	if len(ranges) != 2 {
		t.Fatalf("position ranges=%#v, want two partitions", ranges)
	}
	var latestTotal int64
	for _, positionRange := range ranges {
		earliest, earliestErr := kafkaPositionNextOffset(positionRange.Earliest, positionRange.Partition)
		latest, latestErr := kafkaPositionNextOffset(positionRange.Latest, positionRange.Partition)
		if earliestErr != nil || latestErr != nil || latest < earliest {
			t.Fatalf("invalid position range %#v: earliest_err=%v latest_err=%v", positionRange, earliestErr, latestErr)
		}
		latestTotal += latest
	}
	if latestTotal != 2 {
		t.Fatalf("latest offset total=%d, want 2", latestTotal)
	}
	var records []plugin.ChangeRecord
	deadline := time.Now().Add(10 * time.Second)
	for len(records) < 2 && time.Now().Before(deadline) {
		batch, err := reader.Poll(ctx, 10)
		if err != nil {
			t.Fatalf("Poll failed: %v", err)
		}
		records = append(records, batch.Records...)
	}
	if len(records) != 2 {
		t.Fatalf("read %d records, want 2", len(records))
	}
	if records[0].Position.Values["next_offset"] == "" || records[1].Position.Values["next_offset"] == "" {
		t.Fatalf("records missing next_offset: %#v", records)
	}
}

func TestIntegrationKafkaChangeStreamRebalance(t *testing.T) {
	if os.Getenv("ADDP_KAFKA_INTEGRATION") != "1" {
		t.Skip("set ADDP_KAFKA_INTEGRATION=1 to run Kafka integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connInfo := kafkaIntegrationConnInfo()
	p := &KafkaPlugin{}
	if err := p.TestConnection(ctx, connInfo); err != nil {
		t.Skipf("Kafka is not available: %v", err)
	}
	client, err := newKafkaClient(connInfo)
	if err != nil {
		t.Fatalf("newKafkaClient failed: %v", err)
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	topic := fmt.Sprintf("addp-transfer-rebalance-it-%d", time.Now().UnixNano())
	created, err := admin.CreateTopics(ctx, 2, 1, nil, topic)
	if err != nil || created.Error() != nil {
		t.Fatalf("create topic: response=%v error=%v", created.Error(), err)
	}
	defer admin.DeleteTopics(context.Background(), topic)

	topicPath := kafkaTopicEntry(plugin.CatalogRootPath(p.CatalogModel(), 30), topic).Path
	group := fmt.Sprintf("addp-transfer-rebalance-it-%d", time.Now().UnixNano())
	open := func() plugin.ChangeStreamReader {
		reader, openErr := p.OpenChangeStream(ctx, connInfo, topicPath, plugin.ChangeStreamReadOptions{
			ConsumerGroup: group, InitialPosition: plugin.ChangeStreamInitialEarliest, PollTimeout: 200 * time.Millisecond,
		})
		if openErr != nil {
			t.Fatalf("OpenChangeStream failed: %v", openErr)
		}
		return reader
	}
	readerA := open()
	defer readerA.Close(context.Background())
	if _, err := readerA.Poll(ctx, 1); err != nil {
		t.Fatalf("initial reader A poll failed: %v", err)
	}
	readerB := open()
	defer readerB.Close(context.Background())

	waitForAssignments := func() ([]string, []string) {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := readerA.Poll(ctx, 1); err != nil {
				t.Fatalf("reader A rebalance poll failed: %v", err)
			}
			if _, err := readerB.Poll(ctx, 1); err != nil {
				t.Fatalf("reader B rebalance poll failed: %v", err)
			}
			a, b := readerA.Assignments(), readerB.Assignments()
			combined := append(append([]string(nil), a...), b...)
			sort.Strings(combined)
			if len(a) == 1 && len(b) == 1 && strings.Join(combined, ",") == "0,1" {
				return a, b
			}
		}
		t.Fatalf("rebalance assignments A=%v B=%v, want non-overlapping coverage of partitions 0 and 1", readerA.Assignments(), readerB.Assignments())
		return nil, nil
	}
	assignmentsA, assignmentsB := waitForAssignments()
	if assignmentsA[0] == assignmentsB[0] {
		t.Fatalf("rebalance assignments overlap: A=%v B=%v", assignmentsA, assignmentsB)
	}

	produced := client.ProduceSync(ctx,
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("partition-0"), Value: []byte(`{"partition":0}`)},
		&kgo.Record{Topic: topic, Partition: 1, Key: []byte("partition-1"), Value: []byte(`{"partition":1}`)},
	)
	if err := produced.FirstErr(); err != nil {
		t.Fatalf("produce partition records: %v", err)
	}

	seen := map[string]bool{}
	deadline := time.Now().Add(10 * time.Second)
	for len(seen) < 2 && time.Now().Before(deadline) {
		for _, reader := range []plugin.ChangeStreamReader{readerA, readerB} {
			batch, pollErr := reader.Poll(ctx, 10)
			if pollErr != nil {
				t.Fatalf("poll after rebalance failed: %v", pollErr)
			}
			for _, record := range batch.Records {
				seen[record.Partition] = true
			}
		}
	}
	if !seen["0"] || !seen["1"] {
		t.Fatalf("records after rebalance seen=%v, want both partitions", seen)
	}
}

func TestIntegrationKafkaChangeStreamRejectsExpiredCommittedPosition(t *testing.T) {
	if os.Getenv("ADDP_KAFKA_INTEGRATION") != "1" {
		t.Skip("set ADDP_KAFKA_INTEGRATION=1 to run Kafka integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connInfo := kafkaIntegrationConnInfo()
	p := &KafkaPlugin{}
	client, err := newKafkaClient(connInfo, kgo.RecordPartitioner(kgo.ManualPartitioner()))
	if err != nil {
		t.Fatalf("newKafkaClient failed: %v", err)
	}
	defer client.Close()
	if err := client.Ping(ctx); err != nil {
		t.Skipf("Kafka is not available: %v", err)
	}
	admin := kadm.NewClient(client)
	topic := fmt.Sprintf("addp-transfer-retention-it-%d", time.Now().UnixNano())
	created, err := admin.CreateTopics(ctx, 1, 1, nil, topic)
	if err != nil || created.Error() != nil {
		t.Fatalf("create topic: response=%v error=%v", created.Error(), err)
	}
	defer admin.DeleteTopics(context.Background(), topic)

	produced := client.ProduceSync(ctx,
		&kgo.Record{Topic: topic, Partition: 0, Value: []byte(`{"id":1}`)},
		&kgo.Record{Topic: topic, Partition: 0, Value: []byte(`{"id":2}`)},
		&kgo.Record{Topic: topic, Partition: 0, Value: []byte(`{"id":3}`)},
	)
	if err := produced.FirstErr(); err != nil {
		t.Fatalf("produce records: %v", err)
	}
	deleted, err := admin.DeleteRecords(ctx, kadm.OffsetsList{{Topic: topic, Partition: 0, At: 2}}.Offsets())
	if err != nil || deleted.Error() != nil {
		t.Fatalf("delete records: response=%v error=%v", deleted.Error(), err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		starts, listErr := admin.ListStartOffsets(ctx, topic)
		start, ok := starts.Lookup(topic, 0)
		if listErr == nil && ok && start.Err == nil && start.Offset >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("topic start offset did not advance: start=%#v error=%v", start, listErr)
		}
		time.Sleep(50 * time.Millisecond)
	}

	topicPath := kafkaTopicEntry(plugin.CatalogRootPath(p.CatalogModel(), 30), topic).Path
	_, err = p.OpenChangeStream(ctx, connInfo, topicPath, plugin.ChangeStreamReadOptions{
		ConsumerGroup: fmt.Sprintf("addp-transfer-retention-it-%d", time.Now().UnixNano()),
		CommittedPositions: map[string]plugin.ChangeStreamPosition{
			"0": kafkaOffsetPosition("0", 1),
		},
		InitialPosition: plugin.ChangeStreamInitialEarliest,
		PollTimeout:     time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "no longer retained") {
		t.Fatalf("OpenChangeStream error=%v, want expired committed position error", err)
	}
	_, err = p.OpenChangeStream(ctx, connInfo, topicPath, plugin.ChangeStreamReadOptions{
		ConsumerGroup: fmt.Sprintf("addp-transfer-ahead-it-%d", time.Now().UnixNano()),
		CommittedPositions: map[string]plugin.ChangeStreamPosition{
			"0": kafkaOffsetPosition("0", 999),
		},
		InitialPosition: plugin.ChangeStreamInitialEarliest,
		PollTimeout:     time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "ahead of topic end") {
		t.Fatalf("OpenChangeStream error=%v, want committed position ahead error", err)
	}
}

func kafkaIntegrationConnInfo() plugin.ConnectionInfo {
	info := plugin.ConnectionInfo{
		"bootstrap_servers": kafkaIntegrationEnv("ADDP_TEST_KAFKA_BOOTSTRAP_SERVERS", "localhost:9092"),
		"security_protocol": kafkaIntegrationEnv("ADDP_TEST_KAFKA_SECURITY_PROTOCOL", "plaintext"),
	}
	for _, key := range []string{"username", "password", "sasl_mechanism", "tls_ca_cert", "tls_client_cert", "tls_client_key"} {
		envKey := "ADDP_TEST_KAFKA_" + strings.ToUpper(key)
		if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
			info[key] = value
		}
	}
	return info
}

func kafkaIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
