package continuous

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
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
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twpayne/go-geom/encoding/wkt"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type oracleSpatialCDCGeometryCase struct {
	name             string
	tablePrefix      string
	geometryType     string
	dimension        int
	initialGeometry  string
	insertedGeometry string
	updatedGeometry  string
	initialEWKT      string
	insertedEWKT     string
	updatedEWKT      string
	injectRecoveries bool
}

func TestIntegrationOracleSpatialCDCGeometryMatrixAndRecovery(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_SPATIAL_CDC_DATA_E2E") != "1" {
		t.Skip("set ADDP_ORACLE_SPATIAL_CDC_DATA_E2E=1 to run Oracle Spatial CDC data-plane integration test")
	}
	cases := []oracleSpatialCDCGeometryCase{
		{
			name: "polygon_xy", tablePrefix: "ADDP_SP_P", geometryType: "POLYGON", dimension: 2, injectRecoveries: true,
			initialGeometry:  oracleSpatialCDCPolygon(0, 0, 10, 10),
			insertedGeometry: oracleSpatialCDCPolygon(20, 20, 30, 30),
			updatedGeometry:  oracleSpatialCDCPolygon(40, 40, 50, 50),
			initialEWKT:      "SRID=4326;POLYGON((0 0,10 0,10 10,0 10,0 0))",
			insertedEWKT:     "SRID=4326;POLYGON((20 20,30 20,30 30,20 30,20 20))",
			updatedEWKT:      "SRID=4326;POLYGON((40 40,50 40,50 50,40 50,40 40))",
		},
		{
			name: "multipolygon_xy", tablePrefix: "ADDP_SP_MP", geometryType: "MULTIPOLYGON", dimension: 2,
			initialGeometry:  oracleSpatialCDCMultiPolygon(0),
			insertedGeometry: oracleSpatialCDCMultiPolygon(20),
			updatedGeometry:  oracleSpatialCDCMultiPolygon(40),
			initialEWKT:      "SRID=4326;MULTIPOLYGON(((0 0,4 0,4 4,0 4,0 0)),((6 6,10 6,10 10,6 10,6 6)))",
			insertedEWKT:     "SRID=4326;MULTIPOLYGON(((20 20,24 20,24 24,20 24,20 20)),((26 26,30 26,30 30,26 30,26 26)))",
			updatedEWKT:      "SRID=4326;MULTIPOLYGON(((40 40,44 40,44 44,40 44,40 40)),((46 46,50 46,50 50,46 50,46 46)))",
		},
		{
			name: "point_xyz", tablePrefix: "ADDP_SP_Z", geometryType: "POINT", dimension: 3,
			initialGeometry:  oracleSpatialCDCPointXYZ(1, 2, 3),
			insertedGeometry: oracleSpatialCDCPointXYZ(4, 5, 6),
			updatedGeometry:  oracleSpatialCDCPointXYZ(7, 8, 9),
			initialEWKT:      "SRID=4326;POINT(1 2 3)",
			insertedEWKT:     "SRID=4326;POINT(4 5 6)",
			updatedEWKT:      "SRID=4326;POINT(7 8 9)",
		},
	}
	for _, testCase := range cases {
		for _, targetType := range []string{"postgresql", "mysql", "oracle"} {
			if targetType != "postgresql" && testCase.dimension != 2 {
				continue
			}
			t.Run(testCase.name+"/target="+targetType, func(t *testing.T) {
				runIntegrationOracleSpatialCDCGeometryCase(t, testCase, targetType)
			})
		}
	}
}

