package continuous

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/kafka"
	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/capture"
	transferconfig "github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/addp/transfer/internal/testpg"
	_ "github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationMySQLCDCDataPlaneViaPublicAPIFullLifecycle(t *testing.T) {
	if os.Getenv("ADDP_MYSQL_CDC_DATA_E2E") != "1" {
		t.Skip("set ADDP_MYSQL_CDC_DATA_E2E=1 to run MySQL CDC full-lifecycle integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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
	if err := repository.MigrateCaptureProviderResources(infraDB); err != nil {
		t.Fatalf("migrate capture resources: %v", err)
	}
	if err := infraDB.AutoMigrate(
		&models.TransferTask{}, &models.SyncState{}, &models.RuntimeLease{}, &models.CaptureResource{},
		&models.PostgreSQLCaptureResource{}, &models.MySQLCaptureResource{}, &models.SchemaChangeRequest{},
	); err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UnixNano()
	sourceDatabase := fmt.Sprintf("cdc_mysql_%d", suffix)
	sourceTable := "orders"
	targetSchema := fmt.Sprintf("cdc_mysql_target_%d", suffix)
	targetTable := "orders_target"

	rootInfo := mysqlCDCConnInfo(
		cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_ROOT_USER", "root"),
		cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_ROOT_PASSWORD", "password"),
		"mysql",
	)
	rootDB := openMySQLIntegrationDB(t, rootInfo)
	defer rootDB.Close()
	if _, err := rootDB.ExecContext(ctx, "CREATE DATABASE "+mysqlCDCQuoteIdentifier(sourceDatabase)); err != nil {
		t.Fatal(err)
	}
	defer rootDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+mysqlCDCQuoteIdentifier(sourceDatabase))
	if _, err := rootDB.ExecContext(ctx, "CREATE TABLE "+mysqlCDCQuoteIdentifier(sourceDatabase)+"."+mysqlCDCQuoteIdentifier(sourceTable)+` (
		id BIGINT NOT NULL PRIMARY KEY,
		name VARCHAR(255) NULL,
		amount DECIMAL(30,4) NOT NULL,
		business_date DATE NOT NULL,
		business_time TIME(3) NOT NULL,
		changed_at DATETIME(3) NOT NULL,
		changed_timestamp TIMESTAMP(3) NOT NULL,
		payload JSON NOT NULL,
		binary_payload BLOB NOT NULL,
		score DOUBLE NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := rootDB.ExecContext(ctx, "INSERT INTO "+mysqlCDCQuoteIdentifier(sourceDatabase)+"."+mysqlCDCQuoteIdentifier(sourceTable)+` VALUES (
		1, 'snapshot', 12345678901234567890.1234, '2024-01-02', '03:04:05.006',
		'2024-01-02 03:04:05.006', '2024-01-02 03:04:05.006', JSON_OBJECT('enabled', true, 'items', 2),
		X'0001FE', 12.5
	)`); err != nil {
		t.Fatal(err)
	}

	targetInfo := cdcDataBusinessPostgresConnInfo()
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
		t.Fatalf("business PostgreSQL is unavailable: %v", err)
	}
	if _, err := targetDB.ExecContext(ctx, `CREATE SCHEMA `+pq.QuoteIdentifier(targetSchema)); err != nil {
		t.Fatal(err)
	}
	defer targetDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+pq.QuoteIdentifier(targetSchema)+` CASCADE`)

	sourceInfo := mysqlCDCConnInfo(
		cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_CDC_USER", "addp_cdc"),
		cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_CDC_PASSWORD", "addp_cdc_password"),
		sourceDatabase,
	)
	resolver := planner.StaticEngineResolver{
		12: {Type: "mysql", EngineID: 12, ConnInfo: sourceInfo},
		20: {Type: "postgresql", EngineID: 20, ConnInfo: targetInfo, Capabilities: ptrEngineCapabilities((&postgresql.PostgreSQLPlugin{}).Capabilities())},
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
	capturePlanResolver := capture.NewDatabasePlanResolver(resolver)
	captureSupervisor, err := capture.NewSupervisor(
		captureRepo, capturePlanResolver, connectClient, topicAdmin,
		capture.DatabaseSourceResources{}, capture.SupervisorConfig{
			TopicRetention:               time.Hour,
			TopicReplication:             1,
			ConnectLoopbackHost:          cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_LOOPBACK_HOST", "host.docker.internal"),
			ConnectBootstrapServers:      cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_BOOTSTRAP_SERVERS", "kafka:29092"),
			ConnectKafkaUsername:         cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_USERNAME", "connect"),
			ConnectKafkaPassword:         cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_PASSWORD", "addp_kafka_connect"),
			ConnectKafkaSecurityProtocol: cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_SECURITY_PROTOCOL", "sasl_plaintext"),
			ProvisioningTimeout:          90 * time.Second, StatusPollInterval: 500 * time.Millisecond, MonitorInterval: time.Second,
		}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	leaseRepo := repository.NewRuntimeLeaseRepository(infraDB, repository.ContinuousRecoveryPolicy{
		InitialBackoff: time.Second, MaxBackoff: 4 * time.Second, MaxFailures: 3,
		CircuitOpenTime: 10 * time.Second, StabilityWindow: 30 * time.Second,
	})
	stateRepo := repository.NewSyncStateRepository(infraDB)
	executionService := service.NewExecutionService(infraDB, commonExecution.NewTaskExecutionRepository(infraDB))
	taskService := service.NewTaskService(infraDB, nil, &transferconfig.Config{
		ContinuousRuntimeStopTimeout: 5 * time.Second, ContinuousRuntimeStopPollInterval: 50 * time.Millisecond,
	}, nil)
	taskService.SetEngineResolver(resolver)
	taskService.SetExecutionService(executionService)
	taskService.SetCaptureControl(captureSupervisor)
	taskService.SetSchemaChangeInspector(capturePlanResolver)
	apiRouter := cdcDataAPIRouter(taskService, uint(800000+suffix%90000), 800001)
	task := cdcDataCreateTaskViaAPI(t, apiRouter, mysqlCDCDataTaskConfig(sourceDatabase, sourceTable, targetSchema, targetTable))
	defer cleanupCDCDataInfraRows(infraDB, task.ID)

	execution := cdcDataStartTaskViaAPI(t, apiRouter, task.ID)
	resource, err := captureRepo.GetLatest(ctx, task.ID, task.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if resource.SourceType != models.CaptureSourceMySQL || resource.MySQL == nil || resource.PostgreSQL != nil {
		t.Fatalf("MySQL capture provider facts = %#v", resource)
	}
	captureStopped := false
	defer func() {
		if !captureStopped {
			_ = captureSupervisor.Stop(context.Background(), &task)
		}
	}()

	claim, err := leaseRepo.ClaimNext(ctx, "mysql-cdc-worker-a", time.Now(), 30*time.Second)
	if err != nil || claim == nil || claim.Execution.ExecutionID != execution.ExecutionID {
		t.Fatalf("first claim=%#v err=%v", claim, err)
	}
	task.ApplyIdentity = claim.Task.ApplyIdentity
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
	waitCDCDataRow(t, ctx, firstDone, targetDB, targetSchema, targetTable, 1, "snapshot", true)
	assertMySQLCDCTypeMatrix(t, ctx, targetDB, targetSchema, targetTable)
	if _, err := rootDB.ExecContext(ctx, "INSERT INTO "+mysqlCDCQuoteIdentifier(sourceDatabase)+"."+mysqlCDCQuoteIdentifier(sourceTable)+` VALUES (
		2, 'inserted', 2.5000, '2024-02-03', '04:05:06.007', '2024-02-03 04:05:06.007',
		'2024-02-03 04:05:06.007', JSON_OBJECT('inserted', true), X'', 22.25
	)`); err != nil {
		t.Fatal(err)
	}
	waitCDCDataRow(t, ctx, firstDone, targetDB, targetSchema, targetTable, 2, "inserted", true)
	if _, err := rootDB.ExecContext(ctx, "UPDATE "+mysqlCDCQuoteIdentifier(sourceDatabase)+"."+mysqlCDCQuoteIdentifier(sourceTable)+" SET name='updated', binary_payload=X'CAFE' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	waitCDCDataRow(t, ctx, firstDone, targetDB, targetSchema, targetTable, 1, "updated", true)

	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first runner error=%v", err)
	}
	if err := infraDB.Model(&models.RuntimeLease{}).Where("task_id = ?", task.ID).Updates(map[string]interface{}{
		"lease_until": time.Now().Add(-time.Second), "heartbeat_at": time.Now().Add(-time.Second),
	}).Error; err != nil {
		t.Fatal(err)
	}
	recoveryDetectedAt := time.Now()
	if recoveryClaim, err := leaseRepo.ClaimNext(ctx, "mysql-cdc-worker-b", recoveryDetectedAt, 30*time.Second); err != nil || recoveryClaim != nil {
		t.Fatalf("recovery backoff claim=%#v err=%v", recoveryClaim, err)
	}
	secondClaim, err := leaseRepo.ClaimNext(ctx, "mysql-cdc-worker-b", recoveryDetectedAt.Add(time.Second), 30*time.Second)
	if err != nil || secondClaim == nil || secondClaim.Lease.FencingToken <= claim.Lease.FencingToken {
		t.Fatalf("recovery claim=%#v err=%v", secondClaim, err)
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- runner.Run(ctx, *secondClaim) }()
	if _, err := rootDB.ExecContext(ctx, "DELETE FROM "+mysqlCDCQuoteIdentifier(sourceDatabase)+"."+mysqlCDCQuoteIdentifier(sourceTable)+" WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	waitCDCDataRow(t, ctx, secondDone, targetDB, targetSchema, targetTable, 2, "", false)

	mysqlCDCPauseTaskViaAPI(t, apiRouter, task.ID)
	pauseErr := waitMySQLCDCRunnerExit(t, secondDone, 10*time.Second)
	if pauseErr == nil {
		t.Fatal("paused MySQL CDC runner exited without fencing error")
	}
	if err := leaseRepo.Finish(context.Background(), *secondClaim, commonExecution.ExecutionStatusCancelled, "paused", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := rootDB.ExecContext(ctx, "INSERT INTO "+mysqlCDCQuoteIdentifier(sourceDatabase)+"."+mysqlCDCQuoteIdentifier(sourceTable)+` VALUES (
		3, 'paused-backlog', 3.5000, '2024-03-04', '05:06:07.008', '2024-03-04 05:06:07.008',
		'2024-03-04 05:06:07.008', JSON_OBJECT('paused', true), X'03', 33.5
	)`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(750 * time.Millisecond)
	assertMySQLCDCTargetRowAbsent(t, ctx, targetDB, targetSchema, targetTable, 3)

	resumeExecution := mysqlCDCResumeTaskViaAPI(t, apiRouter, task.ID)
	thirdClaim, err := leaseRepo.ClaimNext(ctx, "mysql-cdc-worker-c", time.Now(), 30*time.Second)
	if err != nil || thirdClaim == nil || thirdClaim.Execution.ExecutionID != resumeExecution.ExecutionID {
		t.Fatalf("resume claim=%#v err=%v", thirdClaim, err)
	}
	thirdDone := make(chan error, 1)
	go func() { thirdDone <- runner.Run(ctx, *thirdClaim) }()
	waitCDCDataRow(t, ctx, thirdDone, targetDB, targetSchema, targetTable, 3, "paused-backlog", true)

	committedBeforeDrift := mysqlCDCCommittedOffset(t, ctx, stateRepo, task.ID, resource.SourceIdentity)
	var ledgerOffset int64
	if err := targetDB.QueryRowContext(ctx, `
		SELECT next_offset FROM addp_transfer.apply_positions
		WHERE apply_identity=$1::uuid AND partition='0'`, task.ApplyIdentity).Scan(&ledgerOffset); err != nil {
		t.Fatal(err)
	}
	if ledgerOffset != committedBeforeDrift {
		t.Fatalf("MySQL CDC target ledger=%d Transfer committed=%d", ledgerOffset, committedBeforeDrift)
	}
	if _, err := rootDB.ExecContext(ctx, "ALTER TABLE "+mysqlCDCQuoteIdentifier(sourceDatabase)+"."+mysqlCDCQuoteIdentifier(sourceTable)+" ADD COLUMN schema_drift VARCHAR(64) NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := rootDB.ExecContext(ctx, "INSERT INTO "+mysqlCDCQuoteIdentifier(sourceDatabase)+"."+mysqlCDCQuoteIdentifier(sourceTable)+` (
		id, name, amount, business_date, business_time, changed_at, changed_timestamp, payload, binary_payload, score, schema_drift
	) VALUES (4, 'blocked', 4.5000, '2024-04-05', '06:07:08.009', '2024-04-05 06:07:08.009',
		'2024-04-05 06:07:08.009', JSON_OBJECT('blocked', true), X'04', 44.5, 'new-field')`); err != nil {
		t.Fatal(err)
	}
	runnerErr := waitMySQLCDCRunnerExit(t, thirdDone, 20*time.Second)
	var schemaErr *SchemaChangeError
	if !errors.As(runnerErr, &schemaErr) || !containsTestString(schemaErr.UnexpectedFields, "schema_drift") {
		t.Fatalf("MySQL schema drift error=%v schema=%#v", runnerErr, schemaErr)
	}
	if got := mysqlCDCCommittedOffset(t, ctx, stateRepo, task.ID, resource.SourceIdentity); got != committedBeforeDrift {
		t.Fatalf("schema drift advanced committed position: before=%d after=%d", committedBeforeDrift, got)
	}
	assertMySQLCDCTargetRowAbsent(t, ctx, targetDB, targetSchema, targetTable, 4)
	if err := leaseRepo.Finish(context.Background(), *thirdClaim, commonExecution.ExecutionStatusFailed, "schema_change_blocked", runnerErr.Error(), time.Now()); err != nil {
		t.Fatal(err)
	}

	change := cdcDataGetSchemaChangeViaAPI(t, apiRouter, task.ID)
	if !change.Approvable || len(change.SuggestedFields) != 1 ||
		change.SuggestedFields[0].Source != "schema_drift" || change.SuggestedFields[0].TargetType != "string" {
		t.Fatalf("MySQL additive schema request=%#v", change)
	}
	change = cdcDataApproveSchemaChangeViaAPI(t, apiRouter, task.ID, models.SchemaChangeField{
		Source: "schema_drift", Target: "schema_drift", TargetType: "string", Nullable: true,
	})
	if change.Status != models.SchemaChangeRequestApplied || change.MetadataScanStatus != "failed" {
		t.Fatalf("applied MySQL schema request=%#v", change)
	}
	repeatedChange := cdcDataApproveSchemaChangeViaAPI(t, apiRouter, task.ID, models.SchemaChangeField{
		Source: "schema_drift", Target: "schema_drift", TargetType: "string", Nullable: true,
	})
	if repeatedChange.ID != change.ID || repeatedChange.MetadataScanStatus != models.SchemaChangeMetadataScanFailed ||
		repeatedChange.MetadataScanAttempt != 1 {
		t.Fatalf("failed Meta scan was retried by repeated approval: first=%#v repeated=%#v", change, repeatedChange)
	}
	fourthExecution := mysqlCDCResumeTaskViaAPI(t, apiRouter, task.ID)
	fourthClaim, err := leaseRepo.ClaimNext(ctx, "mysql-cdc-worker-d", time.Now(), 30*time.Second)
	if err != nil || fourthClaim == nil || fourthClaim.Execution.ExecutionID != fourthExecution.ExecutionID {
		t.Fatalf("schema resume claim=%#v err=%v", fourthClaim, err)
	}
	fourthCtx, cancelFourth := context.WithCancel(ctx)
	fourthDone := make(chan error, 1)
	go func() { fourthDone <- runner.Run(fourthCtx, *fourthClaim) }()
	waitCDCDataRow(t, ctx, fourthDone, targetDB, targetSchema, targetTable, 4, "blocked", true)
	var additiveValue string
	if err := targetDB.QueryRowContext(ctx, `SELECT schema_drift FROM `+pq.QuoteIdentifier(targetSchema)+`.`+pq.QuoteIdentifier(targetTable)+` WHERE id=4`).Scan(&additiveValue); err != nil || additiveValue != "new-field" {
		t.Fatalf("resumed MySQL additive value=%q err=%v", additiveValue, err)
	}
	if _, err := rootDB.ExecContext(ctx, "INSERT INTO "+mysqlCDCQuoteIdentifier(sourceDatabase)+"."+mysqlCDCQuoteIdentifier(sourceTable)+` (
		id, name, amount, business_date, business_time, changed_at, changed_timestamp, payload, binary_payload, score, schema_drift
	) VALUES (5, 'after-additive', 5.5000, '2024-05-06', '07:08:09.010', '2024-05-06 07:08:09.010',
		'2024-05-06 07:08:09.010', JSON_OBJECT('after', true), X'05', 55.5, 'continued')`); err != nil {
		t.Fatal(err)
	}
	waitCDCDataRow(t, ctx, fourthDone, targetDB, targetSchema, targetTable, 5, "after-additive", true)
	mysqlCDCPauseTaskViaAPI(t, apiRouter, task.ID)
	cancelFourth()
	if err := <-fourthDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("MySQL additive runner error=%v", err)
	}
	if err := leaseRepo.Finish(context.Background(), *fourthClaim, commonExecution.ExecutionStatusCancelled, "paused", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	committedAfterAdditive := mysqlCDCCommittedOffset(t, ctx, stateRepo, task.ID, resource.SourceIdentity)
	if _, err := rootDB.ExecContext(ctx, "INSERT INTO "+mysqlCDCQuoteIdentifier(sourceDatabase)+"."+mysqlCDCQuoteIdentifier(sourceTable)+` (
		id, name, amount, business_date, business_time, changed_at, changed_timestamp, payload, binary_payload, score, schema_drift
	) VALUES (6, 'retention-backlog', 6.5000, '2024-06-07', '08:09:10.011', '2024-06-07 08:09:10.011',
		'2024-06-07 08:09:10.011', JSON_OBJECT('retention', true), X'06', 66.5, 'backlog')`); err != nil {
		t.Fatal(err)
	}

	adminClient, err := mysqlCDCKafkaAdminClient()
	if err != nil {
		t.Fatal(err)
	}
	defer adminClient.Close()
	admin := kadm.NewClient(adminClient)
	mysqlCDCExpireCommittedPosition(t, ctx, admin, resource.TopicName, committedAfterAdditive)
	fifthExecution := mysqlCDCResumeTaskViaAPI(t, apiRouter, task.ID)
	fifthClaim, err := leaseRepo.ClaimNext(ctx, "mysql-cdc-worker-e", time.Now(), 30*time.Second)
	if err != nil || fifthClaim == nil || fifthClaim.Execution.ExecutionID != fifthExecution.ExecutionID {
		t.Fatalf("retention claim=%#v err=%v", fifthClaim, err)
	}
	retentionErr := runner.Run(ctx, *fifthClaim)
	if retentionErr == nil || !strings.Contains(retentionErr.Error(), "no longer retained") {
		t.Fatalf("expired MySQL CDC committed position error=%v", retentionErr)
	}
	mysqlCDCPauseTaskViaAPI(t, apiRouter, task.ID)
	if err := leaseRepo.Finish(context.Background(), *fifthClaim, commonExecution.ExecutionStatusFailed, "retention_unavailable", retentionErr.Error(), time.Now()); err != nil {
		t.Fatal(err)
	}

	cdcDataStopTaskViaAPI(t, apiRouter, task.ID, task.Name)
	captureStopped = true
	assertMySQLCDCCleanup(t, ctx, captureRepo, connectClient, admin, task, resource)
}

func mysqlCDCDataTaskConfig(sourceDatabase, sourceTable, targetSchema, targetTable string) models.JSONMap {
	fields := []interface{}{
		map[string]interface{}{"source": "id", "target": "id", "target_type": "bigint", "nullable": false},
		map[string]interface{}{"source": "name", "target": "name", "target_type": "string", "nullable": true},
		map[string]interface{}{"source": "amount", "target": "amount", "target_type": "decimal", "nullable": false},
		map[string]interface{}{"source": "business_date", "target": "business_date", "target_type": "date", "nullable": false},
		map[string]interface{}{"source": "business_time", "target": "business_time", "target_type": "time", "nullable": false},
		map[string]interface{}{"source": "changed_at", "target": "changed_at", "target_type": "timestamp", "nullable": false},
		map[string]interface{}{"source": "changed_timestamp", "target": "changed_timestamp", "target_type": "timestamp", "nullable": false},
		map[string]interface{}{"source": "payload", "target": "payload", "target_type": "json", "nullable": false},
		map[string]interface{}{"source": "binary_payload", "target": "binary_payload", "target_type": "bytes", "nullable": false},
		map[string]interface{}{"source": "score", "target": "score", "target_type": "double", "nullable": false},
	}
	return models.JSONMap{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
		"load":    map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{"type": "cdc", "bootstrap": "initial_snapshot"}},
		"source": map[string]interface{}{
			"locator":   fmt.Sprintf("addp://engine/12/path/%s/%s?type=table", sourceDatabase, sourceTable),
			"data_type": "table", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": fmt.Sprintf("addp://engine/20/path/%s?type=schema", targetSchema), "name": targetTable,
			"data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert_delete", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{"type": "field_mapping", "version": "v1", "mode": "project", "fields": fields}},
	}
}

func mysqlCDCConnInfo(user, password, database string) engineplugin.ConnectionInfo {
	return engineplugin.ConnectionInfo{
		"host": cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_HOST", "localhost"),
		"port": cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_PORT", "3306"),
		"user": user, "password": password, "database": database,
	}
}

func openMySQLIntegrationDB(t *testing.T, info engineplugin.ConnectionInfo) *sql.DB {
	t.Helper()
	dsn, err := engineplugin.BuildMySQLCompatibleDSN(info, 3306, "MySQL", map[string]string{"parseTime": "false"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("business MySQL is unavailable: %v", err)
	}
	return db
}

func mysqlCDCQuoteIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func assertMySQLCDCTypeMatrix(t *testing.T, ctx context.Context, db *sql.DB, schema, table string) {
	t.Helper()
	var amount, date, businessTime, changedAt, changedTimestamp, payload, binaryHex string
	var score float64
	err := db.QueryRowContext(ctx, `
		SELECT amount::text, business_date::text, business_time::text,
		       to_char(changed_at, 'YYYY-MM-DD HH24:MI:SS.MS'),
		       to_char(changed_timestamp, 'YYYY-MM-DD HH24:MI:SS.MS'),
		       payload::text, encode(binary_payload, 'hex'), score
		FROM `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(table)+` WHERE id=1`).
		Scan(&amount, &date, &businessTime, &changedAt, &changedTimestamp, &payload, &binaryHex, &score)
	if err != nil {
		t.Fatal(err)
	}
	if amount != "12345678901234567890.1234" || date != "2024-01-02" || businessTime != "03:04:05.006" ||
		changedAt != "2024-01-02 03:04:05.006" || changedTimestamp != "2024-01-02 03:04:05.006" ||
		!strings.Contains(payload, `"items": 2`) || binaryHex != "0001fe" || score != 12.5 {
		t.Fatalf("MySQL CDC type matrix mismatch: amount=%q date=%q time=%q datetime=%q timestamp=%q payload=%q bytes=%q score=%v",
			amount, date, businessTime, changedAt, changedTimestamp, payload, binaryHex, score)
	}
}

func mysqlCDCPauseTaskViaAPI(t *testing.T, router http.Handler, taskID uint) {
	t.Helper()
	response := cdcDataAPIRequest(t, router, "POST", fmt.Sprintf("/api/v1/transfer/task-definitions/%d/pause", taskID), nil)
	if response.Code != 200 {
		t.Fatalf("pause MySQL CDC task status=%d body=%s", response.Code, response.Body.String())
	}
}

func mysqlCDCResumeTaskViaAPI(t *testing.T, router http.Handler, taskID uint) models.TaskExecution {
	t.Helper()
	response := cdcDataAPIRequest(t, router, "POST", fmt.Sprintf("/api/v1/transfer/task-definitions/%d/resume", taskID), nil)
	if response.Code != 200 {
		t.Fatalf("resume MySQL CDC task status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Execution models.TaskExecution `json:"execution"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Execution.ExecutionID == "" {
		t.Fatalf("resume MySQL CDC response=%s", response.Body.String())
	}
	return payload.Execution
}

func waitMySQLCDCRunnerExit(t *testing.T, done <-chan error, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		t.Fatal("MySQL CDC runner did not exit before timeout")
		return nil
	}
}

