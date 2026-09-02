package continuous

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/kafka"
	mysqlplugin "github.com/addp/common/engine/plugins/mysql"
	oracleplugin "github.com/addp/common/engine/plugins/oracle"
	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/capture"
	transferconfig "github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/addp/transfer/internal/testpg"
	"github.com/lib/pq"
	"github.com/twmb/franz-go/pkg/kadm"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationDatabaseCDCToOracleTargetLifecycle(t *testing.T) {
	if os.Getenv("ADDP_DATABASE_CDC_ORACLE_TARGET_E2E") != "1" {
		t.Skip("set ADDP_DATABASE_CDC_ORACLE_TARGET_E2E=1 to run PostgreSQL/MySQL CDC to Oracle target integration tests")
	}
	for _, sourceType := range []string{"postgresql", "mysql"} {
		t.Run("source="+sourceType, func(t *testing.T) {
			runIntegrationDatabaseCDCToOracleTargetLifecycle(t, sourceType)
		})
	}
}

func runIntegrationDatabaseCDCToOracleTargetLifecycle(t *testing.T, sourceType string) {
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
		t.Fatal(err)
	}
	if err := infraDB.AutoMigrate(
		&models.TransferTask{}, &models.SyncState{}, &models.RuntimeLease{}, &models.CaptureResource{},
		&models.PostgreSQLCaptureResource{}, &models.MySQLCaptureResource{}, &models.OracleCaptureResource{}, &models.SchemaChangeRequest{},
	); err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UnixNano()
	sourceNamespace := fmt.Sprintf("cdc_%s_oracle_%d", sourceType, suffix)
	sourceTable := "orders"
	sourceInfo, sourceDB := prepareOracleTargetCDCSource(t, ctx, sourceType, sourceNamespace, sourceTable)
	target := openCDCDataE2ETarget(t, ctx, "oracle", "BUSINESS", "ignored", nil, nil)
	defer target.Close()

	resolver := planner.StaticEngineResolver{
		12: {Type: sourceType, EngineID: 12, ConnInfo: sourceInfo},
		target.EngineID: {
			Type: target.Type, EngineID: target.EngineID, ConnInfo: target.ConnInfo,
			Capabilities: ptrEngineCapabilities(target.Plugin.Capabilities()),
		},
	}
	captureRepo := repository.NewCaptureRepository(infraDB)
	topicAdmin, err := capture.NewKafkaTopicAdmin(capture.KafkaAdminConfig{
		BootstrapServers: cdcDataEnv("ADDP_TEST_INFRA_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		Username:         cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_USERNAME", "admin"),
		Password:         cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_PASSWORD", "addp_kafka_admin"),
		SecurityProtocol: cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
		SASLMechanism:    cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"),
		TLSCACertFile:    cdcDataEnv("ADDP_TEST_INFRA_KAFKA_TLS_CA_CERT_FILE", ""),
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
			TopicReplication:             cdcDataEnvInt16("ADDP_TEST_INFRA_KAFKA_REPLICATION_FACTOR", 1),
			ConnectLoopbackHost:          cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_LOOPBACK_HOST", "host.docker.internal"),
			ConnectBootstrapServers:      cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_BOOTSTRAP_SERVERS", "redpanda:29092"),
			ConnectKafkaUsername:         cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_USERNAME", "connect"),
			ConnectKafkaPassword:         cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_PASSWORD", "addp_kafka_connect"),
			ConnectKafkaSecurityProtocol: cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_SECURITY_PROTOCOL", "sasl_plaintext"),
			ConnectKafkaSASLMechanism:    cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"),
			ConnectKafkaTLSCACertFile:    cdcDataEnv("ADDP_TEST_INFRA_KAFKA_TLS_CA_CERT_FILE", ""),
			ProvisioningTimeout:          90 * time.Second,
			StatusPollInterval:           500 * time.Millisecond,
			MonitorInterval:              time.Second,
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
	})
	taskService.SetEngineResolver(resolver)
	taskService.SetExecutionService(executionService)
	taskService.SetCaptureControl(captureSupervisor)
	taskService.SetSchemaChangeInspector(capturePlanResolver)
	apiRouter := cdcDataAPIRouter(t, taskService, uint(940000+suffix%50000), 940001)
	task := cdcDataCreateTaskViaAPI(t, apiRouter, databaseCDCToOracleTargetTaskConfig(sourceNamespace, sourceTable, target))
	defer cleanupCDCDataInfraRows(infraDB, task.ID)
	if err := infraDB.Model(&models.TransferTask{}).Where("id = ?", task.ID).Update("auto_scan_metadata", false).Error; err != nil {
		t.Fatal(err)
	}

	execution := cdcDataStartTaskViaAPI(t, apiRouter, task.ID)
	resource, err := captureRepo.GetLatest(ctx, task.ID, task.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	captureStopped := false
	defer func() {
		if !captureStopped {
			_ = captureSupervisor.Stop(context.Background(), &task)
		}
	}()
	claim, err := leaseRepo.ClaimNext(ctx, sourceType+"-oracle-target-worker", time.Now(), 2*time.Minute)
	if err != nil || claim == nil || claim.Execution.ExecutionID != execution.ExecutionID {
		t.Fatalf("%s CDC claim=%#v err=%v", sourceType, claim, err)
	}
	task.ApplyIdentity = claim.Task.ApplyIdentity
	runner := &DataSessionRunner{
		ProtectionGate: allowSourceProtectionGate{},
		Resolver:       resolver, States: stateRepo, Progress: leaseRepo, Captures: captureRepo,
		InfraKafkaConnection: cdcDataTransferKafkaConnection(), PollTimeout: 500 * time.Millisecond,
		DiagnosticsInterval: time.Second,
		GetPlugin: func(engineType string) (engineplugin.EnginePlugin, error) {
			switch engineType {
			case "kafka":
				return &kafka.KafkaPlugin{}, nil
			case "postgresql":
				return &postgresql.PostgreSQLPlugin{}, nil
			case "mysql":
				return &mysqlplugin.MySQLPlugin{}, nil
			case "oracle":
				return &oracleplugin.OraclePlugin{}, nil
			default:
				return nil, fmt.Errorf("unexpected engine type %q", engineType)
			}
		},
	}
	runnerCtx, cancelRunner := context.WithCancel(ctx)
	runnerDone := make(chan error, 1)
	go func() { runnerDone <- runner.Run(runnerCtx, *claim) }()
	target.WaitRow(t, ctx, runnerDone, 1, "snapshot", true)
	assertDatabaseCDCOracleTargetValues(t, ctx, target, 1, "snapshot", "12345678901234567890.1234", "2024-01-02", "2024-01-02 03:04:05.006", 12.5)

	execDatabaseCDCSourceMutation(t, ctx, sourceType, sourceDB, sourceNamespace, sourceTable, "insert")
	target.WaitRow(t, ctx, runnerDone, 2, "inserted", true)
	execDatabaseCDCSourceMutation(t, ctx, sourceType, sourceDB, sourceNamespace, sourceTable, "update")
	target.WaitRow(t, ctx, runnerDone, 1, "updated", true)
	assertDatabaseCDCOracleTargetValues(t, ctx, target, 1, "updated", "98765432109876543210.4321", "2024-01-02", "2024-02-03 04:05:06.007", 99.25)
	execDatabaseCDCSourceMutation(t, ctx, sourceType, sourceDB, sourceNamespace, sourceTable, "delete")
	target.WaitRow(t, ctx, runnerDone, 2, "", false)

	committed := mysqlCDCCommittedOffset(t, ctx, stateRepo, task.ID, resource.SourceIdentity)
	if ledger := target.LedgerOffset(t, ctx, task.ApplyIdentity); ledger != committed {
		t.Fatalf("%s CDC Oracle target ledger=%d Transfer committed=%d", sourceType, ledger, committed)
	}
	cancelRunner()
	if err := <-runnerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("%s CDC Oracle target runner error=%v", sourceType, err)
	}
	if err := leaseRepo.Finish(context.Background(), *claim, commonExecution.ExecutionStatusCancelled, "test_complete", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	cdcDataStopTaskViaAPI(t, apiRouter, task.ID, task.Name)
	captureStopped = true
	if sourceType == "postgresql" {
		assertCDCDataCaptureCleanup(t, ctx, sourceDB, captureRepo, connectClient, task, resource)
	} else {
		adminClient, err := mysqlCDCKafkaAdminClient()
		if err != nil {
			t.Fatal(err)
		}
		defer adminClient.Close()
		assertMySQLCDCCleanup(t, ctx, captureRepo, connectClient, kadm.NewClient(adminClient), task, resource)
	}
}