// TestIntegrationOracleSpatialCDCSchemaDriftBlocksAndStops injects an external
// change into the generation-owned mirror table. The source-table DDL guard is
// intentionally bypassed because normal Oracle Spatial CDC operation freezes
// the source schema by design.
func TestIntegrationOracleSpatialCDCSchemaDriftBlocksAndStops(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_SPATIAL_CDC_SCHEMA_DRIFT_E2E") != "1" {
		t.Skip("set ADDP_ORACLE_SPATIAL_CDC_SCHEMA_DRIFT_E2E=1 to run Oracle Spatial CDC schema drift integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
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
		&models.PostgreSQLCaptureResource{}, &models.MySQLCaptureResource{}, &models.OracleCaptureResource{}, &models.SchemaChangeRequest{},
	); err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UnixNano()
	tableName := fmt.Sprintf("ADDP_SD_%012d", suffix%1_000_000_000_000)
	indexName := fmt.Sprintf("ADDP_SDI_%012d", suffix%1_000_000_000_000)
	targetSchema := fmt.Sprintf("cdc_oracle_drift_%d", suffix)
	targetTable := "features_target"
	sourceInfo := oracleSpatialCDCConnectionInfo()
	sourceDB := openOracleSpatialCDCDB(t, sourceInfo)
	defer sourceDB.Close()
	createOracleSpatialCDCTestTable(t, ctx, sourceDB, tableName, indexName, 2, oracleSpatialCDCPolygon(0, 0, 10, 10))
	defer dropOracleSpatialCDCTestTable(t, sourceDB, tableName)
	target := openCDCDataE2ETarget(t, ctx, "postgresql", targetSchema, targetTable, nil, nil)
	defer target.Close()

	resolver := planner.StaticEngineResolver{
		22:              {Type: "oracle", EngineID: 22, ConnInfo: sourceInfo},
		target.EngineID: {Type: target.Type, EngineID: target.EngineID, ConnInfo: target.ConnInfo, Capabilities: ptrEngineCapabilities(target.Plugin.Capabilities())},
	}
	captureRepo := repository.NewCaptureRepository(infraDB)
	topicAdmin, err := capture.NewKafkaTopicAdmin(capture.KafkaAdminConfig{
		BootstrapServers: cdcDataEnv("ADDP_TEST_INFRA_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		Username:         cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_USERNAME", "admin"), Password: cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_PASSWORD", "addp_kafka_admin"),
		SecurityProtocol: cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"), SASLMechanism: cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"),
		TLSCACertFile: cdcDataEnv("ADDP_TEST_INFRA_KAFKA_TLS_CA_CERT_FILE", ""),
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
	captureSupervisor, err := capture.NewSupervisor(captureRepo, capturePlanResolver, connectClient, topicAdmin, capture.DatabaseSourceResources{}, capture.SupervisorConfig{
		TopicRetention: time.Hour, TopicReplication: cdcDataEnvInt16("ADDP_TEST_INFRA_KAFKA_REPLICATION_FACTOR", 1),
		ConnectLoopbackHost:     cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_LOOPBACK_HOST", "host.docker.internal"),
		ConnectBootstrapServers: cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_BOOTSTRAP_SERVERS", "redpanda:29092"), ConnectKafkaUsername: cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_USERNAME", "connect"), ConnectKafkaPassword: cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_PASSWORD", "addp_kafka_connect"),
		ConnectKafkaSecurityProtocol: cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_SECURITY_PROTOCOL", "sasl_plaintext"), ConnectKafkaSASLMechanism: cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"), ConnectKafkaTLSCACertFile: cdcDataEnv("ADDP_TEST_INFRA_KAFKA_TLS_CA_CERT_FILE", ""),
		ProvisioningTimeout: 2 * time.Minute, StatusPollInterval: 500 * time.Millisecond, MonitorInterval: time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	leaseRepo := repository.NewRuntimeLeaseRepository(infraDB, repository.ContinuousRecoveryPolicy{InitialBackoff: time.Second, MaxBackoff: 4 * time.Second, MaxFailures: 3, CircuitOpenTime: 10 * time.Second, StabilityWindow: 30 * time.Second})
	stateRepo := repository.NewSyncStateRepository(infraDB)
	executionService := service.NewExecutionService(infraDB, commonExecution.NewTaskExecutionRepository(infraDB))
	taskService := service.NewTaskService(infraDB, nil, &transferconfig.Config{ContinuousRuntimeStopTimeout: 5 * time.Second, ContinuousRuntimeStopPollInterval: 50 * time.Millisecond})
	taskService.SetEngineResolver(resolver)
	taskService.SetExecutionService(executionService)
	taskService.SetCaptureControl(captureSupervisor)
	taskService.SetSchemaChangeInspector(capturePlanResolver)
	apiRouter := cdcDataAPIRouter(t, taskService, uint(910000+suffix%80000), 910001)
	task := cdcDataCreateTaskViaAPI(t, apiRouter, oracleSpatialCDCDataTaskConfig(tableName, target))
	defer cleanupCDCDataInfraRows(infraDB, task.ID)
	if err := infraDB.Model(&models.TransferTask{}).Where("id = ?", task.ID).Update("auto_scan_metadata", false).Error; err != nil {
		t.Fatal(err)
	}
	execution := cdcDataStartTaskViaAPI(t, apiRouter, task.ID)
	resource, err := captureRepo.GetLatest(ctx, task.ID, task.TenantID)
	if err != nil || resource.Oracle == nil || !resource.Oracle.SpatialArtifactsOwned {
		t.Fatalf("Oracle Spatial capture resource=%#v err=%v", resource, err)
	}
	captureStopped := false
	defer func() {
		if !captureStopped {
			_ = captureSupervisor.Stop(context.Background(), &task)
		}
	}()
	claim, err := leaseRepo.ClaimNext(ctx, "oracle-schema-drift-worker", time.Now(), 30*time.Second)
	if err != nil || claim == nil || claim.Execution.ExecutionID != execution.ExecutionID {
		t.Fatalf("Oracle schema drift claim=%#v err=%v", claim, err)
	}
	task.ApplyIdentity = claim.Task.ApplyIdentity
	runner := &DataSessionRunner{ProtectionGate: allowSourceProtectionGate{}, Resolver: resolver, States: stateRepo, Progress: leaseRepo, Captures: captureRepo, InfraKafkaConnection: cdcDataTransferKafkaConnection(), PollTimeout: 500 * time.Millisecond, DiagnosticsInterval: time.Second,
		GetPlugin: func(engineType string) (engineplugin.EnginePlugin, error) {
			switch engineType {
			case "kafka":
				return &kafka.KafkaPlugin{}, nil
			case "oracle":
				return &oracleplugin.OraclePlugin{}, nil
			case "postgresql":
				return &postgresql.PostgreSQLPlugin{}, nil
			default:
				return nil, fmt.Errorf("unexpected engine type %q", engineType)
			}
		},
	}
	runnerCtx, cancelRunner := context.WithCancel(ctx)
	runnerDone := make(chan error, 1)
	go func() { runnerDone <- runner.Run(runnerCtx, *claim) }()
	waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target, 1, "snapshot", oracleSpatialCDCGeometryCase{geometryType: "POLYGON", dimension: 2}, "SRID=4326;POLYGON((0 0,10 0,10 10,0 10,0 0))")
	waitOracleSpatialCDCOffsetsConverged(t, ctx, stateRepo, target, task.ID, resource.SourceIdentity, task.ApplyIdentity)
	committed, err := oracleSpatialCDCCommittedOffset(ctx, stateRepo, task.ID, resource.SourceIdentity)
	if err != nil {
		t.Fatal(err)
	}
	quotedMirror := oracleSpatialCDCQuoteIdentifier(resource.Oracle.SpatialMirrorTableName)
	execOracleSpatialCDC(t, ctx, sourceDB, `ALTER TABLE `+quotedMirror+` ADD "EXTRA_FIELD" VARCHAR2(100 CHAR)`)
	execOracleSpatialCDC(t, ctx, sourceDB, `INSERT INTO `+quotedMirror+` ("ID", "NAME", "SHAPE", "EXTRA_FIELD") SELECT 2, 'drifted', SDO_UTIL.TO_WKBGEOMETRY("SHAPE"), 'external drift' FROM `+oracleSpatialCDCQuoteIdentifier(tableName)+` WHERE "ID" = 1`)
	var runnerErr error
	select {
	case runnerErr = <-runnerDone:
	case <-time.After(30 * time.Second):
		cancelRunner()
		t.Fatal("Oracle schema drift runner did not stop")
	}
	var schemaErr *SchemaChangeError
	if !errors.As(runnerErr, &schemaErr) || !containsTestString(schemaErr.UnexpectedFields, "EXTRA_FIELD") {
		t.Fatalf("Oracle schema drift runner error=%v schema=%#v", runnerErr, schemaErr)
	}
	after, err := oracleSpatialCDCCommittedOffset(ctx, stateRepo, task.ID, resource.SourceIdentity)
	if err != nil || after != committed {
		t.Fatalf("Oracle schema drift advanced offset: before=%d after=%d err=%v", committed, after, err)
	}
	target.AssertRowAbsent(t, ctx, 2)
	if err := leaseRepo.Finish(context.Background(), *claim, commonExecution.ExecutionStatusFailed, "schema_change_blocked", runnerErr.Error(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var blockedTask models.TransferTask
	if err := infraDB.First(&blockedTask, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if blockedTask.Status != models.TaskStatusBlocked || blockedTask.DesiredState != models.TaskDesiredStateRunning {
		t.Fatalf("Oracle schema-blocked task status=%q desired=%q", blockedTask.Status, blockedTask.DesiredState)
	}
	var blockedExecution commonExecution.TaskExecution
	if err := infraDB.Where("execution_id = ?", claim.Execution.ExecutionID).First(&blockedExecution).Error; err != nil {
		t.Fatal(err)
	}
	continuousMetadata, _ := blockedExecution.Metadata["continuous"].(map[string]interface{})
	schemaChange, _ := continuousMetadata["schema_change"].(map[string]interface{})
	if fmt.Sprint(schemaChange["unexpected_fields"]) != "[EXTRA_FIELD]" || schemaChange["status"] != "pending" ||
		schemaChange["request_id"] == nil || blockedExecution.Metadata["stop_reason"] != "schema_change_blocked" {
		t.Fatalf("Oracle schema-blocked execution metadata=%#v", blockedExecution.Metadata)
	}
	startResponse := cdcDataAPIRequest(t, apiRouter, http.MethodPost, fmt.Sprintf("/api/v1/transfer/task-definitions/%d/start", task.ID), nil)
	if startResponse.Code != http.StatusConflict {
		t.Fatalf("blocked Oracle start status=%d body=%s", startResponse.Code, startResponse.Body.String())
	}
	if _, err := taskService.ResumeTask(ctx, task.ID, task.TenantID, 910001); !errors.Is(err, service.ErrCDCSchemaChangeBlocked) {
		t.Fatalf("blocked Oracle resume error=%v", err)
	}
	if _, err := executionService.RetryExecution(ctx, uint(blockedExecution.ID), task.TenantID, 910001); !errors.Is(err, service.ErrCDCSchemaChangeBlocked) {
		t.Fatalf("blocked Oracle retry error=%v", err)
	}
	cancelRunner()
	cddStop := cdcDataAPIRequest(t, apiRouter, http.MethodPost, fmt.Sprintf("/api/v1/transfer/task-definitions/%d/stop", task.ID), models.StopTaskRequest{Confirmed: true, ConfirmationText: task.Name})
	if cddStop.Code != http.StatusOK {
		t.Fatalf("stop Oracle schema drift task status=%d body=%s", cddStop.Code, cddStop.Body.String())
	}
	captureStopped = true
	adminClient, err := oracleSpatialCDCKafkaAdminClient()
	if err != nil {
		t.Fatal(err)
	}
	defer adminClient.Close()
	assertOracleSpatialCDCCleanup(t, ctx, sourceDB, captureRepo, connectClient, kadm.NewClient(adminClient), task, resource)
}

func TestIntegrationOracleCDCNativeTypeMatrix(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_CDC_DATA_E2E") != "1" {
		t.Skip("set ADDP_ORACLE_CDC_DATA_E2E=1 to run Oracle CDC native type matrix integration test")
	}
	for _, targetType := range []string{"postgresql", "mysql", "oracle"} {
		t.Run("target="+targetType, func(t *testing.T) {
			runIntegrationOracleCDCNativeTypeMatrix(t, targetType)
		})
	}
}

func runIntegrationOracleCDCNativeTypeMatrix(t *testing.T, targetType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
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
		&models.PostgreSQLCaptureResource{}, &models.MySQLCaptureResource{}, &models.OracleCaptureResource{}, &models.SchemaChangeRequest{},
	); err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UnixNano()
	tableName := fmt.Sprintf("ADDP_NT_%012d", suffix%1_000_000_000_000)
	targetSchema := fmt.Sprintf("cdc_oracle_native_%d", suffix)
	targetTable := "native_target"
	sourceInfo := oracleSpatialCDCConnectionInfo()
	sourceDB := openOracleSpatialCDCDB(t, sourceInfo)
	defer sourceDB.Close()
	createOracleCDCNativeTypeTable(t, ctx, sourceDB, tableName)
	defer dropOracleSpatialCDCTestTable(t, sourceDB, tableName)
	var mysqlRootInfo engineplugin.ConnectionInfo
	var mysqlRootDB *sql.DB
	if targetType == "mysql" {
		mysqlRootInfo = mysqlCDCConnInfo(
			cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_ROOT_USER", "root"),
			cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_ROOT_PASSWORD", "password"),
			"mysql",
		)
		mysqlRootDB = openMySQLIntegrationDB(t, mysqlRootInfo)
		defer mysqlRootDB.Close()
	}
	target := openCDCDataE2ETarget(t, ctx, targetType, targetSchema, targetTable, mysqlRootInfo, mysqlRootDB)
	defer target.Close()

	resolver := planner.StaticEngineResolver{
		22:              {Type: "oracle", EngineID: 22, ConnInfo: sourceInfo},
		target.EngineID: {Type: target.Type, EngineID: target.EngineID, ConnInfo: target.ConnInfo, Capabilities: ptrEngineCapabilities(target.Plugin.Capabilities())},
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
	captureSupervisor, err := capture.NewSupervisor(captureRepo, capturePlanResolver, connectClient, topicAdmin, capture.DatabaseSourceResources{}, capture.SupervisorConfig{
		TopicRetention: time.Hour, TopicReplication: cdcDataEnvInt16("ADDP_TEST_INFRA_KAFKA_REPLICATION_FACTOR", 1),
		ConnectLoopbackHost:          cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_LOOPBACK_HOST", "host.docker.internal"),
		ConnectBootstrapServers:      cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_BOOTSTRAP_SERVERS", "redpanda:29092"),
		ConnectKafkaUsername:         cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_USERNAME", "connect"),
		ConnectKafkaPassword:         cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_PASSWORD", "addp_kafka_connect"),
		ConnectKafkaSecurityProtocol: cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_SECURITY_PROTOCOL", "sasl_plaintext"),
		ConnectKafkaSASLMechanism:    cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"),
		ConnectKafkaTLSCACertFile:    cdcDataEnv("ADDP_TEST_INFRA_KAFKA_TLS_CA_CERT_FILE", ""),
		ProvisioningTimeout:          2 * time.Minute, StatusPollInterval: 500 * time.Millisecond, MonitorInterval: time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	leaseRepo := repository.NewRuntimeLeaseRepository(infraDB, repository.ContinuousRecoveryPolicy{InitialBackoff: time.Second, MaxBackoff: 4 * time.Second, MaxFailures: 3, CircuitOpenTime: 10 * time.Second, StabilityWindow: 30 * time.Second})
	stateRepo := repository.NewSyncStateRepository(infraDB)
	executionService := service.NewExecutionService(infraDB, commonExecution.NewTaskExecutionRepository(infraDB))
	taskService := service.NewTaskService(infraDB, nil, &transferconfig.Config{ContinuousRuntimeStopTimeout: 5 * time.Second, ContinuousRuntimeStopPollInterval: 50 * time.Millisecond})
	taskService.SetEngineResolver(resolver)
	taskService.SetExecutionService(executionService)
	taskService.SetCaptureControl(captureSupervisor)
	taskService.SetSchemaChangeInspector(capturePlanResolver)
	apiRouter := cdcDataAPIRouter(t, taskService, uint(930000+suffix%60000), 930001)
	task := cdcDataCreateTaskViaAPI(t, apiRouter, oracleCDCNativeTypeTaskConfig(tableName, target))
	defer cleanupCDCDataInfraRows(infraDB, task.ID)
	if err := infraDB.Model(&models.TransferTask{}).Where("id = ?", task.ID).Update("auto_scan_metadata", false).Error; err != nil {
		t.Fatal(err)
	}
	execution := cdcDataStartTaskViaAPI(t, apiRouter, task.ID)
	resource, err := captureRepo.GetLatest(ctx, task.ID, task.TenantID)
	if err != nil || resource.SourceType != models.CaptureSourceOracle || resource.Oracle == nil || resource.Oracle.SpatialArtifactsOwned {
		t.Fatalf("ordinary Oracle capture resource=%#v err=%v", resource, err)
	}
	captureStopped := false
	defer func() {
		if !captureStopped {
			_ = captureSupervisor.Stop(context.Background(), &task)
		}
	}()
	claim, err := leaseRepo.ClaimNext(ctx, "oracle-native-worker", time.Now(), 5*time.Minute)
	if err != nil || claim == nil || claim.Execution.ExecutionID != execution.ExecutionID {
		t.Fatalf("ordinary Oracle claim=%#v err=%v", claim, err)
	}
	task.ApplyIdentity = claim.Task.ApplyIdentity
	runner := &DataSessionRunner{ProtectionGate: allowSourceProtectionGate{}, Resolver: resolver, States: stateRepo, Progress: leaseRepo, Captures: captureRepo, InfraKafkaConnection: cdcDataTransferKafkaConnection(), PollTimeout: 500 * time.Millisecond, DiagnosticsInterval: time.Second,
		GetPlugin: func(engineType string) (engineplugin.EnginePlugin, error) {
			switch engineType {
			case "kafka":
				return &kafka.KafkaPlugin{}, nil
			case "mysql":
				return &mysqlplugin.MySQLPlugin{}, nil
			case "oracle":
				return &oracleplugin.OraclePlugin{}, nil
			case "postgresql":
				return &postgresql.PostgreSQLPlugin{}, nil
			default:
				return nil, fmt.Errorf("unexpected engine type %q", engineType)
			}
		},
	}
	runnerCtx, cancelRunner := context.WithCancel(ctx)
	runnerDone := make(chan error, 1)
	go func() { runnerDone <- runner.Run(runnerCtx, *claim) }()
	target.WaitRow(t, ctx, runnerDone, 1, "snapshot", true)
	assertOracleCDCNativeTypeTarget(t, ctx, target, 1, "snapshot")
	waitOracleSpatialCDCOffsetsConverged(t, ctx, stateRepo, target, task.ID, resource.SourceIdentity, task.ApplyIdentity)
	if target.Type == "postgresql" {
		assertOracleCDCRecoveryWindowObservation(t, ctx, capturePlanResolver, connectClient, resource)
	}
	committedBeforeLongTransaction, err := oracleSpatialCDCCommittedOffset(ctx, stateRepo, task.ID, resource.SourceIdentity)
	if err != nil {
		t.Fatal(err)
	}
	longTransaction := beginOracleCDCNativeTypeBatch(t, ctx, sourceDB, tableName, 1000, 128, "long-transaction")
	defer longTransaction.Rollback()
	waitOracleCDCSourceTransactionObservation(t, ctx, capturePlanResolver, connectClient, resource, 128)
	time.Sleep(1500 * time.Millisecond)
	assertOracleCDCTargetCount(t, ctx, target, 1000, 1127, 0)
	if committed, err := oracleSpatialCDCCommittedOffset(ctx, stateRepo, task.ID, resource.SourceIdentity); err != nil || committed != committedBeforeLongTransaction {
		t.Fatalf("uncommitted Oracle transaction advanced position: before=%d after=%d err=%v", committedBeforeLongTransaction, committed, err)
	}
	if err := longTransaction.Commit(); err != nil {
		t.Fatalf("commit Oracle CDC long transaction: %v", err)
	}
	waitOracleSpatialCDCTargetCount(t, ctx, runnerDone, target, 1000, 1127, 128)
	target.WaitRow(t, ctx, runnerDone, 1000, "long-transaction-1000", true)
	target.WaitRow(t, ctx, runnerDone, 1127, "long-transaction-1127", true)
	waitOracleSpatialCDCOffsetsConverged(t, ctx, stateRepo, target, task.ID, resource.SourceIdentity, task.ApplyIdentity)
	committedAfterLongTransaction, err := oracleSpatialCDCCommittedOffset(ctx, stateRepo, task.ID, resource.SourceIdentity)
	if err != nil || committedAfterLongTransaction <= committedBeforeLongTransaction {
		t.Fatalf("committed Oracle transaction position: before=%d after=%d err=%v", committedBeforeLongTransaction, committedAfterLongTransaction, err)
	}
	rolledBackTransaction := beginOracleCDCNativeTypeBatch(t, ctx, sourceDB, tableName, 2000, 128, "rolled-back")
	if err := rolledBackTransaction.Rollback(); err != nil {
		t.Fatalf("rollback Oracle CDC long transaction: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	assertOracleCDCTargetCount(t, ctx, target, 2000, 2127, 0)
	if committed, err := oracleSpatialCDCCommittedOffset(ctx, stateRepo, task.ID, resource.SourceIdentity); err != nil || committed != committedAfterLongTransaction {
		t.Fatalf("rolled-back Oracle transaction advanced position: before=%d after=%d err=%v", committedAfterLongTransaction, committed, err)
	}
	execOracleSpatialCDC(t, ctx, sourceDB, oracleCDCNativeTypeInsertSQL(tableName, 2, "inserted"))
	target.WaitRow(t, ctx, runnerDone, 2, "inserted", true)
	assertOracleCDCNativeTypeTarget(t, ctx, target, 2, "inserted")
	execOracleSpatialCDC(t, ctx, sourceDB, `UPDATE `+oracleSpatialCDCQuoteIdentifier(tableName)+` SET "NAME" = 'updated', "AMOUNT" = 98765432109876543210.4321, "CHANGED_AT" = TIMESTAMP '2026-02-03 04:05:06.789' WHERE "ID" = 2`)
	target.WaitRow(t, ctx, runnerDone, 2, "updated", true)
	assertOracleCDCNativeTypeUpdatedTarget(t, ctx, target)
	execOracleSpatialCDC(t, ctx, sourceDB, `DELETE FROM `+oracleSpatialCDCQuoteIdentifier(tableName)+` WHERE "ID" = 2`)
	target.WaitRow(t, ctx, runnerDone, 2, "", false)
	waitOracleSpatialCDCOffsetsConverged(t, ctx, stateRepo, target, task.ID, resource.SourceIdentity, task.ApplyIdentity)
	cancelRunner()
	if err := <-runnerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("ordinary Oracle final runner error=%v", err)
	}
	if err := leaseRepo.Finish(context.Background(), *claim, commonExecution.ExecutionStatusCancelled, "test_complete", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	cdcDataStopTaskViaAPI(t, apiRouter, task.ID, task.Name)
	captureStopped = true
	adminClient, err := oracleSpatialCDCKafkaAdminClient()
	if err != nil {
		t.Fatal(err)
	}
	defer adminClient.Close()
	assertOracleCDCCleanup(t, ctx, captureRepo, connectClient, kadm.NewClient(adminClient), task, resource)
}

func runIntegrationOracleSpatialCDCGeometryCase(t *testing.T, geometryCase oracleSpatialCDCGeometryCase, targetType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
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
		&models.PostgreSQLCaptureResource{}, &models.MySQLCaptureResource{}, &models.OracleCaptureResource{}, &models.SchemaChangeRequest{},
	); err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UnixNano()
	tableName := fmt.Sprintf("%s_%012d", geometryCase.tablePrefix, suffix%1_000_000_000_000)
	indexName := fmt.Sprintf("ADDP_SI_%012d", suffix%1_000_000_000_000)
	targetSchema := fmt.Sprintf("cdc_oracle_spatial_%d", suffix)
	targetTable := "features_target"
	sourceInfo := oracleSpatialCDCConnectionInfo()
	sourceDB := openOracleSpatialCDCDB(t, sourceInfo)
	defer sourceDB.Close()
	createOracleSpatialCDCTestTable(t, ctx, sourceDB, tableName, indexName, geometryCase.dimension, geometryCase.initialGeometry)
	defer dropOracleSpatialCDCTestTable(t, sourceDB, tableName)

	var mysqlRootInfo engineplugin.ConnectionInfo
	var mysqlRootDB *sql.DB
	if targetType == "mysql" {
		mysqlRootInfo = mysqlCDCConnInfo(
			cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_ROOT_USER", "root"),
			cdcDataEnv("ADDP_TEST_BUSINESS_MYSQL_ROOT_PASSWORD", "password"),
			"mysql",
		)
		mysqlRootDB = openMySQLIntegrationDB(t, mysqlRootInfo)
		defer mysqlRootDB.Close()
	}
	target := openCDCDataE2ETarget(t, ctx, targetType, targetSchema, targetTable, mysqlRootInfo, mysqlRootDB)
	defer target.Close()
	resolver := planner.StaticEngineResolver{
		22: {Type: "oracle", EngineID: 22, ConnInfo: sourceInfo},
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
		captureRepo, capturePlanResolver, connectClient, topicAdmin, capture.DatabaseSourceResources{},
		capture.SupervisorConfig{
			TopicRetention:               time.Hour,
			TopicReplication:             cdcDataEnvInt16("ADDP_TEST_INFRA_KAFKA_REPLICATION_FACTOR", 1),
			ConnectLoopbackHost:          cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_LOOPBACK_HOST", "host.docker.internal"),
			ConnectBootstrapServers:      cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_BOOTSTRAP_SERVERS", "redpanda:29092"),
			ConnectKafkaUsername:         cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_USERNAME", "connect"),
			ConnectKafkaPassword:         cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_PASSWORD", "addp_kafka_connect"),
			ConnectKafkaSecurityProtocol: cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_SECURITY_PROTOCOL", "sasl_plaintext"),
			ConnectKafkaSASLMechanism:    cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"),
			ConnectKafkaTLSCACertFile:    cdcDataEnv("ADDP_TEST_INFRA_KAFKA_TLS_CA_CERT_FILE", ""),
			ProvisioningTimeout:          2 * time.Minute, StatusPollInterval: 500 * time.Millisecond, MonitorInterval: time.Second,
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
	apiRouter := cdcDataAPIRouter(t, taskService, uint(900000+suffix%90000), 900001)
	task := cdcDataCreateTaskViaAPI(t, apiRouter, oracleSpatialCDCDataTaskConfig(tableName, target))
	defer cleanupCDCDataInfraRows(infraDB, task.ID)

	execution := cdcDataStartTaskViaAPI(t, apiRouter, task.ID)
	resource, err := captureRepo.GetLatest(ctx, task.ID, task.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if resource.SourceType != models.CaptureSourceOracle || resource.Oracle == nil || !resource.Oracle.SpatialArtifactsOwned {
		t.Fatalf("Oracle Spatial capture provider facts=%#v", resource)
	}
	captureStopped := false
	defer func() {
		if !captureStopped {
			_ = captureSupervisor.Stop(context.Background(), &task)
		}
	}()

	claim, err := leaseRepo.ClaimNext(ctx, "oracle-spatial-worker-a", time.Now(), 30*time.Second)
	if err != nil || claim == nil || claim.Execution.ExecutionID != execution.ExecutionID {
		t.Fatalf("first Oracle Spatial claim=%#v err=%v", claim, err)
	}
	task.ApplyIdentity = claim.Task.ApplyIdentity
	var metadataScanCalls atomic.Int32
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/meta/scan/run/manual" {
			http.Error(w, "unexpected Meta request", http.StatusNotFound)
			return
		}
		call := metadataScanCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"id":%d,"tenant_id":%d,"execution_id":"oracle-spatial-meta-%d","module":"meta","task_type":"scan","status":"pending","trigger_type":"manual"}`,
			call, task.TenantID, call)
	}))
	defer metaServer.Close()
	metadataScanner := &TargetMetadataScanner{
		Store: leaseRepo,
		Client: commonClient.NewMetaClient(metaServer.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
			return "oracle-spatial-e2e-token", nil
		})),
		ClaimTTL: 2 * time.Minute,
	}
	runner := &DataSessionRunner{
		ProtectionGate: allowSourceProtectionGate{},
		Resolver:       resolver, States: stateRepo, Progress: leaseRepo, Captures: captureRepo,
		InfraKafkaConnection: cdcDataTransferKafkaConnection(), PollTimeout: 500 * time.Millisecond,
		DiagnosticsInterval: time.Second, MetadataScanner: metadataScanner,
		GetPlugin: func(engineType string) (engineplugin.EnginePlugin, error) {
			switch engineType {
			case "kafka":
				return &kafka.KafkaPlugin{}, nil
			case "oracle":
				return &oracleplugin.OraclePlugin{}, nil
			case "mysql":
				return &mysqlplugin.MySQLPlugin{}, nil
			case "postgresql":
				return &postgresql.PostgreSQLPlugin{}, nil
			default:
				return nil, fmt.Errorf("unexpected engine type %q", engineType)
			}
		},
	}

	activeClaim := claim
	runnerCtx, cancelRunner := context.WithCancel(ctx)
	runnerDone := make(chan error, 1)
	go func(current repository.RuntimeLeaseClaim) { runnerDone <- runner.Run(runnerCtx, current) }(*claim)
	waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target, 1, "snapshot", geometryCase, geometryCase.initialEWKT)

	injectRecoveries := geometryCase.injectRecoveries && target.Type == "postgresql"
	if injectRecoveries {
		cancelRunner()
		if err := <-runnerDone; !errors.Is(err, context.Canceled) {
			t.Fatalf("Oracle Spatial service-interruption runner error=%v", err)
		}
		if err := infraDB.Model(&models.RuntimeLease{}).Where("task_id = ?", task.ID).Updates(map[string]interface{}{
			"lease_until": time.Now().Add(-time.Second), "heartbeat_at": time.Now().Add(-time.Second),
		}).Error; err != nil {
			t.Fatal(err)
		}
		recoveryDetectedAt := time.Now()
		if recoveryClaim, claimErr := leaseRepo.ClaimNext(ctx, "oracle-spatial-worker-b", recoveryDetectedAt, 30*time.Second); claimErr != nil || recoveryClaim != nil {
			t.Fatalf("Oracle Spatial recovery backoff claim=%#v err=%v", recoveryClaim, claimErr)
		}
		activeClaim, err = leaseRepo.ClaimNext(ctx, "oracle-spatial-worker-b", recoveryDetectedAt.Add(time.Second), 30*time.Second)
		if err != nil || activeClaim == nil || activeClaim.Lease.FencingToken <= claim.Lease.FencingToken {
			t.Fatalf("Oracle Spatial recovery claim=%#v err=%v", activeClaim, err)
		}
		runnerCtx, cancelRunner = context.WithCancel(ctx)
		runnerDone = make(chan error, 1)
		go func(current repository.RuntimeLeaseClaim) { runnerDone <- runner.Run(runnerCtx, current) }(*activeClaim)

		if err := connectClient.Pause(ctx, resource.ConnectorName); err != nil {
			t.Fatal(err)
		}
		waitOracleSpatialCDCConnectorState(t, ctx, connectClient, resource.ConnectorName, "PAUSED")
		execOracleSpatialCDC(t, ctx, sourceDB, `INSERT INTO `+oracleSpatialCDCQuoteIdentifier(tableName)+` ("ID", "NAME", "SHAPE") VALUES (2, 'connect-backlog', `+geometryCase.insertedGeometry+`)`)
		time.Sleep(1500 * time.Millisecond)
		target.AssertRowAbsent(t, ctx, 2)
		if err := connectClient.Resume(ctx, resource.ConnectorName); err != nil {
			t.Fatal(err)
		}
		waitOracleSpatialCDCConnectorHealthy(t, ctx, connectClient, resource.ConnectorName)
		waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target, 2, "connect-backlog", geometryCase, geometryCase.insertedEWKT)

		if os.Getenv("ADDP_ORACLE_SPATIAL_CDC_CONTAINER_FAULT") == "1" {
			injectOracleSpatialCDCContainerFault(t, ctx, sourceInfo, connectClient, resource.ConnectorName)
			execOracleSpatialCDC(t, ctx, sourceDB, `INSERT INTO `+oracleSpatialCDCQuoteIdentifier(tableName)+` ("ID", "NAME", "SHAPE") VALUES (3, 'oracle-recovered', `+geometryCase.insertedGeometry+`)`)
			waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target, 3, "oracle-recovered", geometryCase, geometryCase.insertedEWKT)
		}
	} else {
		execOracleSpatialCDC(t, ctx, sourceDB, `INSERT INTO `+oracleSpatialCDCQuoteIdentifier(tableName)+` ("ID", "NAME", "SHAPE") VALUES (2, 'inserted', `+geometryCase.insertedGeometry+`)`)
		waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target, 2, "inserted", geometryCase, geometryCase.insertedEWKT)
	}

	execOracleSpatialCDC(t, ctx, sourceDB, `UPDATE `+oracleSpatialCDCQuoteIdentifier(tableName)+` SET "SHAPE" = `+geometryCase.updatedGeometry+` WHERE "ID" = 1`)
	waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target, 1, "snapshot", geometryCase, geometryCase.updatedEWKT)
	execOracleSpatialCDC(t, ctx, sourceDB, `DELETE FROM `+oracleSpatialCDCQuoteIdentifier(tableName)+` WHERE "ID" = 2`)
	target.WaitRow(t, ctx, runnerDone, 2, "", false)
	if injectRecoveries {
		rollbackOracleSpatialCDCInsert(t, ctx, sourceDB, tableName, 2000)
		insertOracleSpatialCDCBatch(t, ctx, sourceDB, tableName, 100, 32)
		assertOracleSpatialCDCMirrorCount(t, ctx, sourceDB, resource.Oracle.SpatialMirrorTableName, 100, 131, 32)
		waitOracleSpatialCDCTargetCount(t, ctx, runnerDone, target, 100, 131, 32)
		assertOracleSpatialCDCMirrorCount(t, ctx, sourceDB, resource.Oracle.SpatialMirrorTableName, 2000, 2000, 0)
		target.AssertRowAbsent(t, ctx, 2000)
		insertOracleSpatialCDCConcurrentBatches(t, ctx, sourceDB, tableName, 300, 4, 8)
		assertOracleSpatialCDCMirrorCount(t, ctx, sourceDB, resource.Oracle.SpatialMirrorTableName, 300, 331, 32)
		waitOracleSpatialCDCTargetCount(t, ctx, runnerDone, target, 300, 331, 32)

		execOracleSpatialCDC(t, ctx, sourceDB, `UPDATE `+oracleSpatialCDCQuoteIdentifier(tableName)+` SET "ID" = 1000, "NAME" = 'primary-key-updated' WHERE "ID" = 100`)
		target.WaitRow(t, ctx, runnerDone, 100, "", false)
		waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target, 1000, "primary-key-updated", geometryCase, geometryCase.updatedEWKT)
		assertOracleSpatialCDCMirrorCount(t, ctx, sourceDB, resource.Oracle.SpatialMirrorTableName, 100, 131, 31)

		execOracleSpatialCDC(t, ctx, sourceDB, `DELETE FROM `+oracleSpatialCDCQuoteIdentifier(tableName)+` WHERE ("ID" BETWEEN 101 AND 131) OR ("ID" BETWEEN 300 AND 331) OR "ID" = 1000`)
		waitOracleSpatialCDCTargetCount(t, ctx, runnerDone, target, 100, 1000, 0)
		assertOracleSpatialCDCMirrorCount(t, ctx, sourceDB, resource.Oracle.SpatialMirrorTableName, 100, 1000, 0)
	}
	waitOracleSpatialCDCOffsetsConverged(t, ctx, stateRepo, target, task.ID, resource.SourceIdentity, task.ApplyIdentity)
	if calls := metadataScanCalls.Load(); calls != 1 {
		t.Fatalf("Oracle Spatial target metadata scan calls=%d, want 1", calls)
	}

	cancelRunner()
	if err := <-runnerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Oracle Spatial final runner error=%v", err)
	}
	if err := leaseRepo.Finish(context.Background(), *activeClaim, commonExecution.ExecutionStatusCancelled, "test_complete", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	cdcDataStopTaskViaAPI(t, apiRouter, task.ID, task.Name)
	captureStopped = true
	adminClient, err := oracleSpatialCDCKafkaAdminClient()
	if err != nil {
		t.Fatal(err)
	}
	defer adminClient.Close()
	assertOracleSpatialCDCCleanup(t, ctx, sourceDB, captureRepo, connectClient, kadm.NewClient(adminClient), task, resource)
}

func oracleSpatialCDCConnectionInfo() engineplugin.ConnectionInfo {
	return engineplugin.ConnectionInfo{
		"host":              cdcDataEnv("ADDP_TEST_ORACLE_HOST", "127.0.0.1"),
		"port":              cdcDataEnv("ADDP_TEST_ORACLE_PORT", "15210"),
		"service_name":      cdcDataEnv("ADDP_TEST_ORACLE_SERVICE_NAME", "FREEPDB1"),
		"user":              cdcDataEnv("ADDP_TEST_ORACLE_USER", "business"),
		"password":          cdcDataEnv("ADDP_TEST_ORACLE_PASSWORD", "business_oracle_password"),
		"cdc_database_name": cdcDataEnv("ADDP_TEST_ORACLE_CDC_DATABASE_NAME", "FREE"),
		"cdc_user":          cdcDataEnv("ADDP_TEST_ORACLE_CDC_USER", "C##ADDP_CDC"),
		"cdc_password":      cdcDataEnv("ADDP_TEST_ORACLE_CDC_PASSWORD", "addp_oracle_cdc_password"),
	}
}

func openOracleSpatialCDCDB(t *testing.T, info engineplugin.ConnectionInfo) *sql.DB {
	t.Helper()
	dsn, err := (&oracleplugin.OraclePlugin{}).BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("business Oracle is unavailable: %v", err)
	}
	return db
}

func createOracleSpatialCDCTestTable(t *testing.T, ctx context.Context, db *sql.DB, tableName, indexName string, dimension int, initialGeometry string) {
	t.Helper()
	quotedTable := oracleSpatialCDCQuoteIdentifier(tableName)
	execOracleSpatialCDC(t, ctx, db, `CREATE TABLE `+quotedTable+` (
		"ID" NUMBER(10,0) NOT NULL,
		"NAME" VARCHAR2(100 CHAR) NOT NULL,
		"SHAPE" MDSYS.SDO_GEOMETRY NOT NULL,
		CONSTRAINT `+oracleSpatialCDCQuoteIdentifier(tableName+"_PK")+` PRIMARY KEY ("ID")
	)`)
	dimInfo := `MDSYS.SDO_DIM_ARRAY(MDSYS.SDO_DIM_ELEMENT('X', -180, 180, 0.0000001), MDSYS.SDO_DIM_ELEMENT('Y', -90, 90, 0.0000001))`
	if dimension == 3 {
		dimInfo = `MDSYS.SDO_DIM_ARRAY(MDSYS.SDO_DIM_ELEMENT('X', -180, 180, 0.0000001), MDSYS.SDO_DIM_ELEMENT('Y', -90, 90, 0.0000001), MDSYS.SDO_DIM_ELEMENT('Z', -100000, 100000, 0.0000001))`
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO USER_SDO_GEOM_METADATA (TABLE_NAME, COLUMN_NAME, DIMINFO, SRID) VALUES (:1, 'SHAPE', `+dimInfo+`, 4326)`, tableName); err != nil {
		t.Fatal(err)
	}
	execOracleSpatialCDC(t, ctx, db, `CREATE INDEX `+oracleSpatialCDCQuoteIdentifier(indexName)+` ON `+quotedTable+` ("SHAPE") INDEXTYPE IS MDSYS.SPATIAL_INDEX_V2`)
	execOracleSpatialCDC(t, ctx, db, `ALTER TABLE `+quotedTable+` ADD SUPPLEMENTAL LOG DATA (ALL) COLUMNS`)
	execOracleSpatialCDC(t, ctx, db, `INSERT INTO `+quotedTable+` ("ID", "NAME", "SHAPE") VALUES (1, 'snapshot', `+initialGeometry+`)`)
}

func dropOracleSpatialCDCTestTable(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = :1`, tableName).Scan(&exists); err != nil {
		t.Errorf("inspect Oracle Spatial test table %s: %v", tableName, err)
		return
	}
	if exists != 0 {
		if _, err := db.ExecContext(ctx, `DROP TABLE `+oracleSpatialCDCQuoteIdentifier(tableName)+` PURGE`); err != nil {
			t.Errorf("drop Oracle Spatial test table %s: %v", tableName, err)
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM USER_SDO_GEOM_METADATA WHERE TABLE_NAME = :1`, tableName); err != nil {
		t.Errorf("delete Oracle Spatial test metadata %s: %v", tableName, err)
	}
}

func createOracleCDCNativeTypeTable(t *testing.T, ctx context.Context, db *sql.DB, tableName string) {
	t.Helper()
	quotedTable := oracleSpatialCDCQuoteIdentifier(tableName)
	execOracleSpatialCDC(t, ctx, db, `CREATE TABLE `+quotedTable+` (
		"ID" NUMBER(18,0) NOT NULL,
		"NAME" VARCHAR2(100 CHAR) NOT NULL,
		"SMALL_COUNT" NUMBER(9,0) NOT NULL,
		"LARGE_COUNT" NUMBER(18,0) NOT NULL,
		"AMOUNT" NUMBER(30,4) NOT NULL,
		"SCORE_FLOAT" BINARY_FLOAT NOT NULL,
		"SCORE_DOUBLE" BINARY_DOUBLE NOT NULL,
		"CREATED_AT" DATE NOT NULL,
		"CHANGED_AT" TIMESTAMP(3) NOT NULL,
		CONSTRAINT `+oracleSpatialCDCQuoteIdentifier(tableName+"_PK")+` PRIMARY KEY ("ID")
	)`)
	execOracleSpatialCDC(t, ctx, db, `ALTER TABLE `+quotedTable+` ADD SUPPLEMENTAL LOG DATA (ALL) COLUMNS`)
	execOracleSpatialCDC(t, ctx, db, oracleCDCNativeTypeInsertSQL(tableName, 1, "snapshot"))
}

func oracleCDCNativeTypeInsertSQL(tableName string, id int, name string) string {
	return fmt.Sprintf(`INSERT INTO %s ("ID", "NAME", "SMALL_COUNT", "LARGE_COUNT", "AMOUNT", "SCORE_FLOAT", "SCORE_DOUBLE", "CREATED_AT", "CHANGED_AT") VALUES (%d, '%s', 123456789, 123456789012345678, 12345678901234567890.1234, 12.5, 12345.6789, DATE '2026-01-02', TIMESTAMP '2026-01-02 03:04:05.678')`,
		oracleSpatialCDCQuoteIdentifier(tableName), id, strings.ReplaceAll(name, "'", "''"))
}

func beginOracleCDCNativeTypeBatch(t *testing.T, ctx context.Context, db *sql.DB, tableName string, firstID, count int, namePrefix string) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	statement := `INSERT INTO ` + oracleSpatialCDCQuoteIdentifier(tableName) + `
		("ID", "NAME", "SMALL_COUNT", "LARGE_COUNT", "AMOUNT", "SCORE_FLOAT", "SCORE_DOUBLE", "CREATED_AT", "CHANGED_AT")
		VALUES (:1, :2, 123456789, 123456789012345678, 12345678901234567890.1234, 12.5, 12345.6789, DATE '2026-01-02', TIMESTAMP '2026-01-02 03:04:05.678')`
	prepared, err := tx.PrepareContext(ctx, statement)
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	defer prepared.Close()
	for id := firstID; id < firstID+count; id++ {
		if _, err := prepared.ExecContext(ctx, id, fmt.Sprintf("%s-%d", namePrefix, id)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert Oracle CDC transaction row %d: %v", id, err)
		}
	}
	return tx
}

func waitOracleCDCSourceTransactionObservation(
	t *testing.T,
	ctx context.Context,
	plans *capture.DatabasePlanResolver,
	connectClient *capture.ConnectClient,
	resource *models.CaptureResource,
	minimumUndoRecords uint64,
) {
	t.Helper()
	plan, err := plans.ResolveForObservation(ctx, resource)
	if err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		var transactions *models.CaptureSourceTransactions
		var undoRecords uint64
		offsets, offsetsErr := connectClient.Offsets(ctx, resource.ConnectorName)
		observation, observationErr := (capture.DatabaseSourceResources{}).Observe(ctx, plan, resource, offsets, time.Now())
		if observationErr == nil && observation != nil && observation.TransactionsError == nil {
			transactions = observation.Transactions
		}
		if transactions != nil {
			undoRecords, _ = strconv.ParseUint(transactions.UsedUndoRecords, 10, 64)
		}
		if transactions != nil && transactions.Status == "available" && transactions.ActiveCount > 0 &&
			transactions.OldestStartPosition != "" && transactions.OldestDurationSeconds != nil && undoRecords >= minimumUndoRecords {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait Oracle source transaction observation: transactions=%#v undo_records=%d offsets_err=%v observation_err=%v", transactions, undoRecords, offsetsErr, observationErr)
		case <-ticker.C:
		}
	}
}

func assertOracleCDCRecoveryWindowObservation(
	t *testing.T,
	ctx context.Context,
	plans *capture.DatabasePlanResolver,
	connectClient *capture.ConnectClient,
	resource *models.CaptureResource,
) {
	t.Helper()
	plan, err := plans.ResolveForObservation(ctx, resource)
	if err != nil {
		t.Fatal(err)
	}
	var healthy *models.CaptureSourceRecovery
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for healthy == nil {
		offsets, offsetsErr := connectClient.Offsets(ctx, resource.ConnectorName)
		observation, observationErr := (capture.DatabaseSourceResources{}).Observe(ctx, plan, resource, offsets, time.Now())
		if offsetsErr == nil && observationErr == nil && observation != nil && observation.RecoveryError == nil {
			healthy = observation.Recovery
		}
		if healthy != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait Oracle recovery window observation: offsets_err=%v observation_err=%v", offsetsErr, observationErr)
		case <-ticker.C:
		}
	}
	if healthy.Health != "healthy" || healthy.CapturePosition == "" || healthy.CurrentPosition == "" ||
		healthy.EarliestAvailablePosition == "" || healthy.PositionHeadroom == "" || healthy.EarliestAvailableAt == nil || healthy.WindowSeconds == nil {
		t.Fatalf("Oracle healthy recovery observation=%#v", healthy)
	}
	staleOffsets := &capture.ConnectorOffsets{Offsets: []capture.ConnectorOffset{{
		Offset: map[string]json.RawMessage{"scn": json.RawMessage(`"0"`)},
	}}}
	staleObservation, err := (capture.DatabaseSourceResources{}).Observe(ctx, plan, resource, staleOffsets, time.Now())
	if err != nil || staleObservation == nil || staleObservation.RecoveryError != nil || staleObservation.Recovery == nil {
		t.Fatalf("Oracle stale recovery observation=%#v err=%v", staleObservation, err)
	}
	if staleObservation.Recovery.Health != "critical" || !strings.HasPrefix(staleObservation.Recovery.PositionHeadroom, "-") ||
		staleObservation.Recovery.CapturePosition != "0" {
		t.Fatalf("Oracle stale recovery facts=%#v", staleObservation.Recovery)
	}
}

func oracleCDCNativeTypeTaskConfig(sourceTable string, target *cdcDataE2ETarget) models.JSONMap {
	return models.JSONMap{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
		"load":    map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{"type": "cdc", "bootstrap": "initial_snapshot"}},
		"source": map[string]interface{}{
			"locator": fmt.Sprintf("addp://engine/22/path/BUSINESS/%s?type=table", sourceTable), "data_type": "table", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": target.ParentLocator(), "name": target.Table, "data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert_delete", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{
				map[string]interface{}{"source": "ID", "target": "id", "target_type": "bigint", "nullable": false},
				map[string]interface{}{"source": "NAME", "target": "name", "target_type": "string", "nullable": false},
				map[string]interface{}{"source": "SMALL_COUNT", "target": "small_count", "target_type": "int", "nullable": false},
				map[string]interface{}{"source": "LARGE_COUNT", "target": "large_count", "target_type": "bigint", "nullable": false},
				map[string]interface{}{"source": "AMOUNT", "target": "amount", "target_type": "decimal", "precision": 30, "scale": 4, "nullable": false},
				map[string]interface{}{"source": "SCORE_FLOAT", "target": "score_float", "target_type": "float", "nullable": false},
				map[string]interface{}{"source": "SCORE_DOUBLE", "target": "score_double", "target_type": "double", "nullable": false},
				map[string]interface{}{"source": "CREATED_AT", "target": "created_at", "target_type": "timestamp", "nullable": false},
				map[string]interface{}{"source": "CHANGED_AT", "target": "changed_at", "target_type": "timestamp", "nullable": false},
			},
		}},
	}
}

