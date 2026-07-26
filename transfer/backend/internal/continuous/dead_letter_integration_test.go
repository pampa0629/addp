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
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/deadletter"
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

func TestIntegrationContinuousKafkaDeadLetterSkipAndCAS(t *testing.T) {
	if os.Getenv("ADDP_CONTINUOUS_DLQ_E2E") != "1" {
		t.Skip("set ADDP_CONTINUOUS_DLQ_E2E=1 to run Kafka DLQ -> PostgreSQL continuous E2E")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	infraInfo := testpg.ConnInfoFromEnv(t)
	infraDSN, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(infraInfo)
	if err != nil {
		t.Fatal(err)
	}
	infraDB, err := gorm.Open(postgresdriver.Open(infraDSN), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := infraDB.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(infraDB); err != nil {
		t.Fatal(err)
	}
	if err := infraDB.AutoMigrate(&models.TransferTask{}, &models.SyncState{}, &models.RuntimeLease{}, &models.DeadLetter{}); err != nil {
		t.Fatal(err)
	}

	adminKafkaInfo := cdcDataKafkaConnection(engineplugin.ConnectionInfo{
		"bootstrap_servers": cdcDataEnv("ADDP_TEST_INFRA_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		"security_protocol": cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
		"username":          cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_USERNAME", "admin"),
		"password":          cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_PASSWORD", "addp_kafka_admin"),
		"sasl_mechanism":    cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"),
	})
	producer, err := newContinuousIntegrationProducer(adminKafkaInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	admin := kadm.NewClient(producer)
	topic := fmt.Sprintf("addp-continuous-dlq-e2e-%d", time.Now().UnixNano())
	created, err := admin.CreateTopics(ctx, 1, 1, nil, topic)
	if err != nil || created.Error() != nil {
		t.Fatalf("create source topic: response=%v error=%v", created.Error(), err)
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
	targetSchema := fmt.Sprintf("ct_dlq_e2e_%d", time.Now().UnixNano())
	if _, err := targetDB.ExecContext(ctx, `CREATE SCHEMA `+quoteIntegrationIdentifier(targetSchema)); err != nil {
		t.Fatal(err)
	}
	defer targetDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+quoteIntegrationIdentifier(targetSchema)+` CASCADE`)

	config := continuousIntegrationConfig(topic, targetSchema)
	config["runtime"].(map[string]interface{})["record_failure"] = map[string]interface{}{"mode": "dead_letter"}
	task := models.TransferTask{
		TenantID: 999999, Name: "continuous-dlq-e2e", TaskType: commonExecution.TaskTypeSync,
		Config: config, BatchSize: 100, Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped,
	}
	if err := infraDB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	dlqTopic, err := deadletter.TopicName(task.TenantID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.DeleteTopics(context.Background(), dlqTopic)
	defer func() {
		_ = infraDB.Where("task_id = ?", task.ID).Delete(&models.DeadLetter{}).Error
		_ = infraDB.Where("task_id = ?", task.ID).Delete(&models.RuntimeLease{}).Error
		_ = infraDB.Where("task_id = ?", task.ID).Delete(&models.SyncState{}).Error
		_ = infraDB.Where("module = ? AND source_task_id = ?", commonExecution.ModuleTransfer, fmt.Sprint(task.ID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = infraDB.Unscoped().Delete(&models.TransferTask{}, task.ID).Error
		_, _ = targetDB.ExecContext(context.Background(), `DELETE FROM addp_transfer.apply_positions WHERE apply_identity = $1::uuid`, task.ApplyIdentity)
	}()

	transferKafkaInfo := cdcDataTransferKafkaConnection()
	dlqWriter, err := deadletter.NewKafkaPayloadWriter(deadletter.KafkaWriterConfig{
		ConnectionInfo: transferKafkaInfo, RetentionMillis: int64(time.Hour / time.Millisecond),
		ReplicationFactor: cdcDataEnvInt16("ADDP_TEST_INFRA_KAFKA_REPLICATION_FACTOR", 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer dlqWriter.Close()
	dlqRecorder, err := deadletter.NewRecorder(dlqWriter, repository.NewDeadLetterRepository(infraDB))
	if err != nil {
		t.Fatal(err)
	}

	taskRepo := repository.NewTaskRepository(infraDB)
	execution := continuousIntegrationExecution(task, uuid.NewString())
	if _, err := taskRepo.StartContinuousExecution(ctx, task.ID, task.TenantID, &execution); err != nil {
		t.Fatal(err)
	}
	sourceCaps := (&kafka.KafkaPlugin{}).Capabilities()
	targetCaps := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	resolver := planner.StaticEngineResolver{
		30: {Type: "kafka", ConnInfo: adminKafkaInfo, Capabilities: &sourceCaps},
		8:  {Type: "postgresql", ConnInfo: targetInfo, Capabilities: &targetCaps},
	}
	leaseRepo := repository.NewRuntimeLeaseRepository(infraDB, repository.ContinuousRecoveryPolicy{
		InitialBackoff: time.Second, MaxBackoff: 2 * time.Second, MaxFailures: 3,
		CircuitOpenTime: 10 * time.Second, StabilityWindow: 30 * time.Second,
	})
	runner := &DataSessionRunner{
		Resolver: resolver, States: repository.NewSyncStateRepository(infraDB), Progress: leaseRepo, DeadLetters: dlqRecorder,
		PollTimeout: 100 * time.Millisecond, MaxBytes: 4 << 20, DiagnosticsInterval: 50 * time.Millisecond,
	}
	supervisor, err := NewSupervisor(leaseRepo, runner, Config{
		OwnerInstanceID: "continuous-dlq-e2e", Capacity: 1, LeaseDuration: 2 * time.Second,
		HeartbeatInterval: 100 * time.Millisecond, ClaimInterval: 20 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	supervisorCtx, stopSupervisor := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- supervisor.Run(supervisorCtx) }()
	defer func() {
		stopSupervisor()
		<-done
	}()

	produceContinuousRecords(t, ctx, producer,
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("bad"), Value: []byte(`{"id":"bad","name":"invalid"}`)},
		&kgo.Record{Topic: topic, Partition: 0, Key: []byte("7"), Value: []byte(`{"id":7,"name":"valid"}`)},
	)
	waitContinuousCondition(t, ctx, "DLQ skip, valid target row, and aligned positions", func() bool {
		var deadLetterCount int64
		if infraDB.Model(&models.DeadLetter{}).Where("task_id = ?", task.ID).Count(&deadLetterCount).Error != nil || deadLetterCount != 1 {
			return false
		}
		var state models.SyncState
		if infraDB.Where("task_id = ? AND partition = ?", task.ID, "0").First(&state).Error != nil {
			return false
		}
		position, ok, err := syncStatePosition(&state)
		if err != nil || !ok {
			return false
		}
		nextOffset, _ := kafkaNextOffset(position)
		var targetName string
		if targetDB.QueryRowContext(ctx, `SELECT name FROM `+quoteIntegrationIdentifier(targetSchema)+`.orders WHERE id=7`).Scan(&targetName) != nil {
			return false
		}
		var ledgerOffset int64
		if targetDB.QueryRowContext(ctx, `SELECT next_offset FROM addp_transfer.apply_positions WHERE apply_identity=$1::uuid AND partition='0'`, task.ApplyIdentity).Scan(&ledgerOffset) != nil {
			return false
		}
		return targetName == "valid" && nextOffset == 2 && ledgerOffset == 2
	})

	var deadLetterRow models.DeadLetter
	if err := infraDB.Where("task_id = ?", task.ID).First(&deadLetterRow).Error; err != nil {
		t.Fatal(err)
	}
	if deadLetterRow.ErrorCode != "incompatible_field_type" || deadLetterRow.PayloadTopic != dlqTopic || deadLetterRow.SourceOffset != 0 {
		t.Fatalf("dead-letter control index = %#v", deadLetterRow)
	}
	if err := taskRepo.SetContinuousDesiredState(ctx, task.ID, task.TenantID, models.TaskDesiredStateStopped, "stopped"); err != nil {
		t.Fatal(err)
	}
	waitContinuousExecutionStatus(t, ctx, infraDB, execution.ExecutionID, commonExecution.ExecutionStatusCancelled, "stopped")
}
