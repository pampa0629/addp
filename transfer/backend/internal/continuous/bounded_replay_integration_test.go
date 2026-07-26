package continuous

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/kafka"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/transfer/internal/planner"
	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestIntegrationBoundedReplayCreatesIsolatedPostgreSQLTarget(t *testing.T) {
	if os.Getenv("ADDP_CONTINUOUS_REPLAY_E2E") != "1" {
		t.Skip("set ADDP_CONTINUOUS_REPLAY_E2E=1 to run Kafka -> isolated PostgreSQL bounded replay E2E")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	kafkaInfo := cdcDataKafkaConnection(engineplugin.ConnectionInfo{
		"bootstrap_servers": cdcDataEnv("ADDP_TEST_INFRA_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		"security_protocol": cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
		"username":          cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_USERNAME", "admin"),
		"password":          cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_PASSWORD", "addp_kafka_admin"),
		"sasl_mechanism":    cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"),
	})
	producer, err := newContinuousIntegrationProducer(kafkaInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	admin := kadm.NewClient(producer)
	topic := fmt.Sprintf("addp-bounded-replay-e2e-%d", time.Now().UnixNano())
	created, err := admin.CreateTopics(ctx, 1, 1, nil, topic)
	if err != nil || created.Error() != nil {
		t.Fatalf("create replay source topic: response=%v error=%v", created.Error(), err)
	}
	defer admin.DeleteTopics(context.Background(), topic)

	targetInfo := continuousBusinessPostgresConnInfo()
	targetDSN, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(targetInfo)
	if err != nil {
		t.Fatal(err)
	}
	targetDB, err := sql.Open("postgres", targetDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer targetDB.Close()
	if err := targetDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("ct_replay_e2e_%d", time.Now().UnixNano())
	if _, err := targetDB.ExecContext(ctx, `CREATE SCHEMA `+quoteIntegrationIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer targetDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+quoteIntegrationIdentifier(schema)+` CASCADE`)
	if _, err := targetDB.ExecContext(ctx, `CREATE TABLE `+quoteIntegrationIdentifier(schema)+`.orders (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := targetDB.ExecContext(ctx, `INSERT INTO `+quoteIntegrationIdentifier(schema)+`.orders (id,name) VALUES (999,'owner-target-sentinel')`); err != nil {
		t.Fatal(err)
	}

	produceContinuousRecords(t, ctx, producer,
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("1"), Value: []byte(`{"id":1,"name":"one"}`)},
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("2"), Value: []byte(`{"id":2,"name":"two"}`)},
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("3"), Value: []byte(`{"id":3,"name":"three"}`)},
	)

	ownerSpec, err := planner.ParseContinuousTaskSpec(continuousIntegrationConfig(topic, schema))
	if err != nil {
		t.Fatal(err)
	}
	sourceCapabilities := (&kafka.KafkaPlugin{}).Capabilities()
	targetCapabilities := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	plan, err := planner.BuildReplayContinuousPlan(ownerSpec, planner.ReplayTargetSpec{
		ParentLocator: fmt.Sprintf("addp://engine/8/path/%s?type=schema", schema), Name: "orders_replay",
	}, planner.StaticEngineResolver{
		30: {Type: "kafka", ConnInfo: kafkaInfo, Capabilities: &sourceCapabilities},
		8:  {Type: "postgresql", ConnInfo: targetInfo, Capabilities: &targetCapabilities},
	})
	if err != nil {
		t.Fatal(err)
	}
	applyIdentity := uuid.NewString()
	runtime := NewReplayRuntime(BoundedReplayRunner{
		PollTimeout: 100 * time.Millisecond, MaxBytes: 4 << 20,
		AssertTargetAbsent: NewReplayTargetAbsenceValidator(nil),
	})
	ranges := []planner.ReplayOffsetRange{{Partition: "0", StartOffset: 0, EndOffset: 3}}
	snapshot, err := runtime.Prepare(ctx, plan, ranges, applyIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 1 || snapshot[0].EarliestOffset > 0 || snapshot[0].LatestOffset < 3 {
		t.Fatalf("retention snapshot = %#v", snapshot)
	}
	result, err := runtime.Run(ctx, plan, ranges, applyIdentity, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.RecordsRead != 3 || result.RecordsWritten != 3 || result.Positions["0"] != 3 {
		t.Fatalf("replay result = %#v", result)
	}

	var replayRows, ownerRows int
	if err := targetDB.QueryRowContext(ctx, `SELECT count(*) FROM `+quoteIntegrationIdentifier(schema)+`.orders_replay`).Scan(&replayRows); err != nil {
		t.Fatal(err)
	}
	if err := targetDB.QueryRowContext(ctx, `SELECT count(*) FROM `+quoteIntegrationIdentifier(schema)+`.orders`).Scan(&ownerRows); err != nil {
		t.Fatal(err)
	}
	var ownerName string
	if err := targetDB.QueryRowContext(ctx, `SELECT name FROM `+quoteIntegrationIdentifier(schema)+`.orders WHERE id=999`).Scan(&ownerName); err != nil {
		t.Fatal(err)
	}
	if replayRows != 3 || ownerRows != 1 || ownerName != "owner-target-sentinel" {
		t.Fatalf("isolated replay rows=%d owner rows=%d owner name=%q", replayRows, ownerRows, ownerName)
	}
	var nextOffset int64
	var targetIdentity string
	if err := targetDB.QueryRowContext(ctx, `SELECT next_offset,target_identity FROM addp_transfer.apply_positions WHERE apply_identity=$1::uuid AND partition='0'`, applyIdentity).Scan(&nextOffset, &targetIdentity); err != nil {
		t.Fatal(err)
	}
	if nextOffset != 3 || targetIdentity != schema+".orders_replay" {
		t.Fatalf("replay ledger next_offset=%d target_identity=%q", nextOffset, targetIdentity)
	}
	defer targetDB.ExecContext(context.Background(), `DELETE FROM addp_transfer.apply_positions WHERE apply_identity=$1::uuid`, applyIdentity)
}
