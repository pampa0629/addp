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

func TestIntegrationContinuousKafkaToPostgresPauseResumeStop(t *testing.T) {
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
	producer, err := kgo.NewClient(
		kgo.SeedBrokers(strings.Split(engineplugin.GetString(kafkaInfo, "bootstrap_servers"), ",")...),
		kgo.RecordPartitioner(kgo.ManualPartitioner()),
	)
	if err != nil {
		t.Fatalf("create Kafka producer: %v", err)
	}
	defer producer.Close()
	if err := producer.Ping(ctx); err != nil {
		t.Skipf("Kafka is not available: %v", err)
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
	leaseRepo := repository.NewRuntimeLeaseRepository(infraDB)
	runner := &DataSessionRunner{
		Resolver: resolver, States: repository.NewSyncStateRepository(infraDB), Progress: leaseRepo,
		PollTimeout: 100 * time.Millisecond, MaxBytes: 4 << 20,
	}
	supervisor, err := NewSupervisor(leaseRepo, runner, Config{
		OwnerInstanceID: "continuous-e2e-worker", Capacity: 1, LeaseDuration: 2 * time.Second,
		HeartbeatInterval: 100 * time.Millisecond, ClaimInterval: 20 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("NewSupervisor() error = %v", err)
	}
	supervisorCtx, stopSupervisor := context.WithCancel(ctx)
	supervisorDone := make(chan error, 1)
	go func() { supervisorDone <- supervisor.Run(supervisorCtx) }()
	defer func() {
		stopSupervisor()
		<-supervisorDone
	}()

	produceContinuousRecords(t, ctx, producer,
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("1"), Value: []byte(`{"id":1,"name":"first"}`)},
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("1"), Value: []byte(`{"id":1,"name":"latest"}`)},
		&kgo.Record{Topic: topic, Partition: 1, Key: []byte("2"), Value: []byte(`{"id":2,"name":"second"}`)},
	)
	waitContinuousCondition(t, ctx, "first continuous target apply", func() bool {
		return integrationTargetRows(targetDB, targetSchema) == 2 && integrationTargetName(targetDB, targetSchema, 1) == "latest"
	})

	if err := taskRepo.SetContinuousDesiredState(ctx, task.ID, task.TenantID, models.TaskDesiredStatePaused, "paused"); err != nil {
		t.Fatalf("pause continuous task: %v", err)
	}
	waitContinuousExecutionStatus(t, ctx, infraDB, firstExecution.ExecutionID, commonExecution.ExecutionStatusCancelled, "paused")
	produceContinuousRecords(t, ctx, producer,
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("3"), Value: []byte(`{"id":3,"name":"paused"}`)},
	)
	time.Sleep(300 * time.Millisecond)
	if count := integrationTargetRows(targetDB, targetSchema); count != 2 {
		t.Fatalf("target rows after pause = %d, want 2", count)
	}

	secondExecution := continuousIntegrationExecution(task, uuid.NewString())
	if _, err := taskRepo.StartContinuousExecution(ctx, task.ID, task.TenantID, &secondExecution); err != nil {
		t.Fatalf("resume continuous execution: %v", err)
	}
	waitContinuousCondition(t, ctx, "resume from committed Kafka offsets", func() bool {
		return integrationTargetRows(targetDB, targetSchema) == 3
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
}

func continuousKafkaIntegrationConnInfo() engineplugin.ConnectionInfo {
	return engineplugin.ConnectionInfo{
		"bootstrap_servers": integrationEnv("ADDP_TEST_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		"security_protocol": "plaintext",
	}
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
		TriggeredBy: &triggeredBy, ExecutionConfig: task.Config, StartedAt: &now, CreatedAt: now, UpdatedAt: now,
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
