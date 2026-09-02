package continuous

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/kafka"
	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationContinuousKafkaToPostgresMultiWorkerFailoverPauseResumeStop(t *testing.T) {
	if os.Getenv("ADDP_CONTINUOUS_E2E") != "1" {
		t.Skip("set ADDP_CONTINUOUS_E2E=1 to run Kafka -> PostgreSQL continuous integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	infraInfo := testpg.ConnInfoFromEnv(t)
	infraDSN, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(infraInfo)
	if err != nil {
		t.Fatalf("build Infra PostgreSQL DSN: %v", err)
	}
	infraDB, err := gorm.Open(postgresdriver.Open(infraDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("open Infra PostgreSQL: %v", err)
	}
	if err := infraDB.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatalf("create transfer schema: %v", err)
	}
	if err := commonExecution.EnsureStore(infraDB); err != nil {
		t.Fatalf("ensure execution store: %v", err)
	}
	if err := infraDB.AutoMigrate(&models.TransferTask{}, &models.SyncState{}, &models.RuntimeLease{}); err != nil {
		t.Fatalf("migrate continuous Infra models: %v", err)
	}

	kafkaInfo := continuousKafkaIntegrationConnInfo()
	producer, err := newContinuousIntegrationProducer(kafkaInfo)
	if err != nil {
		t.Fatalf("create Kafka producer: %v", err)
	}
	defer producer.Close()
	if err := producer.Ping(ctx); err != nil {
		t.Fatalf("Kafka is not available: %v", err)
	}
	topic := fmt.Sprintf("addp-continuous-e2e-%d", time.Now().UnixNano())
	admin := kadm.NewClient(producer)
	created, err := admin.CreateTopics(ctx, 2, 1, nil, topic)
	if err != nil || created.Error() != nil {
		t.Fatalf("create Kafka topic: response=%v error=%v", created.Error(), err)
	}
	defer admin.DeleteTopics(context.Background(), topic)

	targetInfo := continuousBusinessPostgresConnInfo()
	targetDSN, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(targetInfo)
	if err != nil {
		t.Fatalf("build business PostgreSQL DSN: %v", err)
	}
	targetDB, err := sql.Open("postgres", targetDSN)
	if err != nil {
		t.Fatalf("open business PostgreSQL: %v", err)
	}
	defer targetDB.Close()
	if err := targetDB.PingContext(ctx); err != nil {
		t.Skipf("business PostgreSQL is not available: %v", err)
	}
	targetSchema := fmt.Sprintf("ct_e2e_%d", time.Now().UnixNano())
	if _, err := targetDB.ExecContext(ctx, `CREATE SCHEMA `+quoteIntegrationIdentifier(targetSchema)); err != nil {
		t.Fatalf("create target schema: %v", err)
	}
	defer targetDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+quoteIntegrationIdentifier(targetSchema)+` CASCADE`)

	task := models.TransferTask{
		TenantID: 999999, Name: "continuous-e2e", TaskType: commonExecution.TaskTypeSync,
		Config: continuousIntegrationConfig(topic, targetSchema), BatchSize: 100,
		Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped,
	}
	if err := infraDB.Create(&task).Error; err != nil {
		t.Fatalf("create continuous task: %v", err)
	}
	defer func() {
		_ = infraDB.Where("task_id = ?", task.ID).Delete(&models.RuntimeLease{}).Error
		_ = infraDB.Where("task_id = ?", task.ID).Delete(&models.SyncState{}).Error
		_ = infraDB.Where("module = ? AND source_task_id = ?", commonExecution.ModuleTransfer, fmt.Sprint(task.ID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = infraDB.Unscoped().Delete(&models.TransferTask{}, task.ID).Error
		_, _ = targetDB.ExecContext(context.Background(), `DELETE FROM addp_transfer.apply_positions WHERE apply_identity = $1::uuid`, task.ApplyIdentity)
	}()

	taskRepo := repository.NewTaskRepository(infraDB)
	firstExecution := continuousIntegrationExecution(task, uuid.NewString())
	if _, err := taskRepo.StartContinuousExecution(ctx, task.ID, task.TenantID, &firstExecution); err != nil {
		t.Fatalf("start first continuous execution: %v", err)
	}
	sourceCaps := (&kafka.KafkaPlugin{}).Capabilities()
	targetCaps := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	resolver := planner.StaticEngineResolver{
		30: {Type: "kafka", ConnInfo: kafkaInfo, Capabilities: &sourceCaps},
		8:  {Type: "postgresql", ConnInfo: targetInfo, Capabilities: &targetCaps},
	}
	leaseRepo := repository.NewRuntimeLeaseRepository(infraDB, repository.ContinuousRecoveryPolicy{
		InitialBackoff: time.Second, MaxBackoff: 4 * time.Second, MaxFailures: 3,
		CircuitOpenTime: 10 * time.Second, StabilityWindow: 30 * time.Second,
	})
	runner := &DataSessionRunner{
		ProtectionGate: allowSourceProtectionGate{},
		Resolver:       resolver, States: repository.NewSyncStateRepository(infraDB), Progress: leaseRepo,
		PollTimeout: 100 * time.Millisecond, MaxBytes: 4 << 20, DiagnosticsInterval: 50 * time.Millisecond,
	}
	workerStops := map[string]context.CancelFunc{}
	workerDone := map[string]chan error{}
	for _, owner := range []string{"continuous-e2e-worker-a", "continuous-e2e-worker-b"} {
		supervisor, err := NewSupervisor(leaseRepo, runner, Config{
			OwnerInstanceID: owner, Capacity: 1, LeaseDuration: 2 * time.Second,
			HeartbeatInterval: 100 * time.Millisecond, ClaimInterval: 20 * time.Millisecond,
		}, nil)
		if err != nil {
			t.Fatalf("NewSupervisor(%s) error = %v", owner, err)
		}
		supervisorCtx, stopSupervisor := context.WithCancel(ctx)
		done := make(chan error, 1)
		workerStops[owner] = stopSupervisor
		workerDone[owner] = done
		go func() { done <- supervisor.Run(supervisorCtx) }()
	}
	defer func() {
		for owner, stop := range workerStops {
			if workerDone[owner] == nil {
				continue
			}
			stop()
			<-workerDone[owner]
		}
	}()

	produceContinuousRecords(t, ctx, producer,
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("1"), Value: []byte(`{"id":1,"name":"first"}`)},
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("1"), Value: []byte(`{"id":1,"name":"latest"}`)},
		&kgo.Record{Topic: topic, Partition: 1, Key: []byte("2"), Value: []byte(`{"id":2,"name":"second"}`)},
	)
	waitContinuousCondition(t, ctx, "first continuous target apply", func() bool {
		return integrationTargetRows(targetDB, targetSchema) == 2 && integrationTargetName(targetDB, targetSchema, 1) == "latest"
	})
	waitContinuousCondition(t, ctx, "healthy continuous position diagnostics", func() bool {
		diagnostics := integrationContinuousDiagnostics(infraDB, firstExecution.ExecutionID)
		if diagnostics["health"] != continuousHealthHealthy || diagnostics["checkpoint_health"] != continuousHealthHealthy {
			return false
		}
		partitions, _ := diagnostics["partitions"].(map[string]interface{})
		partition0, _ := partitions["0"].(map[string]interface{})
		return fmt.Sprint(partition0["latest_offset"]) == "2" && fmt.Sprint(partition0["next_offset"]) == "2"
	})

	var firstLease models.RuntimeLease
	if err := infraDB.Where("task_id = ?", task.ID).First(&firstLease).Error; err != nil {
		t.Fatalf("load first runtime lease: %v", err)
	}
	firstOwner := firstLease.OwnerInstanceID
	workerStops[firstOwner]()
	<-workerDone[firstOwner]
	workerDone[firstOwner] = nil

	var failoverExecution commonExecution.TaskExecution
	waitContinuousCondition(t, ctx, "second worker claims recovery execution", func() bool {
		var lease models.RuntimeLease
		if err := infraDB.Where("task_id = ?", task.ID).First(&lease).Error; err != nil ||
			lease.OwnerInstanceID == firstOwner || lease.FencingToken <= firstLease.FencingToken {
			return false
		}
		return infraDB.Where("execution_id = ? AND status = ?", lease.ExecutionID, commonExecution.ExecutionStatusRunning).
			First(&failoverExecution).Error == nil
	})
	produceContinuousRecords(t, ctx, producer,
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("4"), Value: []byte(`{"id":4,"name":"after-failover"}`)},
	)
	waitContinuousCondition(t, ctx, "second worker continues from committed offsets", func() bool {
		return integrationTargetRows(targetDB, targetSchema) == 3 && integrationTargetName(targetDB, targetSchema, 4) == "after-failover"
	})

	if err := taskRepo.SetContinuousDesiredState(ctx, task.ID, task.TenantID, models.TaskDesiredStatePaused, "paused"); err != nil {
		t.Fatalf("pause continuous task: %v", err)
	}
	waitContinuousExecutionStatus(t, ctx, infraDB, failoverExecution.ExecutionID, commonExecution.ExecutionStatusCancelled, "paused")
	produceContinuousRecords(t, ctx, producer,
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("3"), Value: []byte(`{"id":3,"name":"paused"}`)},
	)
	time.Sleep(300 * time.Millisecond)
	if count := integrationTargetRows(targetDB, targetSchema); count != 3 {
		t.Fatalf("target rows after pause = %d, want 3", count)
	}

	secondExecution := continuousIntegrationExecution(task, uuid.NewString())
	if _, err := taskRepo.StartContinuousExecution(ctx, task.ID, task.TenantID, &secondExecution); err != nil {
		t.Fatalf("resume continuous execution: %v", err)
	}
	waitContinuousCondition(t, ctx, "resume from committed Kafka offsets", func() bool {
		return integrationTargetRows(targetDB, targetSchema) == 4
	})
	if err := taskRepo.SetContinuousDesiredState(ctx, task.ID, task.TenantID, models.TaskDesiredStateStopped, "stopped"); err != nil {
		t.Fatalf("stop continuous task: %v", err)
	}
	waitContinuousExecutionStatus(t, ctx, infraDB, secondExecution.ExecutionID, commonExecution.ExecutionStatusCancelled, "stopped")

	var states []models.SyncState
	if err := infraDB.Where("task_id = ?", task.ID).Order("partition").Find(&states).Error; err != nil {
		t.Fatalf("load committed partition states: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("sync states = %#v, want two partitions", states)
	}
	for _, state := range states {
		if state.PositionCommittedAt == nil {
			t.Fatalf("partition %q has no real position commit time", state.Partition)
		}
	}
}