func assertOracleCDCNativeTypeTarget(t *testing.T, ctx context.Context, target *cdcDataE2ETarget, id int64, name string) {
	t.Helper()
	var actualName, amount, createdAt, changedAt string
	var smallCount int32
	var largeCount int64
	var scoreFloat float32
	var scoreDouble float64
	var err error
	if target.Type == "postgresql" {
		err = target.DB.QueryRowContext(ctx, `SELECT name, small_count, large_count, amount::text, score_float, score_double,
			to_char(created_at, 'YYYY-MM-DD HH24:MI:SS.MS'), to_char(changed_at, 'YYYY-MM-DD HH24:MI:SS.MS')
			FROM `+pq.QuoteIdentifier(target.Namespace)+`.`+pq.QuoteIdentifier(target.Table)+` WHERE id=$1`, id).
			Scan(&actualName, &smallCount, &largeCount, &amount, &scoreFloat, &scoreDouble, &createdAt, &changedAt)
	} else if target.Type == "mysql" {
		err = target.DB.QueryRowContext(ctx, "SELECT name, small_count, large_count, CAST(amount AS CHAR), score_float, score_double, "+
			"DATE_FORMAT(created_at, '%Y-%m-%d %H:%i:%s.%f'), DATE_FORMAT(changed_at, '%Y-%m-%d %H:%i:%s.%f') FROM "+
			mysqlCDCQuoteIdentifier(target.Namespace)+"."+mysqlCDCQuoteIdentifier(target.Table)+" WHERE id = ?", id).
			Scan(&actualName, &smallCount, &largeCount, &amount, &scoreFloat, &scoreDouble, &createdAt, &changedAt)
		createdAt = strings.TrimSuffix(createdAt, "000")
		changedAt = strings.TrimSuffix(changedAt, "000")
	} else {
		err = target.DB.QueryRowContext(ctx, `SELECT "name", "small_count", "large_count", TO_CHAR("amount", 'FM99999999999999999999999999999999999999D9999', 'NLS_NUMERIC_CHARACTERS=''.,'''),
			"score_float", "score_double", TO_CHAR("created_at", 'YYYY-MM-DD HH24:MI:SS.FF3'), TO_CHAR("changed_at", 'YYYY-MM-DD HH24:MI:SS.FF3')
			FROM `+oracleSpatialCDCQuoteIdentifier(target.Namespace)+`.`+oracleSpatialCDCQuoteIdentifier(target.Table)+` WHERE "id"=:1`, id).
			Scan(&actualName, &smallCount, &largeCount, &amount, &scoreFloat, &scoreDouble, &createdAt, &changedAt)
	}
	if err != nil {
		t.Fatal(err)
	}
	if actualName != name || smallCount != 123456789 || largeCount != 123456789012345678 || amount != "12345678901234567890.1234" ||
		scoreFloat != 12.5 || scoreDouble != 12345.6789 || createdAt != "2026-01-02 00:00:00.000" || changedAt != "2026-01-02 03:04:05.678" {
		t.Fatalf("ordinary Oracle CDC type matrix mismatch: name=%q small=%d large=%d amount=%q float=%v double=%v created=%q changed=%q",
			actualName, smallCount, largeCount, amount, scoreFloat, scoreDouble, createdAt, changedAt)
	}
}