func assertMySQLCDCTargetRowAbsent(t *testing.T, ctx context.Context, db *sql.DB, schema, table string, id int64) {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(table)+` WHERE id=$1)`, id).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("target row id=%d unexpectedly exists", id)
	}
}

func mysqlCDCCommittedOffset(t *testing.T, ctx context.Context, states *repository.SyncStateRepository, taskID uint, sourceIdentity string) int64 {
	t.Helper()
	values, err := states.List(ctx, taskID, sourceIdentity)
	if err != nil || len(values) != 1 {
		t.Fatalf("MySQL CDC sync states=%#v err=%v", values, err)
	}
	position, ok, err := syncStatePosition(&values[0])
	if err != nil || !ok {
		t.Fatalf("MySQL CDC committed position=%#v ok=%v err=%v", position, ok, err)
	}
	offset, err := kafkaNextOffset(position)
	if err != nil {
		t.Fatal(err)
	}
	return offset
}

func mysqlCDCKafkaAdminClient() (*kgo.Client, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(strings.Split(cdcDataEnv("ADDP_TEST_INFRA_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"), ",")...),
		kgo.ClientID("addp-transfer-mysql-cdc-e2e-admin"),
	}
	if cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext") == "sasl_plaintext" {
		username := cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_USERNAME", "admin")
		password := cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_PASSWORD", "addp_kafka_admin")
		opts = append(opts, kgo.SASL(plain.Plain(func(context.Context) (plain.Auth, error) {
			return plain.Auth{User: username, Pass: password}, nil
		})))
	}
	return kgo.NewClient(opts...)
}

