package kafka

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

// The gate provisions Connect and Transfer with the existing CDC ACLs.
func TestInfraKafkaStreamPrincipals(t *testing.T) {
	if os.Getenv("ADDP_INFRA_KAFKA_ACL_TEST") != "1" {
		t.Skip("requires an owned Infra Kafka broker")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	client := func(user, password string, options ...kgo.Opt) *kgo.Client {
		t.Helper()
		cfg := kafkaIntegrationConnInfo()
		cfg["username"], cfg["password"] = user, password
		c, err := newKafkaClient(cfg, append(options, kgo.RecordDeliveryTimeout(6*time.Second))...)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	const topic = "addp.cdc.test"
	writer := client("connect", os.Getenv("INFRA_KAFKA_CONNECT_PASSWORD"))
	defer writer.Close()
	if err := writer.ProduceSync(ctx, &kgo.Record{Topic: topic, Value: []byte("one")}, &kgo.Record{Topic: topic, Value: []byte("two")}).FirstErr(); err != nil {
		t.Fatal(err)
	}

	password := os.Getenv("INFRA_KAFKA_TRANSFER_PASSWORD")
	for offset, want := range []string{"one", "two"} {
		func() {
			reader := client("transfer", password, kgo.ConsumerGroup("transfer.gate"), kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()), kgo.DisableAutoCommit())
			defer reader.Close()
			fetched := reader.PollRecords(ctx, 1)
			if err := fetched.Err(); err != nil {
				t.Fatal(err)
			}
			records := fetched.Records()
			if len(records) != 1 || records[0].Offset != int64(offset) || string(records[0].Value) != want {
				t.Fatalf("consumer did not resume at offset %d with expected bytes", offset)
			}
			if err := reader.CommitRecords(ctx, records...); err != nil {
				t.Fatal(err)
			}
		}()
	}
	transfer := client("transfer", password)
	defer transfer.Close()
	md, err := kadm.NewClient(transfer).Metadata(ctx, "connect-configs")
	if err == nil {
		err = md.Topics["connect-configs"].Err
	}
	if !errors.Is(err, kerr.TopicAuthorizationFailed) {
		t.Fatalf("Transfer reading Connect metadata: %v", err)
	}
	if err := transfer.ProduceSync(ctx, &kgo.Record{Topic: topic, Value: []byte("denied")}).FirstErr(); !errors.Is(err, kerr.TopicAuthorizationFailed) {
		t.Fatalf("Transfer writing CDC: %v", err)
	}
}
