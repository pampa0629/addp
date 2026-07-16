package continuous

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/kafka"
	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/capture"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	"github.com/lib/pq"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgreSQLCDCDataPlaneSnapshotUpdateDeleteCrashResumeAndSchemaDriftBlock(t *testing.T) {
	if os.Getenv("ADDP_CDC_DATA_E2E") != "1" {
		t.Skip("set ADDP_CDC_DATA_E2E=1 to run PostgreSQL CDC data-plane integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	if err := infraDB.AutoMigrate(&models.TransferTask{}, &models.SyncState{}, &models.RuntimeLease{}, &models.CaptureResource{}); err != nil {
		t.Fatal(err)
	}

	businessInfo := cdcDataBusinessPostgresConnInfo()
	businessDSN, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(businessInfo)
	if err != nil {
		t.Fatal(err)
	}
	businessDB, err := sql.Open("postgres", businessDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer businessDB.Close()
	if err := businessDB.PingContext(ctx); err != nil {
		t.Skipf("business PostgreSQL is unavailable: %v", err)
	}
	suffix := time.Now().UnixNano()
	schema := fmt.Sprintf("cdc_data_%d", suffix)
	sourceTable := "orders"
	targetTable := "orders_target"
	if _, err := businessDB.ExecContext(ctx, `CREATE SCHEMA `+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer businessDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+pq.QuoteIdentifier(schema)+` CASCADE`)
	if _, err := businessDB.ExecContext(ctx, `CREATE TABLE `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(sourceTable)+` (
		id bigint PRIMARY KEY,
		name text,
		amount numeric(30,4) NOT NULL,
		business_date date NOT NULL,
		business_time time(3) without time zone NOT NULL,
		changed_at timestamp(3) without time zone NOT NULL,
		changed_at_tz timestamp(6) with time zone NOT NULL,
		enabled boolean NOT NULL,
		payload jsonb NOT NULL,
		ref uuid NOT NULL,
		shape geometry(MultiPolygon,4549)
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := businessDB.ExecContext(ctx, `INSERT INTO `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(sourceTable)+` VALUES (
		1, 'snapshot', 12345678901234567890.1234, DATE '2024-01-02', TIME '03:04:05.006',
		TIMESTAMP '2024-01-02 03:04:05.006', TIMESTAMPTZ '2024-01-02 03:04:05.006789+08', true,
		'{"enabled":true,"items":2}'::jsonb, '550e8400-e29b-41d4-a716-446655440000'::uuid,
		ST_GeomFromText('MULTIPOLYGON(((0 0,10 0,10 10,0 0)))', 4549)
	)`); err != nil {
		t.Fatal(err)
	}

	task := models.TransferTask{
		TenantID: uint(700000 + suffix%90000), Name: "cdc-data-e2e", TaskType: commonExecution.TaskTypeSync,
		Config: cdcDataTaskConfig(schema, sourceTable, targetTable), BatchSize: 100,
		Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped,
	}
	if err := infraDB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	defer cleanupCDCDataInfraRows(infraDB, task.ID)

	resolver := planner.StaticEngineResolver{
		12: {Type: "postgresql", EngineID: 12, ConnInfo: businessInfo},
		20: {Type: "postgresql", EngineID: 20, ConnInfo: businessInfo, Capabilities: ptrEngineCapabilities((&postgresql.PostgreSQLPlugin{}).Capabilities())},
	}
	captureRepo := repository.NewCaptureRepository(infraDB)
	topicAdmin, err := capture.NewKafkaTopicAdmin(capture.KafkaAdminConfig{
		BootstrapServers: cdcDataEnv("ADDP_TEST_INFRA_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		Username:         cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_USERNAME", "admin"),
		Password:         cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_PASSWORD", "addp_kafka_admin"),
		SecurityProtocol: cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer topicAdmin.Close()
	connectClient, err := capture.NewConnectClient(cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_URL", "http://localhost:18083"), "", "", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	captureSupervisor, err := capture.NewSupervisor(
		captureRepo, capture.NewPostgreSQLPlanResolver(resolver), connectClient, topicAdmin,
		capture.PostgreSQLSourceResources{}, capture.SupervisorConfig{
			TopicRetention: time.Hour, TopicReplication: 1,
			ConnectLoopbackHost: cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_LOOPBACK_HOST", "host.docker.internal"),
			ProvisioningTimeout: 60 * time.Second, StatusPollInterval: 500 * time.Millisecond, MonitorInterval: time.Second,
		}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := captureSupervisor.Start(ctx, &task)
	if err != nil {
		t.Fatalf("start CDC capture: %v", err)
	}
	defer captureSupervisor.Stop(context.Background(), &task)

	taskRepo := repository.NewTaskRepository(infraDB)
	leaseRepo := repository.NewRuntimeLeaseRepository(infraDB, repository.ContinuousRecoveryPolicy{
		InitialBackoff: time.Second, MaxBackoff: 4 * time.Second, MaxFailures: 3,
		CircuitOpenTime: 10 * time.Second, StabilityWindow: 30 * time.Second,
	})
	stateRepo := repository.NewSyncStateRepository(infraDB)
	execution := cdcDataExecution(task)
	if _, err := taskRepo.StartContinuousExecution(ctx, task.ID, task.TenantID, &execution); err != nil {
		t.Fatal(err)
	}
	claim, err := leaseRepo.ClaimNext(ctx, "cdc-worker-a", time.Now(), 30*time.Second)
	if err != nil || claim == nil {
		t.Fatalf("first claim=%#v err=%v", claim, err)
	}
	runner := &DataSessionRunner{
		Resolver: resolver, States: stateRepo, Progress: leaseRepo, Captures: captureRepo,
		InfraKafkaConnection: cdcDataTransferKafkaConnection(), PollTimeout: 500 * time.Millisecond,
		DiagnosticsInterval: time.Second,
		GetPlugin: func(engineType string) (engineplugin.EnginePlugin, error) {
			switch engineType {
			case "kafka":
				return &kafka.KafkaPlugin{}, nil
			case "postgresql":
				return &postgresql.PostgreSQLPlugin{}, nil
			default:
				return nil, fmt.Errorf("unexpected engine type %q", engineType)
			}
		},
	}

	firstCtx, cancelFirst := context.WithCancel(ctx)
	firstDone := make(chan error, 1)
	go func() { firstDone <- runner.Run(firstCtx, *claim) }()
	waitCDCDataRow(t, ctx, firstDone, businessDB, schema, targetTable, 1, "snapshot", true)
	assertCDCDataTypeMatrix(t, ctx, businessDB, schema, targetTable)
	if _, err := businessDB.ExecContext(ctx, `UPDATE `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(sourceTable)+`
		SET name='updated', shape=ST_GeomFromText('MULTIPOLYGON(((20 20,30 20,30 30,20 20)))', 4549) WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	waitCDCDataRow(t, ctx, firstDone, businessDB, schema, targetTable, 1, "updated", true)
	waitCDCDataGeometry(t, ctx, firstDone, businessDB, schema, targetTable, "MULTIPOLYGON(((20 20,30 20,30 30,20 20)))")
	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first runner error = %v", err)
	}
	if err := infraDB.Model(&models.RuntimeLease{}).Where("task_id = ?", task.ID).Updates(map[string]interface{}{
		"lease_until": time.Now().Add(-time.Second), "heartbeat_at": time.Now().Add(-time.Second),
	}).Error; err != nil {
		t.Fatal(err)
	}
	recoveryDetectedAt := time.Now()
	if recoveryClaim, err := leaseRepo.ClaimNext(ctx, "cdc-worker-b", recoveryDetectedAt, 30*time.Second); err != nil || recoveryClaim != nil {
		t.Fatalf("lease-expiry detection claim=%#v err=%v, want persisted backoff", recoveryClaim, err)
	}
	secondClaim, err := leaseRepo.ClaimNext(ctx, "cdc-worker-b", recoveryDetectedAt.Add(time.Second), 30*time.Second)
	if err != nil || secondClaim == nil || secondClaim.Lease.FencingToken <= claim.Lease.FencingToken {
		t.Fatalf("second claim=%#v err=%v", secondClaim, err)
	}
	secondCtx, cancelSecond := context.WithCancel(ctx)
	secondDone := make(chan error, 1)
	go func() { secondDone <- runner.Run(secondCtx, *secondClaim) }()
	if _, err := businessDB.ExecContext(ctx, `DELETE FROM `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(sourceTable)+` WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	waitCDCDataRow(t, ctx, secondDone, businessDB, schema, targetTable, 1, "", false)

	states, err := stateRepo.List(ctx, task.ID, resource.SourceIdentity)
	if err != nil || len(states) != 1 {
		t.Fatalf("sync states=%#v err=%v", states, err)
	}
	nextOffset, hasPosition, err := syncStatePosition(&states[0])
	if err != nil {
		t.Fatal(err)
	}
	if !hasPosition {
		t.Fatal("CDC sync state has no committed position")
	}
	committed, err := kafkaNextOffset(nextOffset)
	if err != nil || committed <= 0 {
		t.Fatalf("committed position=%#v err=%v", nextOffset, err)
	}
	var ledgerOffset int64
	if err := businessDB.QueryRowContext(ctx, `SELECT next_offset FROM addp_transfer.apply_positions WHERE apply_identity=$1::uuid AND partition='0'`, task.ApplyIdentity).Scan(&ledgerOffset); err != nil {
		t.Fatal(err)
	}
	if ledgerOffset != committed {
		t.Fatalf("target ledger=%d transfer committed=%d", ledgerOffset, committed)
	}
	if _, err := businessDB.ExecContext(ctx, `ALTER TABLE `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(sourceTable)+` ADD COLUMN schema_drift text`); err != nil {
		t.Fatal(err)
	}
	if _, err := businessDB.ExecContext(ctx, `INSERT INTO `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(sourceTable)+` (
		id, name, amount, business_date, business_time, changed_at, changed_at_tz, enabled, payload, ref, schema_drift
	) VALUES (
		2, 'blocked', 2.5000, DATE '2024-02-03', TIME '04:05:06.007',
		TIMESTAMP '2024-02-03 04:05:06.007', TIMESTAMPTZ '2024-02-03 04:05:06.007890+08', false,
		'{"blocked":true}'::jsonb, '550e8400-e29b-41d4-a716-446655440001'::uuid, 'new field'
	)`); err != nil {
		t.Fatal(err)
	}
	var runnerErr error
	select {
	case runnerErr = <-secondDone:
	case <-time.After(20 * time.Second):
		cancelSecond()
		t.Fatal("CDC runner did not block after source ALTER TABLE")
	}
	var schemaErr *SchemaChangeError
	if !errors.As(runnerErr, &schemaErr) || !containsTestString(schemaErr.UnexpectedFields, "schema_drift") {
		t.Fatalf("schema drift runner error = %v, schema=%#v", runnerErr, schemaErr)
	}
	statesAfterDrift, err := stateRepo.List(ctx, task.ID, resource.SourceIdentity)
	if err != nil || len(statesAfterDrift) != 1 {
		t.Fatalf("sync states after drift=%#v err=%v", statesAfterDrift, err)
	}
	afterDriftPosition, _, err := syncStatePosition(&statesAfterDrift[0])
	if err != nil {
		t.Fatal(err)
	}
	afterDriftCommitted, err := kafkaNextOffset(afterDriftPosition)
	if err != nil || afterDriftCommitted != committed {
		t.Fatalf("schema drift advanced committed position: before=%d after=%d err=%v", committed, afterDriftCommitted, err)
	}
	var targetRow2Exists bool
	if err := businessDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(targetTable)+` WHERE id=2)`).Scan(&targetRow2Exists); err != nil {
		t.Fatal(err)
	}
	if targetRow2Exists {
		t.Fatal("schema drift event was applied to target")
	}
	if err := leaseRepo.Finish(context.Background(), *secondClaim, commonExecution.ExecutionStatusFailed, "schema_change_blocked", runnerErr.Error(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var blockedTask models.TransferTask
	if err := infraDB.First(&blockedTask, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if blockedTask.Status != models.TaskStatusBlocked || blockedTask.DesiredState != models.TaskDesiredStateRunning {
		t.Fatalf("schema-blocked task status=%q desired=%q", blockedTask.Status, blockedTask.DesiredState)
	}
	var blockedExecution commonExecution.TaskExecution
	if err := infraDB.Where("execution_id = ?", secondClaim.Execution.ExecutionID).First(&blockedExecution).Error; err != nil {
		t.Fatal(err)
	}
	continuousMetadata, _ := blockedExecution.Metadata["continuous"].(map[string]interface{})
	schemaChange, _ := continuousMetadata["schema_change"].(map[string]interface{})
	if fmt.Sprint(schemaChange["unexpected_fields"]) != "[schema_drift]" || blockedExecution.Metadata["stop_reason"] != "schema_change_blocked" {
		t.Fatalf("schema-blocked execution metadata=%#v", blockedExecution.Metadata)
	}
	blockedRetry := cdcDataExecution(blockedTask)
	if _, err := taskRepo.StartContinuousExecution(ctx, task.ID, task.TenantID, &blockedRetry); !errors.Is(err, repository.ErrContinuousTaskBlocked) {
		t.Fatalf("restart blocked task error=%v, want ErrContinuousTaskBlocked", err)
	}
	cancelSecond()
}

func cdcDataTaskConfig(schema, sourceTable, targetTable string) models.JSONMap {
	return models.JSONMap{
		"runtime": map[string]interface{}{"boundary": "continuous"},
		"load": map[string]interface{}{
			"mode": "incremental", "change_detection": map[string]interface{}{"type": "cdc", "bootstrap": "initial_snapshot"},
		},
		"source": map[string]interface{}{
			"locator": fmt.Sprintf("addp://engine/12/path/%s/%s?type=table", schema, sourceTable), "data_type": "table", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": fmt.Sprintf("addp://engine/20/path/%s?type=schema", schema), "name": targetTable,
			"data_type": "table", "representation": "native", "policy": map[string]interface{}{"apply_mode": "upsert_delete", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{
				map[string]interface{}{"source": "id", "target": "id", "target_type": "bigint", "nullable": false},
				map[string]interface{}{"source": "name", "target": "name", "target_type": "string", "nullable": true},
				map[string]interface{}{"source": "amount", "target": "amount", "target_type": "decimal", "nullable": false},
				map[string]interface{}{"source": "business_date", "target": "business_date", "target_type": "date", "nullable": false},
				map[string]interface{}{"source": "business_time", "target": "business_time", "target_type": "time", "nullable": false},
				map[string]interface{}{"source": "changed_at", "target": "changed_at", "target_type": "timestamp", "nullable": false},
				map[string]interface{}{"source": "changed_at_tz", "target": "changed_at_tz", "target_type": "timestamp", "nullable": false},
				map[string]interface{}{"source": "enabled", "target": "enabled", "target_type": "bool", "nullable": false},
				map[string]interface{}{"source": "payload", "target": "payload", "target_type": "json", "nullable": false},
				map[string]interface{}{"source": "ref", "target": "ref", "target_type": "uuid", "nullable": false},
				map[string]interface{}{"source": "shape", "target": "geometry", "target_type": "geometry", "nullable": true},
			},
		}},
	}
}

func assertCDCDataTypeMatrix(t *testing.T, ctx context.Context, db *sql.DB, schema, table string) {
	t.Helper()
	var amount, businessDate, businessTime, changedAt, changedAtTZ, payload, ref, geometryType, geometryText string
	var geometrySRID int
	var enabled bool
	err := db.QueryRowContext(ctx, `
		SELECT amount::text, business_date::text, business_time::text,
		       to_char(changed_at, 'YYYY-MM-DD HH24:MI:SS.MS'),
		       to_char(changed_at_tz, 'YYYY-MM-DD HH24:MI:SS.US'),
		       enabled, payload::text, ref::text,
		       GeometryType(geometry), ST_SRID(geometry), ST_AsText(geometry)
		FROM `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(table)+` WHERE id=1
	`).Scan(&amount, &businessDate, &businessTime, &changedAt, &changedAtTZ, &enabled, &payload, &ref, &geometryType, &geometrySRID, &geometryText)
	if err != nil {
		t.Fatal(err)
	}
	if amount != "12345678901234567890.1234" || businessDate != "2024-01-02" || businessTime != "03:04:05.006" ||
		changedAt != "2024-01-02 03:04:05.006" || changedAtTZ != "2024-01-01 19:04:05.006789" || !enabled ||
		payload != `{"items": 2, "enabled": true}` || ref != "550e8400-e29b-41d4-a716-446655440000" ||
		geometryType != "MULTIPOLYGON" || geometrySRID != 4549 || geometryText != "MULTIPOLYGON(((0 0,10 0,10 10,0 0)))" {
		t.Fatalf("CDC type matrix mismatch: amount=%q date=%q time=%q ts=%q tstz=%q enabled=%v payload=%q ref=%q geometry=%s/%d/%q",
			amount, businessDate, businessTime, changedAt, changedAtTZ, enabled, payload, ref, geometryType, geometrySRID, geometryText)
	}
}

func waitCDCDataGeometry(t *testing.T, ctx context.Context, runnerDone <-chan error, db *sql.DB, schema, table, wantText string) {
	t.Helper()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		var geometryType, geometryText string
		var srid int
		err := db.QueryRowContext(ctx, `SELECT GeometryType(geometry), ST_SRID(geometry), ST_AsText(geometry) FROM `+
			pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(table)+` WHERE id=1`).Scan(&geometryType, &srid, &geometryText)
		if err == nil && geometryType == "MULTIPOLYGON" && srid == 4549 && geometryText == wantText {
			return
		}
		select {
		case err := <-runnerDone:
			t.Fatalf("CDC data runner exited before target geometry converged: %v", err)
		case <-ctx.Done():
			t.Fatalf("wait target geometry %q: %v", wantText, ctx.Err())
		case <-ticker.C:
		}
	}
}

func cdcDataExecution(task models.TransferTask) commonExecution.TaskExecution {
	now := time.Now()
	return commonExecution.TaskExecution{
		TenantID: int(task.TenantID), ExecutionID: uuid.NewString(), Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleTransfer,
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), SourceTaskName: &task.Name,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		ExecutionConfig: task.Config, CreatedAt: now, UpdatedAt: now,
	}
}

func waitCDCDataRow(t *testing.T, ctx context.Context, runnerDone <-chan error, db *sql.DB, schema, table string, id int64, wantName string, wantExists bool) {
	t.Helper()
	deadline := time.NewTicker(200 * time.Millisecond)
	defer deadline.Stop()
	for {
		var exists bool
		var name sql.NullString
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema=$1 AND table_name=$2),
			       (SELECT name FROM `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(table)+` WHERE id=$3)
		`, schema, table, id).Scan(&exists, &name)
		if err == nil && exists && (name.Valid == wantExists) && (!wantExists || name.String == wantName) {
			return
		}
		select {
		case err := <-runnerDone:
			t.Fatalf("CDC data runner exited before target row converged: %v", err)
		case <-ctx.Done():
			t.Fatalf("wait target row exists=%v name=%q: %v", wantExists, wantName, ctx.Err())
		case <-deadline.C:
		}
	}
}