func assertOracleCDCNativeTypeUpdatedTarget(t *testing.T, ctx context.Context, target *cdcDataE2ETarget) {
	t.Helper()
	var amount, changedAt string
	var err error
	if target.Type == "postgresql" {
		err = target.DB.QueryRowContext(ctx, `SELECT amount::text, to_char(changed_at, 'YYYY-MM-DD HH24:MI:SS.MS') FROM `+
			pq.QuoteIdentifier(target.Namespace)+`.`+pq.QuoteIdentifier(target.Table)+` WHERE id=2`).Scan(&amount, &changedAt)
	} else if target.Type == "mysql" {
		err = target.DB.QueryRowContext(ctx, "SELECT CAST(amount AS CHAR), DATE_FORMAT(changed_at, '%Y-%m-%d %H:%i:%s.%f') FROM "+
			mysqlCDCQuoteIdentifier(target.Namespace)+"."+mysqlCDCQuoteIdentifier(target.Table)+" WHERE id = ?", 2).Scan(&amount, &changedAt)
		changedAt = strings.TrimSuffix(changedAt, "000")
	} else {
		err = target.DB.QueryRowContext(ctx, `SELECT TO_CHAR("amount", 'FM99999999999999999999999999999999999999D9999', 'NLS_NUMERIC_CHARACTERS=''.,'''),
			TO_CHAR("changed_at", 'YYYY-MM-DD HH24:MI:SS.FF3') FROM `+
			oracleSpatialCDCQuoteIdentifier(target.Namespace)+`.`+oracleSpatialCDCQuoteIdentifier(target.Table)+` WHERE "id"=:1`, 2).Scan(&amount, &changedAt)
	}
	if err != nil {
		t.Fatal(err)
	}
	if amount != "98765432109876543210.4321" || changedAt != "2026-02-03 04:05:06.789" {
		t.Fatalf("ordinary Oracle CDC update mismatch: amount=%q changed=%q", amount, changedAt)
	}
}