func mysqlCDCExpireCommittedPosition(t *testing.T, ctx context.Context, admin *kadm.Client, topic string, committed int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var end kadm.ListedOffset
	for {
		ends, err := admin.ListEndOffsets(ctx, topic)
		listed, ok := ends.Lookup(topic, 0)
		if err == nil && ok && listed.Err == nil && listed.Offset > committed {
			end = listed
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("MySQL CDC end offset=%#v ok=%v err=%v committed=%d", listed, ok, err, committed)
		}
		time.Sleep(100 * time.Millisecond)
	}
	deleted, err := admin.DeleteRecords(ctx, kadm.OffsetsList{{Topic: topic, Partition: 0, At: end.Offset}}.Offsets())
	if err != nil || deleted.Error() != nil {
		t.Fatalf("expire MySQL CDC records: response=%v err=%v", deleted.Error(), err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		starts, listErr := admin.ListStartOffsets(ctx, topic)
		start, found := starts.Lookup(topic, 0)
		if listErr == nil && found && start.Err == nil && start.Offset > committed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("MySQL CDC earliest offset did not pass committed=%d: start=%#v err=%v", committed, start, listErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func assertMySQLCDCCleanup(
	t *testing.T,
	ctx context.Context,
	captures *repository.CaptureRepository,
	connectClient *capture.ConnectClient,
	admin *kadm.Client,
	task models.TransferTask,
	resource *models.CaptureResource,
) {
	t.Helper()
	stopped, err := captures.GetLatest(ctx, task.ID, task.TenantID)
	if err != nil || stopped.Status != models.CaptureStatusStopped {
		t.Fatalf("stopped MySQL capture=%#v err=%v", stopped, err)
	}
	if _, err := connectClient.Status(ctx, resource.ConnectorName); !errors.Is(err, capture.ErrConnectorNotFound) {
		t.Fatalf("MySQL connector still exists: %v", err)
	}
	if resource.MySQL == nil || resource.PostgreSQL != nil {
		t.Fatalf("unexpected MySQL provider resources=%#v", resource)
	}
	details, err := admin.ListTopics(ctx, resource.TopicName, resource.MySQL.SchemaHistoryTopicName)
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{resource.TopicName, resource.MySQL.SchemaHistoryTopicName} {
		if detail, ok := details[topic]; ok && detail.Err == nil {
			t.Fatalf("MySQL CDC topic %q still exists", topic)
		}
	}
	groups, err := admin.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := groups[resource.ConsumerGroup]; ok {
		t.Fatalf("MySQL CDC consumer group %q still exists", resource.ConsumerGroup)
	}
	aclResults, err := admin.DescribeACLs(ctx,
		kadm.NewACLs().Topics(resource.TopicName, resource.MySQL.SchemaHistoryTopicName).Groups(resource.ConsumerGroup).
			ResourcePatternType(kadm.ACLPatternLiteral).Allow().AllowHosts().Operations(kadm.OpAny),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range aclResults {
		if len(result.Described) != 0 {
			t.Fatalf("MySQL CDC ACLs remain after stop: %+v", result.Described)
		}
	}
}