func TestIntegrationContinuousKafkaRetentionHealthTransitions(t *testing.T) {
	if os.Getenv("ADDP_CONTINUOUS_E2E") != "1" {
		t.Skip("set ADDP_CONTINUOUS_E2E=1 to run Kafka continuous diagnostics integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	kafkaInfo := continuousKafkaIntegrationConnInfo()
	producer, err := newContinuousIntegrationProducer(kafkaInfo)
	if err != nil {
		t.Fatalf("create Kafka producer: %v", err)
	}
	defer producer.Close()
	if err := producer.Ping(ctx); err != nil {
		t.Fatalf("Kafka is not available: %v", err)
	}
	topic := fmt.Sprintf("addp-continuous-diagnostics-%d", time.Now().UnixNano())
	admin := kadm.NewClient(producer)
	created, err := admin.CreateTopics(ctx, 1, cdcDataEnvInt16("ADDP_TEST_INFRA_KAFKA_REPLICATION_FACTOR", 1), nil, topic)
	if err != nil || created.Error() != nil {
		t.Fatalf("create Kafka topic: response=%v error=%v", created.Error(), err)
	}
	defer admin.DeleteTopics(context.Background(), topic)

	plugin := &kafka.KafkaPlugin{}
	root := engineplugin.EngineCatalogRootPath(plugin.EngineCatalogModel(), 30)
	entries, err := plugin.ListChildren(ctx, kafkaInfo, root, engineplugin.ListOptions{})
	if err != nil {
		t.Fatalf("list Kafka topics: %v", err)
	}
	var topicPath engineplugin.EngineCatalogPath
	for _, entry := range entries {
		if entry.Name == topic {
			topicPath = entry.Path
			break
		}
	}
	if len(topicPath.Segments) == 0 {
		t.Fatalf("topic %q not found in Kafka catalog", topic)
	}
	reader, err := plugin.OpenChangeStream(ctx, kafkaInfo, topicPath, engineplugin.ChangeStreamReadOptions{
		ConsumerGroup:   fmt.Sprintf("addp-continuous-diagnostics-%d", time.Now().UnixNano()),
		InitialPosition: engineplugin.ChangeStreamInitialEarliest, PollTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("open Kafka change stream: %v", err)
	}
	defer reader.Close(context.Background())

	produceContinuousDiagnosticRecords(t, ctx, producer, topic, 100)
	sampledAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	committed := map[string]engineplugin.ChangeStreamPosition{"0": kafkaOffsetPosition("0", 10)}
	committedAt := map[string]time.Time{"0": sampledAt}
	first, samples := collectContinuousDiagnostics(ctx, reader, committed, committedAt, nil, sampledAt, 10*time.Second, 2*time.Second, time.Minute)
	if first.Health != continuousHealthUnknown {
		t.Fatalf("first diagnostics health = %q, want unknown", first.Health)
	}

	produceContinuousDiagnosticRecords(t, ctx, producer, topic, 20)
	second, samples := collectContinuousDiagnostics(ctx, reader, committed, committedAt, samples, sampledAt.Add(10*time.Second), 10*time.Second, 2*time.Second, time.Minute)
	partition := second.Partitions["0"]
	if second.Health != continuousHealthDegraded || partition.RetentionHorizonSeconds == nil || *partition.RetentionHorizonSeconds != 5 {
		t.Fatalf("degraded diagnostics = %#v", second)
	}

	produceContinuousDiagnosticRecords(t, ctx, producer, topic, 10)
	committed["0"] = kafkaOffsetPosition("0", 1)
	third, _ := collectContinuousDiagnostics(ctx, reader, committed, committedAt, samples, sampledAt.Add(20*time.Second), 10*time.Second, 2*time.Second, time.Minute)
	partition = third.Partitions["0"]
	if third.Health != continuousHealthCritical || partition.RetentionHorizonSeconds == nil || *partition.RetentionHorizonSeconds != 1 {
		t.Fatalf("critical diagnostics = %#v", third)
	}
}

func produceContinuousDiagnosticRecords(t *testing.T, ctx context.Context, producer *kgo.Client, topic string, count int) {
	t.Helper()
	records := make([]*kgo.Record, 0, count)
	for index := 0; index < count; index++ {
		records = append(records, &kgo.Record{Topic: topic, Partition: 0, Value: []byte(`{"id":1}`)})
	}
	produceContinuousRecords(t, ctx, producer, records...)
}

func integrationContinuousDiagnostics(db *gorm.DB, executionID string) map[string]interface{} {
	var execution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", executionID).First(&execution).Error; err != nil {
		return nil
	}
	continuousMetadata, _ := execution.Metadata["continuous"].(map[string]interface{})
	diagnostics, _ := continuousMetadata["diagnostics"].(map[string]interface{})
	return diagnostics
}

func continuousKafkaIntegrationConnInfo() engineplugin.ConnectionInfo {
	info := engineplugin.ConnectionInfo{
		"bootstrap_servers": integrationEnv("ADDP_TEST_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		"security_protocol": integrationEnv("ADDP_TEST_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
	}
	for _, key := range []string{"username", "password", "sasl_mechanism"} {
		if value := integrationEnv("ADDP_TEST_KAFKA_"+strings.ToUpper(key), ""); value != "" {
			info[key] = value
		}
	}
	if pem := os.Getenv("ADDP_TEST_KAFKA_TLS_CA_CERT"); strings.TrimSpace(pem) != "" {
		info["tls_ca_cert"] = pem
	}
	return info
}

func newContinuousIntegrationProducer(info engineplugin.ConnectionInfo) (*kgo.Client, error) {
	return kafka.NewClient(info, kgo.RecordPartitioner(kgo.ManualPartitioner()))
}

func continuousBusinessPostgresConnInfo() engineplugin.ConnectionInfo {
	return engineplugin.ConnectionInfo{
		"host":     integrationEnv("ADDP_TEST_BUSINESS_POSTGRES_HOST", "localhost"),
		"port":     integrationEnv("ADDP_TEST_BUSINESS_POSTGRES_PORT", "5433"),
		"user":     integrationEnv("ADDP_TEST_BUSINESS_POSTGRES_USER", "business"),
		"password": integrationEnv("ADDP_TEST_BUSINESS_POSTGRES_PASSWORD", "business_password"),
		"database": integrationEnv("ADDP_TEST_BUSINESS_POSTGRES_DATABASE", "business"),
		"sslmode":  "disable",
	}
}

func continuousIntegrationConfig(topic, schema string) models.JSONMap {
	config := continuousRunnerConfig()
	config["source"].(map[string]interface{})["locator"] = "addp://engine/30/path/" + topic + "?type=topic"
	target := config["target"].(map[string]interface{})
	target["parent_locator"] = "addp://engine/8/path/" + schema + "?type=schema"
	target["name"] = "orders"
	return config
}

func continuousIntegrationExecution(task models.TransferTask, executionID string) commonExecution.TaskExecution {
	now := time.Now()
	triggeredBy := 1
	return commonExecution.TaskExecution{
		TenantID: int(task.TenantID), ExecutionID: executionID, Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleTransfer,
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), SourceTaskName: &task.Name,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		TriggeredBy: &triggeredBy, ExecutionConfig: task.Config, CreatedAt: now, UpdatedAt: now,
	}
}

func produceContinuousRecords(t *testing.T, ctx context.Context, producer *kgo.Client, records ...*kgo.Record) {
	t.Helper()
	if err := producer.ProduceSync(ctx, records...).FirstErr(); err != nil {
		t.Fatalf("produce Kafka records: %v", err)
	}
}

func waitContinuousCondition(t *testing.T, ctx context.Context, label string, condition func() bool) {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for %s: %v", label, ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitContinuousExecutionStatus(t *testing.T, ctx context.Context, db *gorm.DB, executionID, status, stopReason string) {
	t.Helper()
	waitContinuousCondition(t, ctx, "execution "+executionID+" status "+status, func() bool {
		var execution commonExecution.TaskExecution
		if err := db.Where("execution_id = ?", executionID).First(&execution).Error; err != nil {
			return false
		}
		return execution.Status == status && execution.Metadata["stop_reason"] == stopReason
	})
}

func integrationTargetRows(db *sql.DB, schema string) int {
	var count int
	_ = db.QueryRow(`SELECT count(*) FROM ` + quoteIntegrationIdentifier(schema) + `.orders`).Scan(&count)
	return count
}

func integrationTargetName(db *sql.DB, schema string, id int) string {
	var name string
	_ = db.QueryRow(`SELECT name FROM `+quoteIntegrationIdentifier(schema)+`.orders WHERE id = $1`, id).Scan(&name)
	return name
}

func quoteIntegrationIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func integrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