func cleanupCDCDataInfraRows(db *gorm.DB, taskID uint) {
	var task models.TransferTask
	_ = db.First(&task, taskID).Error
	_ = db.Where("task_id = ?", taskID).Delete(&models.RuntimeLease{}).Error
	_ = db.Where("task_id = ?", taskID).Delete(&models.SyncState{}).Error
	_ = db.Where("task_id = ?", taskID).Delete(&models.CaptureResource{}).Error
	_ = db.Where("module = ? AND source_task_id = ?", commonExecution.ModuleTransfer, fmt.Sprint(taskID)).Delete(&commonExecution.TaskExecution{}).Error
	_ = db.Unscoped().Delete(&models.TransferTask{}, taskID).Error
}

func cdcDataBusinessPostgresConnInfo() engineplugin.ConnectionInfo {
	return engineplugin.ConnectionInfo{
		"host": cdcDataEnv("ADDP_TEST_BUSINESS_POSTGRES_HOST", "localhost"), "port": cdcDataEnv("ADDP_TEST_BUSINESS_POSTGRES_PORT", "5433"),
		"user": cdcDataEnv("ADDP_TEST_BUSINESS_POSTGRES_USER", "business"), "password": cdcDataEnv("ADDP_TEST_BUSINESS_POSTGRES_PASSWORD", "business_password"),
		"database": cdcDataEnv("ADDP_TEST_BUSINESS_POSTGRES_DATABASE", "business"), "sslmode": cdcDataEnv("ADDP_TEST_BUSINESS_POSTGRES_SSLMODE", "disable"),
	}
}

func cdcDataTransferKafkaConnection() engineplugin.ConnectionInfo {
	return engineplugin.ConnectionInfo{
		"bootstrap_servers": cdcDataEnv("ADDP_TEST_INFRA_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		"security_protocol": cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
		"username":          "transfer", "password": cdcDataEnv("ADDP_TEST_INFRA_KAFKA_TRANSFER_PASSWORD", "addp_kafka_transfer"),
		"sasl_mechanism": "plain", "client_id": "addp-transfer-cdc-data-e2e",
	}
}

func cdcDataEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func ptrEngineCapabilities(value engineplugin.EngineCapabilities) *engineplugin.EngineCapabilities {
	return &value
}