func oracleSpatialCDCDataTaskConfig(sourceTable string, target *cdcDataE2ETarget) models.JSONMap {
	return models.JSONMap{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
		"load":    map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{"type": "cdc", "bootstrap": "initial_snapshot"}},
		"source": map[string]interface{}{
			"locator": fmt.Sprintf("addp://engine/22/path/BUSINESS/%s?type=table", sourceTable), "data_type": "table", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": target.ParentLocator(), "name": target.Table, "data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert_delete", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{
				map[string]interface{}{"source": "ID", "target": "id", "target_type": "bigint", "nullable": false},
				map[string]interface{}{"source": "NAME", "target": "name", "target_type": "string", "nullable": false},
				map[string]interface{}{"source": "SHAPE", "target": "geometry", "target_type": "geometry", "nullable": false},
			},
		}},
	}
}

func waitOracleSpatialCDCTargetGeometry(
	t *testing.T,
	ctx context.Context,
	runnerDone <-chan error,
	target *cdcDataE2ETarget,
	id int64,
	wantName string,
	geometryCase oracleSpatialCDCGeometryCase,
	wantEWKT string,
) {
	t.Helper()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		name, geometryType, geometryText, srid, dimension, err := oracleSpatialCDCTargetGeometry(ctx, target, id)
		wantGeometryText := wantEWKT
		if target.Type == "mysql" {
			wantGeometryText = strings.TrimPrefix(wantEWKT, "SRID=4326;")
		}
		geometryMatches := geometryText == wantGeometryText
		if target.Type == "oracle" {
			wantGeometryText = strings.TrimPrefix(wantEWKT, "SRID=4326;")
			geometryMatches = oracleSpatialCDCGeometryTextEqual(geometryText, wantGeometryText)
		}
		if err == nil && name == wantName && strings.EqualFold(geometryType, geometryCase.geometryType) && srid == 4326 && dimension == geometryCase.dimension && geometryMatches {
			return
		}
		select {
		case runnerErr := <-runnerDone:
			t.Fatalf("Oracle Spatial runner exited before %s target geometry converged: %v (last query error=%v type=%q srid=%d dimension=%d geometry=%q)", target.Type, runnerErr, err, geometryType, srid, dimension, geometryText)
		case <-ctx.Done():
			t.Fatalf("wait Oracle Spatial %s target id=%d geometry=%q: %v (last query error=%v type=%q srid=%d dimension=%d geometry=%q)", target.Type, id, wantGeometryText, ctx.Err(), err, geometryType, srid, dimension, geometryText)
		case <-ticker.C:
		}
	}
}