func prepareOracleTargetCDCSource(t *testing.T, ctx context.Context, sourceType, namespace, table string) (engineplugin.ConnectionInfo, *sql.DB) {
	t.Helper()
	if sourceType == "postgresql" {
		info := cdcDataBusinessPostgresConnInfo()
		dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(info)
		if err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+pq.QuoteIdentifier(namespace)); err != nil {
			db.Close()
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+pq.QuoteIdentifier(namespace)+` CASCADE`)
			_ = db.Close()
		})
		if _, err := db.ExecContext(ctx, `CREATE TABLE `+pq.QuoteIdentifier(namespace)+`.`+pq.QuoteIdentifier(table)+` (
			id bigint PRIMARY KEY, name text, amount numeric(30,4) NOT NULL, business_date date NOT NULL,
			changed_at timestamp(3) without time zone NOT NULL, payload jsonb NOT NULL, score double precision NOT NULL
		)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO `+pq.QuoteIdentifier(namespace)+`.`+pq.QuoteIdentifier(table)+` VALUES (
			1, 'snapshot', 12345678901234567890.1234, DATE '2024-01-02', TIMESTAMP '2024-01-02 03:04:05.006',
			'{"enabled":true,"items":2}'::jsonb, 12.5
		)`); err != nil {
			t.Fatal(err)
		}
		return info, db
	}
	rootInfo := mysqlCDCConnInfo(
		cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_ROOT_USER", "root"),
		cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_ROOT_PASSWORD", "password"), "mysql",
	)
	rootDB := openMySQLIntegrationDB(t, rootInfo)
	if _, err := rootDB.ExecContext(ctx, "CREATE DATABASE "+mysqlCDCQuoteIdentifier(namespace)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		rootDB.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = rootDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+mysqlCDCQuoteIdentifier(namespace))
		_ = rootDB.Close()
	})
	if _, err := rootDB.ExecContext(ctx, "CREATE TABLE "+mysqlCDCQuoteIdentifier(namespace)+"."+mysqlCDCQuoteIdentifier(table)+` (
		id BIGINT NOT NULL PRIMARY KEY, name VARCHAR(255) NULL, amount DECIMAL(30,4) NOT NULL,
		business_date DATE NOT NULL, changed_at DATETIME(3) NOT NULL, payload JSON NOT NULL, score DOUBLE NOT NULL
	)`); err != nil {
		rootDB.Close()
		t.Fatal(err)
	}
	if _, err := rootDB.ExecContext(ctx, "INSERT INTO "+mysqlCDCQuoteIdentifier(namespace)+"."+mysqlCDCQuoteIdentifier(table)+` VALUES (
		1, 'snapshot', 12345678901234567890.1234, '2024-01-02', '2024-01-02 03:04:05.006',
		JSON_OBJECT('enabled', true, 'items', 2), 12.5
	)`); err != nil {
		rootDB.Close()
		t.Fatal(err)
	}
	return mysqlCDCConnInfo(
		cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_CDC_USER", "addp_cdc"),
		cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_CDC_PASSWORD", "addp_cdc_password"), namespace,
	), rootDB
}

