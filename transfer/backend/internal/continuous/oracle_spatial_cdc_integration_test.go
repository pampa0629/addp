package continuous

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/kafka"
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
		t.Run(testCase.name, func(t *testing.T) {
			runIntegrationOracleSpatialCDCGeometryCase(t, testCase)
		})
	}
}

func runIntegrationOracleSpatialCDCGeometryCase(t *testing.T, geometryCase oracleSpatialCDCGeometryCase) {
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

	target := openCDCDataE2ETarget(t, ctx, "postgresql", targetSchema, targetTable, nil, nil)
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
	}, nil)
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
		Resolver: resolver, States: stateRepo, Progress: leaseRepo, Captures: captureRepo,
		InfraKafkaConnection: cdcDataTransferKafkaConnection(), PollTimeout: 500 * time.Millisecond,
		DiagnosticsInterval: time.Second, MetadataScanner: metadataScanner,
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

	activeClaim := claim
	runnerCtx, cancelRunner := context.WithCancel(ctx)
	runnerDone := make(chan error, 1)
	go func(current repository.RuntimeLeaseClaim) { runnerDone <- runner.Run(runnerCtx, current) }(*claim)
	waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target.DB, target.Namespace, target.Table, 1, "snapshot", geometryCase, geometryCase.initialEWKT)

	if geometryCase.injectRecoveries {
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
		waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target.DB, target.Namespace, target.Table, 2, "connect-backlog", geometryCase, geometryCase.insertedEWKT)

		if os.Getenv("ADDP_ORACLE_SPATIAL_CDC_CONTAINER_FAULT") == "1" {
			injectOracleSpatialCDCContainerFault(t, ctx, sourceInfo, connectClient, resource.ConnectorName)
			execOracleSpatialCDC(t, ctx, sourceDB, `INSERT INTO `+oracleSpatialCDCQuoteIdentifier(tableName)+` ("ID", "NAME", "SHAPE") VALUES (3, 'oracle-recovered', `+geometryCase.insertedGeometry+`)`)
			waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target.DB, target.Namespace, target.Table, 3, "oracle-recovered", geometryCase, geometryCase.insertedEWKT)
		}
	} else {
		execOracleSpatialCDC(t, ctx, sourceDB, `INSERT INTO `+oracleSpatialCDCQuoteIdentifier(tableName)+` ("ID", "NAME", "SHAPE") VALUES (2, 'inserted', `+geometryCase.insertedGeometry+`)`)
		waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target.DB, target.Namespace, target.Table, 2, "inserted", geometryCase, geometryCase.insertedEWKT)
	}

	execOracleSpatialCDC(t, ctx, sourceDB, `UPDATE `+oracleSpatialCDCQuoteIdentifier(tableName)+` SET "SHAPE" = `+geometryCase.updatedGeometry+` WHERE "ID" = 1`)
	waitOracleSpatialCDCTargetGeometry(t, ctx, runnerDone, target.DB, target.Namespace, target.Table, 1, "snapshot", geometryCase, geometryCase.updatedEWKT)
	execOracleSpatialCDC(t, ctx, sourceDB, `DELETE FROM `+oracleSpatialCDCQuoteIdentifier(tableName)+` WHERE "ID" = 2`)
	target.WaitRow(t, ctx, runnerDone, 2, "", false)
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
	db *sql.DB,
	schema, table string,
	id int64,
	wantName string,
	geometryCase oracleSpatialCDCGeometryCase,
	wantEWKT string,
) {
	t.Helper()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	qualified := pq.QuoteIdentifier(schema) + "." + pq.QuoteIdentifier(table)
	for {
		var name, geometryType, ewkt string
		var srid, dimension int
		err := db.QueryRowContext(ctx, `SELECT name, GeometryType(geometry), ST_SRID(geometry), ST_NDims(geometry), ST_AsEWKT(geometry) FROM `+qualified+` WHERE id=$1`, id).
			Scan(&name, &geometryType, &srid, &dimension, &ewkt)
		if err == nil && name == wantName && strings.EqualFold(geometryType, geometryCase.geometryType) && srid == 4326 && dimension == geometryCase.dimension && ewkt == wantEWKT {
			return
		}
		select {
		case runnerErr := <-runnerDone:
			t.Fatalf("Oracle Spatial runner exited before target geometry converged: %v (last query error=%v type=%q srid=%d dimension=%d ewkt=%q)", runnerErr, err, geometryType, srid, dimension, ewkt)
		case <-ctx.Done():
			t.Fatalf("wait Oracle Spatial target id=%d geometry=%q: %v (last query error=%v type=%q srid=%d dimension=%d ewkt=%q)", id, wantEWKT, ctx.Err(), err, geometryType, srid, dimension, ewkt)
		case <-ticker.C:
		}
	}
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
		var ledger int64
		ledgerErr := target.DB.QueryRowContext(ctx, `SELECT next_offset FROM addp_transfer.apply_positions WHERE apply_identity=$1::uuid AND partition='0'`, applyIdentity).Scan(&ledger)
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