func oracleSpatialCDCGeometryTextEqual(actual, expected string) bool {
	actualGeometry, err := wkt.Unmarshal(actual)
	if err != nil {
		return false
	}
	expectedGeometry, err := wkt.Unmarshal(expected)
	if err != nil {
		return false
	}
	if actualGeometry.Layout() != expectedGeometry.Layout() || len(actualGeometry.FlatCoords()) != len(expectedGeometry.FlatCoords()) {
		return false
	}
	for index, coordinate := range actualGeometry.FlatCoords() {
		if coordinate != expectedGeometry.FlatCoords()[index] {
			return false
		}
	}
	return true
}

func oracleSpatialCDCTargetGeometry(ctx context.Context, target *cdcDataE2ETarget, id int64) (name, geometryType, geometryText string, srid, dimension int, err error) {
	if target.Type == "postgresql" {
		err = target.DB.QueryRowContext(ctx,
			`SELECT name, GeometryType(geometry), ST_SRID(geometry), ST_NDims(geometry), ST_AsEWKT(geometry) FROM `+
				pq.QuoteIdentifier(target.Namespace)+`.`+pq.QuoteIdentifier(target.Table)+` WHERE id=$1`, id,
		).Scan(&name, &geometryType, &srid, &dimension, &geometryText)
		return
	}
	if target.Type == "oracle" {
		dimension = 2
		err = target.DB.QueryRowContext(ctx, `SELECT target_row."name",
			CASE MOD(target_row."geometry".SDO_GTYPE, 1000)
				WHEN 1 THEN 'POINT' WHEN 3 THEN 'POLYGON' WHEN 7 THEN 'MULTIPOLYGON'
			END,
			target_row."geometry".SDO_SRID, SDO_UTIL.TO_WKTGEOMETRY(target_row."geometry") FROM (
				SELECT "name", "geometry" FROM `+
			oracleSpatialCDCQuoteIdentifier(target.Namespace)+`.`+oracleSpatialCDCQuoteIdentifier(target.Table)+` WHERE "id"=:1
			) target_row`, id,
		).Scan(&name, &geometryType, &srid, &geometryText)
		return
	}
	dimension = 2
	err = target.DB.QueryRowContext(ctx,
		"SELECT name, ST_GeometryType(geometry), ST_SRID(geometry), ST_AsText(geometry) FROM "+
			mysqlCDCQuoteIdentifier(target.Namespace)+"."+mysqlCDCQuoteIdentifier(target.Table)+" WHERE id = ?", id,
	).Scan(&name, &geometryType, &srid, &geometryText)
	return
}