func databaseCDCToOracleTargetTaskConfig(namespace, table string, target *cdcDataE2ETarget) models.JSONMap {
	return models.JSONMap{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
		"load":    map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{"type": "cdc", "bootstrap": "initial_snapshot"}},
		"source": map[string]interface{}{
			"locator": fmt.Sprintf("addp://engine/12/path/%s/%s?type=table", namespace, table), "data_type": "table", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": target.ParentLocator(), "name": target.Table, "data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert_delete", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{
				map[string]interface{}{"source": "id", "target": "id", "target_type": "bigint", "nullable": false},
				map[string]interface{}{"source": "name", "target": "name", "target_type": "string", "nullable": true},
				map[string]interface{}{"source": "amount", "target": "amount", "target_type": "decimal", "precision": 30, "scale": 4, "nullable": false},
				map[string]interface{}{"source": "business_date", "target": "business_date", "target_type": "date", "nullable": false},
				map[string]interface{}{"source": "changed_at", "target": "changed_at", "target_type": "timestamp", "nullable": false},
				map[string]interface{}{"source": "payload", "target": "payload", "target_type": "json", "nullable": false},
				map[string]interface{}{"source": "score", "target": "score", "target_type": "double", "nullable": false},
			},
		}},
	}
}

func execDatabaseCDCSourceMutation(t *testing.T, ctx context.Context, sourceType string, db *sql.DB, namespace, table, operation string) {
	t.Helper()
	var statement string
	if sourceType == "postgresql" {
		qualified := pq.QuoteIdentifier(namespace) + "." + pq.QuoteIdentifier(table)
		switch operation {
		case "insert":
			statement = `INSERT INTO ` + qualified + ` VALUES (2, 'inserted', 2.5000, DATE '2024-02-03', TIMESTAMP '2024-02-03 04:05:06.007', '{"inserted":true}'::jsonb, 22.25)`
		case "update":
			statement = `UPDATE ` + qualified + ` SET name='updated', amount=98765432109876543210.4321, changed_at=TIMESTAMP '2024-02-03 04:05:06.007', score=99.25 WHERE id=1`
		case "delete":
			statement = `DELETE FROM ` + qualified + ` WHERE id=2`
		}
	} else {
		qualified := mysqlCDCQuoteIdentifier(namespace) + "." + mysqlCDCQuoteIdentifier(table)
		switch operation {
		case "insert":
			statement = `INSERT INTO ` + qualified + ` VALUES (2, 'inserted', 2.5000, '2024-02-03', '2024-02-03 04:05:06.007', JSON_OBJECT('inserted', true), 22.25)`
		case "update":
			statement = `UPDATE ` + qualified + ` SET name='updated', amount=98765432109876543210.4321, changed_at='2024-02-03 04:05:06.007', score=99.25 WHERE id=1`
		case "delete":
			statement = `DELETE FROM ` + qualified + ` WHERE id=2`
		}
	}
	if statement == "" {
		t.Fatalf("unsupported %s source mutation %q", sourceType, operation)
	}
	if _, err := db.ExecContext(ctx, statement); err != nil {
		t.Fatal(err)
	}
}

