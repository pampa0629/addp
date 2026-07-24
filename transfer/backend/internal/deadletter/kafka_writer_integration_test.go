package deadletter

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/models"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestIntegrationKafkaPayloadWriterCreatesAndWritesTransferDLQTopic(t *testing.T) {
	if os.Getenv("ADDP_DLQ_KAFKA_INTEGRATION") != "1" {
		t.Skip("set ADDP_DLQ_KAFKA_INTEGRATION=1 to run Infra Kafka DLQ integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	transferInfo := kafkaIntegrationConnection("transfer", integrationEnv("INFRA_KAFKA_TRANSFER_PASSWORD", "addp_kafka_transfer"))
	writer, err := NewKafkaPayloadWriter(KafkaWriterConfig{
		ConnectionInfo: transferInfo, RetentionMillis: int64((10 * time.Minute) / time.Millisecond), ReplicationFactor: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	adminInfo := kafkaIntegrationConnection(integrationEnv("INFRA_KAFKA_ADMIN_USERNAME", "admin"), integrationEnv("INFRA_KAFKA_ADMIN_PASSWORD", "addp_kafka_admin"))
	cleaner, err := NewKafkaTopicCleaner(KafkaTopicCleanerConfig{ConnectionInfo: adminInfo})
	if err != nil {
		t.Fatal(err)
	}
	defer cleaner.Close()

	tenantID := uint(999999)
	taskID := uint(time.Now().UnixNano())
	topic, err := TopicName(tenantID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	defer cleaner.DeleteTaskTopic(context.Background(), tenantID, taskID)
	payload := []byte(`{"schema":"transfer.dead_letter/v1","identity":"integration"}`)
	reference, err := writer.Write(ctx, topic, "a59a0ac0-f042-5277-834d-0f982d26b7e5", time.Now(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if reference.Topic != topic || reference.Partition != 0 || reference.Offset < 0 {
		t.Fatalf("payload reference = %#v", reference)
	}

	reader, err := newKafkaClient(transferInfo,
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: {0: kgo.NewOffset().At(reference.Offset)}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	fetches := reader.PollRecords(ctx, 1)
	if err := fetches.Err(); err != nil {
		t.Fatal(err)
	}
	var read *kgo.Record
	fetches.EachRecord(func(record *kgo.Record) { read = record })
	if read == nil || string(read.Key) != "a59a0ac0-f042-5277-834d-0f982d26b7e5" || string(read.Value) != string(payload) {
		t.Fatalf("read dead-letter record = %#v", read)
	}

	probe, err := NewKafkaPayloadAvailabilityProbe(KafkaPayloadProbeConfig{ConnectionInfo: transferInfo, FetchMaxBytes: 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	referenceSnapshot := models.DeadLetterPayloadReference{
		Identity: "a59a0ac0-f042-5277-834d-0f982d26b7e5", Topic: reference.Topic,
		Partition: reference.Partition, Offset: reference.Offset,
	}
	availability, err := probe.Probe(ctx, []models.DeadLetterPayloadReference{referenceSnapshot})
	if err != nil || !availability[referenceSnapshot.Identity] {
		t.Fatalf("available payload probe result=%#v err=%v", availability, err)
	}

	if err := cleaner.DeleteTaskTopic(ctx, tenantID, taskID); err != nil {
		t.Fatal(err)
	}
	if err := cleaner.DeleteTaskTopic(ctx, tenantID, taskID); err != nil {
		t.Fatalf("idempotent dead-letter topic delete: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		probeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		availability, err = probe.Probe(probeCtx, []models.DeadLetterPayloadReference{referenceSnapshot})
		cancel()
		if err == nil {
			if available, confirmed := availability[referenceSnapshot.Identity]; confirmed && !available {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("deleted payload was not reconciled unavailable: result=%#v err=%v", availability, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func kafkaIntegrationConnection(username, password string) engineplugin.ConnectionInfo {
	return engineplugin.ConnectionInfo{
		"bootstrap_servers": integrationEnv("ADDP_TEST_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		"security_protocol": integrationEnv("ADDP_TEST_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
		"username":          username, "password": password, "sasl_mechanism": "plain", "client_id": "addp-transfer-dlq-integration",
	}
}

func integrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