func waitOracleSpatialCDCConnectorState(t *testing.T, ctx context.Context, client *capture.ConnectClient, name, want string) {
	t.Helper()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := client.Status(ctx, name)
		if err == nil && status.ConnectorState == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait Oracle Spatial connector %s state=%s: status=%#v err=%v", name, want, status, err)
		case <-ticker.C:
		}
	}
}

func waitOracleSpatialCDCConnectorHealthy(t *testing.T, ctx context.Context, client *capture.ConnectClient, name string) {
	t.Helper()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := client.Status(ctx, name)
		if err == nil && status.ConnectorState == "RUNNING" && len(status.TaskStates) == 1 && status.TaskStates[0] == "RUNNING" {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait healthy Oracle Spatial connector %s: status=%#v err=%v", name, status, err)
		case <-ticker.C:
		}
	}
}

func insertOracleSpatialCDCBatch(t *testing.T, ctx context.Context, db *sql.DB, tableName string, firstID, count int) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	statement := `INSERT INTO ` + oracleSpatialCDCQuoteIdentifier(tableName) + ` ("ID", "NAME", "SHAPE") ` +
		`SELECT :1, :2, "SHAPE" FROM ` + oracleSpatialCDCQuoteIdentifier(tableName) + ` WHERE "ID" = 1`
	prepared, err := tx.PrepareContext(ctx, statement)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	for id := firstID; id < firstID+count; id++ {
		if _, err := prepared.ExecContext(ctx, id, fmt.Sprintf("batch-%d", id)); err != nil {
			t.Fatalf("insert Oracle Spatial CDC batch row %d: %v", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit Oracle Spatial CDC batch: %v", err)
	}
}

func rollbackOracleSpatialCDCInsert(t *testing.T, ctx context.Context, db *sql.DB, tableName string, id int) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+oracleSpatialCDCQuoteIdentifier(tableName)+` ("ID", "NAME", "SHAPE") SELECT :1, 'rolled-back', "SHAPE" FROM `+oracleSpatialCDCQuoteIdentifier(tableName)+` WHERE "ID" = 1`, id); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert rolled-back Oracle Spatial CDC row: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback Oracle Spatial CDC transaction: %v", err)
	}
}

func insertOracleSpatialCDCConcurrentBatches(t *testing.T, ctx context.Context, db *sql.DB, tableName string, firstID, workers, rowsPerWorker int) {
	t.Helper()
	errorsByWorker := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		go func(worker int) {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				errorsByWorker <- err
				return
			}
			defer tx.Rollback()
			statement := `INSERT INTO ` + oracleSpatialCDCQuoteIdentifier(tableName) + ` ("ID", "NAME", "SHAPE") ` +
				`SELECT :1, :2, "SHAPE" FROM ` + oracleSpatialCDCQuoteIdentifier(tableName) + ` WHERE "ID" = 1`
			for row := 0; row < rowsPerWorker; row++ {
				id := firstID + worker*rowsPerWorker + row
				if _, err := tx.ExecContext(ctx, statement, id, fmt.Sprintf("concurrent-%d", id)); err != nil {
					errorsByWorker <- fmt.Errorf("worker %d row %d: %w", worker, id, err)
					return
				}
			}
			errorsByWorker <- tx.Commit()
		}(worker)
	}
	for worker := 0; worker < workers; worker++ {
		if err := <-errorsByWorker; err != nil {
			t.Fatalf("commit concurrent Oracle Spatial CDC batch: %v", err)
		}
	}
}

func assertOracleSpatialCDCMirrorCount(t *testing.T, ctx context.Context, db *sql.DB, mirrorTable string, firstID, lastID, want int) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+oracleSpatialCDCQuoteIdentifier(mirrorTable)+` WHERE "ID" BETWEEN :1 AND :2`, firstID, lastID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("Oracle Spatial mirror rows in [%d,%d]=%d, want %d", firstID, lastID, count, want)
	}
}

func waitOracleSpatialCDCTargetCount(
	t *testing.T,
	ctx context.Context,
	runnerDone <-chan error,
	target *cdcDataE2ETarget,
	firstID, lastID int,
	want int,
) {
	t.Helper()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int
		var err error
		if target.Type == "postgresql" {
			err = target.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+pq.QuoteIdentifier(target.Namespace)+`.`+pq.QuoteIdentifier(target.Table)+` WHERE id BETWEEN $1 AND $2`, firstID, lastID).Scan(&count)
		} else if target.Type == "mysql" {
			err = target.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+mysqlCDCQuoteIdentifier(target.Namespace)+"."+mysqlCDCQuoteIdentifier(target.Table)+" WHERE id BETWEEN ? AND ?", firstID, lastID).Scan(&count)
		} else {
			err = target.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+oracleSpatialCDCQuoteIdentifier(target.Namespace)+"."+oracleSpatialCDCQuoteIdentifier(target.Table)+" WHERE \"id\" BETWEEN :1 AND :2", firstID, lastID).Scan(&count)
		}
		if err == nil && count == want {
			return
		}
		select {
		case runnerErr := <-runnerDone:
			t.Fatalf("Oracle Spatial runner exited before %s target count converged: %v (range=[%d,%d] count=%d query_error=%v)", target.Type, runnerErr, firstID, lastID, count, err)
		case <-ctx.Done():
			t.Fatalf("wait Oracle Spatial %s target count range=[%d,%d] want=%d: %v (count=%d query_error=%v)", target.Type, firstID, lastID, want, ctx.Err(), count, err)
		case <-ticker.C:
		}
	}
}

func assertOracleCDCTargetCount(t *testing.T, ctx context.Context, target *cdcDataE2ETarget, firstID, lastID, want int) {
	t.Helper()
	var count int
	var err error
	if target.Type == "postgresql" {
		err = target.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+pq.QuoteIdentifier(target.Namespace)+`.`+pq.QuoteIdentifier(target.Table)+` WHERE id BETWEEN $1 AND $2`, firstID, lastID).Scan(&count)
	} else if target.Type == "mysql" {
		err = target.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+mysqlCDCQuoteIdentifier(target.Namespace)+"."+mysqlCDCQuoteIdentifier(target.Table)+" WHERE id BETWEEN ? AND ?", firstID, lastID).Scan(&count)
	} else {
		err = target.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+oracleSpatialCDCQuoteIdentifier(target.Namespace)+"."+oracleSpatialCDCQuoteIdentifier(target.Table)+" WHERE \"id\" BETWEEN :1 AND :2", firstID, lastID).Scan(&count)
	}
	if err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("Oracle CDC %s target rows in [%d,%d]=%d, want %d", target.Type, firstID, lastID, count, want)
	}
}