func assertDatabaseCDCOracleTargetValues(t *testing.T, ctx context.Context, target *cdcDataE2ETarget, id int64, wantName, wantAmount, wantDate, wantChangedAt string, wantScore float64) {
	t.Helper()
	var name, amount, businessDate, changedAt string
	var score float64
	err := target.DB.QueryRowContext(ctx, `SELECT "name",
		TO_CHAR("amount", 'FM99999999999999999999999999999999999999D9999', 'NLS_NUMERIC_CHARACTERS=''.,'''),
		TO_CHAR("business_date", 'YYYY-MM-DD'), TO_CHAR("changed_at", 'YYYY-MM-DD HH24:MI:SS.FF3'), "score"
		FROM `+oracleSpatialCDCQuoteIdentifier(target.Namespace)+`.`+oracleSpatialCDCQuoteIdentifier(target.Table)+` WHERE "id"=:1`, id).
		Scan(&name, &amount, &businessDate, &changedAt, &score)
	if err != nil {
		t.Fatal(err)
	}
	if name != wantName || amount != wantAmount || businessDate != wantDate || changedAt != wantChangedAt || score != wantScore {
		t.Fatalf("Oracle CDC target values: name=%q amount=%q date=%q changed_at=%q score=%v", name, amount, businessDate, changedAt, score)
	}
}