func waitOracleSpatialCDCOffsetsConverged(
	t *testing.T,
	ctx context.Context,
	states *repository.SyncStateRepository,
	target *cdcDataE2ETarget,
	taskID uint,
	sourceIdentity, applyIdentity string,
) {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		committed, committedErr := oracleSpatialCDCCommittedOffset(ctx, states, taskID, sourceIdentity)
		ledger, ledgerErr := target.ledgerOffset(ctx, applyIdentity)
		if committedErr == nil && ledgerErr == nil && committed > 0 && ledger == committed {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait Oracle Spatial offsets: committed=%d ledger=%d committed_err=%v ledger_err=%v", committed, ledger, committedErr, ledgerErr)
		case <-ticker.C:
		}
	}
}

func oracleSpatialCDCCommittedOffset(ctx context.Context, states *repository.SyncStateRepository, taskID uint, sourceIdentity string) (int64, error) {
	values, err := states.List(ctx, taskID, sourceIdentity)
	if err != nil {
		return 0, err
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("expected one Oracle Spatial sync state, got %d", len(values))
	}
	position, ok, err := syncStatePosition(&values[0])
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("Oracle Spatial sync state has no committed position")
	}
	return kafkaNextOffset(position)
}

func injectOracleSpatialCDCContainerFault(t *testing.T, ctx context.Context, sourceInfo engineplugin.ConnectionInfo, connectClient *capture.ConnectClient, connectorName string) {
	t.Helper()
	container := cdcDataEnv("ADDP_TEST_ORACLE_CONTAINER", "business-oracle")
	if output, err := exec.CommandContext(ctx, "docker", "pause", container).CombinedOutput(); err != nil {
		t.Fatalf("pause Oracle container %s: %v: %s", container, err, strings.TrimSpace(string(output)))
	}
	paused := true
	t.Cleanup(func() {
		if paused {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = exec.CommandContext(cleanupCtx, "docker", "unpause", container).Run()
		}
	})
	dsn, err := (&oracleplugin.OraclePlugin{}).BuildDSN(sourceInfo)
	if err != nil {
		t.Fatal(err)
	}
	probeDB, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer probeDB.Close()
	probeDone := make(chan error, 1)
	var probe int
	go func() {
		probeDone <- probeDB.QueryRowContext(context.Background(), `SELECT 1 FROM DUAL`).Scan(&probe)
	}()
	select {
	case probeErr := <-probeDone:
		if probeErr == nil {
			t.Fatal("Oracle fault injection did not interrupt a fresh source connection")
		}
	case <-time.After(3 * time.Second):
		// go-ora may not observe context cancellation while the container is
		// frozen; a connection that remains silent is itself the outage signal.
	}
	if output, err := exec.CommandContext(ctx, "docker", "unpause", container).CombinedOutput(); err != nil && !strings.Contains(strings.ToLower(string(output)), "not paused") {
		t.Fatalf("unpause Oracle container %s: %v: %s", container, err, strings.TrimSpace(string(output)))
	}
	paused = false
	recoveryDB, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer recoveryDB.Close()
	deadline := time.Now().Add(90 * time.Second)
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := recoveryDB.QueryRowContext(probeCtx, `SELECT 1 FROM DUAL`).Scan(&probe)
		cancel()
		if err == nil && probe == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Oracle source did not recover after container unpause: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	waitOracleSpatialCDCConnectorHealthy(t, ctx, connectClient, connectorName)
}

func assertOracleSpatialCDCCleanup(
	t *testing.T,
	ctx context.Context,
	sourceDB *sql.DB,
	captures *repository.CaptureRepository,
	connectClient *capture.ConnectClient,
	admin *kadm.Client,
	task models.TransferTask,
	resource *models.CaptureResource,
) {
	t.Helper()
	stopped, err := captures.GetLatest(ctx, task.ID, task.TenantID)
	if err != nil || stopped.Status != models.CaptureStatusStopped {
		t.Fatalf("stopped Oracle Spatial capture=%#v err=%v", stopped, err)
	}
	if _, err := connectClient.Status(ctx, resource.ConnectorName); !errors.Is(err, capture.ErrConnectorNotFound) {
		t.Fatalf("Oracle Spatial connector still exists: %v", err)
	}
	if resource.Oracle == nil {
		t.Fatal("Oracle Spatial provider resources are missing")
	}
	var sourceArtifacts int
	if err := sourceDB.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = :1) +
		(SELECT COUNT(*) FROM USER_TRIGGERS WHERE TRIGGER_NAME IN (:2, :3))
		FROM DUAL`, resource.Oracle.SpatialMirrorTableName, resource.Oracle.SpatialRowTriggerName, resource.Oracle.SpatialDDLGuardName).Scan(&sourceArtifacts); err != nil {
		t.Fatal(err)
	}
	if sourceArtifacts != 0 {
		t.Fatalf("Oracle Spatial source artifacts remain after Stop: %d", sourceArtifacts)
	}
	details, err := admin.ListTopics(ctx, resource.TopicName, resource.Oracle.SchemaHistoryTopicName)
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{resource.TopicName, resource.Oracle.SchemaHistoryTopicName} {
		if detail, ok := details[topic]; ok && detail.Err == nil {
			t.Fatalf("Oracle Spatial CDC topic %q still exists", topic)
		}
	}
	groups, err := admin.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := groups[resource.ConsumerGroup]; ok {
		t.Fatalf("Oracle Spatial CDC consumer group %q still exists", resource.ConsumerGroup)
	}
	aclResults, err := admin.DescribeACLs(ctx,
		kadm.NewACLs().Topics(resource.TopicName, resource.Oracle.SchemaHistoryTopicName).Groups(resource.ConsumerGroup).
			ResourcePatternType(kadm.ACLPatternLiteral).Allow().AllowHosts().Operations(kadm.OpAny),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range aclResults {
		if len(result.Described) != 0 {
			t.Fatalf("Oracle Spatial CDC ACLs remain after Stop: %+v", result.Described)
		}
	}
}

func assertOracleCDCCleanup(
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
		t.Fatalf("stopped ordinary Oracle capture=%#v err=%v", stopped, err)
	}
	if _, err := connectClient.Status(ctx, resource.ConnectorName); !errors.Is(err, capture.ErrConnectorNotFound) {
		t.Fatalf("ordinary Oracle connector still exists: %v", err)
	}
	if resource.Oracle == nil || resource.Oracle.SpatialArtifactsOwned || resource.Oracle.SpatialMirrorTableName != "" || resource.Oracle.SpatialRowTriggerName != "" || resource.Oracle.SpatialDDLGuardName != "" {
		t.Fatalf("ordinary Oracle capture owns Spatial artifacts: %#v", resource.Oracle)
	}
	details, err := admin.ListTopics(ctx, resource.TopicName, resource.Oracle.SchemaHistoryTopicName)
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{resource.TopicName, resource.Oracle.SchemaHistoryTopicName} {
		if detail, ok := details[topic]; ok && detail.Err == nil {
			t.Fatalf("ordinary Oracle CDC topic %q still exists", topic)
		}
	}
	groups, err := admin.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := groups[resource.ConsumerGroup]; ok {
		t.Fatalf("ordinary Oracle CDC consumer group %q still exists", resource.ConsumerGroup)
	}
	aclResults, err := admin.DescribeACLs(ctx,
		kadm.NewACLs().Topics(resource.TopicName, resource.Oracle.SchemaHistoryTopicName).Groups(resource.ConsumerGroup).
			ResourcePatternType(kadm.ACLPatternLiteral).Allow().AllowHosts().Operations(kadm.OpAny),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range aclResults {
		if len(result.Described) != 0 {
			t.Fatalf("ordinary Oracle CDC ACLs remain after Stop: %+v", result.Described)
		}
	}
}

func oracleSpatialCDCKafkaAdminClient() (*kgo.Client, error) {
	return kafka.NewClient(cdcDataKafkaConnection(engineplugin.ConnectionInfo{
		"bootstrap_servers": cdcDataEnv("ADDP_TEST_INFRA_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		"security_protocol": cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
		"sasl_mechanism":    cdcDataEnv("ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"),
		"username":          cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_USERNAME", "admin"),
		"password":          cdcDataEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_PASSWORD", "addp_kafka_admin"),
		"client_id":         "addp-transfer-oracle-spatial-cdc-e2e-admin",
	}))
}

func execOracleSpatialCDC(t *testing.T, ctx context.Context, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, statement); err != nil {
		t.Fatalf("execute Oracle Spatial CDC statement: %v", err)
	}
}

func oracleSpatialCDCQuoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func oracleSpatialCDCPolygon(minX, minY, maxX, maxY int) string {
	return fmt.Sprintf(`MDSYS.SDO_GEOMETRY(2003, 4326, NULL, MDSYS.SDO_ELEM_INFO_ARRAY(1, 1003, 1), MDSYS.SDO_ORDINATE_ARRAY(%d, %d, %d, %d, %d, %d, %d, %d, %d, %d))`,
		minX, minY, maxX, minY, maxX, maxY, minX, maxY, minX, minY)
}

func oracleSpatialCDCMultiPolygon(offset int) string {
	return fmt.Sprintf(`MDSYS.SDO_GEOMETRY(2007, 4326, NULL, MDSYS.SDO_ELEM_INFO_ARRAY(1, 1003, 1, 11, 1003, 1), MDSYS.SDO_ORDINATE_ARRAY(%d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d, %d))`,
		offset, offset, offset+4, offset, offset+4, offset+4, offset, offset+4, offset, offset,
		offset+6, offset+6, offset+10, offset+6, offset+10, offset+10, offset+6, offset+10, offset+6, offset+6)
}

func oracleSpatialCDCPointXYZ(x, y, z int) string {
	return fmt.Sprintf(`MDSYS.SDO_GEOMETRY(3001, 4326, MDSYS.SDO_POINT_TYPE(%d, %d, %d), NULL, NULL)`, x, y, z)
}
