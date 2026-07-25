package continuous

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/kafka"
	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	transferapi "github.com/addp/transfer/internal/api"
	"github.com/addp/transfer/internal/capture"
	transferconfig "github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/addp/transfer/internal/testpg"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgreSQLCDCDataPlaneViaPublicAPISnapshotUpdateDeleteCrashResumeAndStopCleanup(t *testing.T) {
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
	if err := repository.MigrateCaptureProviderResources(infraDB); err != nil {
		t.Fatalf("migrate legacy capture resources: %v", err)
	}
	if err := infraDB.AutoMigrate(
		&models.TransferTask{}, &models.SyncState{}, &models.RuntimeLease{}, &models.CaptureResource{},
		&models.PostgreSQLCaptureResource{}, &models.MySQLCaptureResource{}, &models.SchemaChangeRequest{},
	); err != nil {
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
		t.Fatalf("business PostgreSQL is unavailable: %v", err)
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
	capturePlanResolver := capture.NewDatabasePlanResolver(resolver)
	captureSupervisor, err := capture.NewSupervisor(
		captureRepo, capturePlanResolver, connectClient, topicAdmin,
		capture.DatabaseSourceResources{}, capture.SupervisorConfig{
			TopicRetention: time.Hour, TopicReplication: 1,
			ConnectLoopbackHost: cdcDataEnv("ADDP_TEST_KAFKA_CONNECT_LOOPBACK_HOST", "host.docker.internal"),
			ProvisioningTimeout: 60 * time.Second, StatusPollInterval: 500 * time.Millisecond, MonitorInterval: time.Second,
		}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	taskRepo := repository.NewTaskRepository(infraDB)
	leaseRepo := repository.NewRuntimeLeaseRepository(infraDB, repository.ContinuousRecoveryPolicy{
		InitialBackoff: time.Second, MaxBackoff: 4 * time.Second, MaxFailures: 3,
		CircuitOpenTime: 10 * time.Second, StabilityWindow: 30 * time.Second,
	})
	stateRepo := repository.NewSyncStateRepository(infraDB)
	executionService := service.NewExecutionService(infraDB, commonExecution.NewTaskExecutionRepository(infraDB))
	var metaScanCalls atomic.Int32
	metaScanEntered := make(chan struct{})
	releaseMetaScan := make(chan struct{}, 1)
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := metaScanCalls.Add(1)
		if call == 1 {
			close(metaScanEntered)
			<-releaseMetaScan
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"execution_id":"meta-scan-%d"}`, call)
	}))
	defer func() {
		select {
		case releaseMetaScan <- struct{}{}:
		default:
		}
		metaServer.Close()
	}()
	taskService := service.NewTaskService(infraDB, nil, &transferconfig.Config{
		ContinuousRuntimeStopTimeout: 5 * time.Second, ContinuousRuntimeStopPollInterval: 50 * time.Millisecond,
		MetaServiceURL: metaServer.URL, InternalAPIKey: "cdc-data-e2e", SchemaChangeMetaScanClaimTTL: 2 * time.Minute,
	}, nil)
	taskService.SetEngineResolver(resolver)
	taskService.SetExecutionService(executionService)
	taskService.SetCaptureControl(captureSupervisor)
	taskService.SetSchemaChangeInspector(capturePlanResolver)
	apiRouter := cdcDataAPIRouter(taskService, uint(700000+suffix%90000), 700001)
	task := cdcDataCreateTaskViaAPI(t, apiRouter, cdcDataTaskConfig(schema, sourceTable, targetTable))
	defer cleanupCDCDataInfraRows(infraDB, task.ID)

	execution := cdcDataStartTaskViaAPI(t, apiRouter, task.ID)
	resource, err := captureRepo.GetLatest(ctx, task.ID, task.TenantID)
	if err != nil {
		t.Fatalf("load API-created CDC capture generation: %v", err)
	}
	captureStopped := false
	defer func() {
		if !captureStopped {
			_ = captureSupervisor.Stop(context.Background(), &task)
		}
	}()
	claim, err := leaseRepo.ClaimNext(ctx, "cdc-worker-a", time.Now(), 30*time.Second)
	if err != nil || claim == nil {
		t.Fatalf("first claim=%#v err=%v", claim, err)
	}
	if claim.Execution.ExecutionID != execution.ExecutionID {
		t.Fatalf("worker claimed execution %q, want API execution %q", claim.Execution.ExecutionID, execution.ExecutionID)
	}
	if task.ApplyIdentity != "" || claim.Task.ApplyIdentity == "" {
		t.Fatalf("apply identity exposure mismatch: API=%q worker=%q", task.ApplyIdentity, claim.Task.ApplyIdentity)
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
	if fmt.Sprint(schemaChange["unexpected_fields"]) != "[schema_drift]" || schemaChange["status"] != "pending" ||
		schemaChange["request_id"] == nil || blockedExecution.Metadata["stop_reason"] != "schema_change_blocked" {
		t.Fatalf("schema-blocked execution metadata=%#v", blockedExecution.Metadata)
	}
	blockedRetry := cdcDataExecution(blockedTask)
	if _, err := taskRepo.StartContinuousExecution(ctx, task.ID, task.TenantID, &blockedRetry); !errors.Is(err, repository.ErrContinuousTaskBlocked) {
		t.Fatalf("restart blocked task error=%v, want ErrContinuousTaskBlocked", err)
	}
	cancelSecond()

	change := cdcDataGetSchemaChangeViaAPI(t, apiRouter, task.ID)
	if !change.Approvable || change.SourcePartition != "0" || change.SourceOffset < committed ||
		len(change.SuggestedFields) != 1 || change.SuggestedFields[0].Source != "schema_drift" ||
		change.SuggestedFields[0].TargetType != "string" || !change.SuggestedFields[0].Nullable {
		t.Fatalf("PostgreSQL additive schema request=%#v", change)
	}
	approval := models.SchemaChangeField{
		Source: "schema_drift", Target: "schema_drift", TargetType: "string", Nullable: true,
	}
	const failCommitCallback = "test:fail_schema_change_task_commit"
	failTaskCommit := true
	if err := infraDB.Callback().Update().Before("gorm:update").Register(failCommitCallback, func(tx *gorm.DB) {
		if failTaskCommit && tx.Statement != nil && tx.Statement.Schema != nil &&
			tx.Statement.Schema.Table == (&models.TransferTask{}).TableName() {
			failTaskCommit = false
			tx.AddError(errors.New("injected Infra task commit failure after target DDL"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	failedApproval, status, body, err := cdcDataSchemaApprovalRequest(apiRouter, task.ID, []models.SchemaChangeField{approval})
	if removeErr := infraDB.Callback().Update().Remove(failCommitCallback); removeErr != nil {
		t.Fatal(removeErr)
	}
	if err != nil || status != http.StatusInternalServerError || failedApproval.ID != 0 {
		t.Fatalf("injected schema approval status=%d body=%s result=%#v err=%v", status, body, failedApproval, err)
	}
	if failTaskCommit {
		t.Fatal("Infra task commit failure callback did not run")
	}
	var targetColumnExists bool
	if err := businessDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema=$1 AND table_name=$2 AND column_name='schema_drift' AND is_nullable='YES'
		)`, schema, targetTable).Scan(&targetColumnExists); err != nil || !targetColumnExists {
		t.Fatalf("target DDL did not commit before injected Infra rollback: exists=%v err=%v", targetColumnExists, err)
	}
	var rolledBackRequest models.SchemaChangeRequest
	if err := infraDB.Where("id = ?", change.ID).First(&rolledBackRequest).Error; err != nil {
		t.Fatal(err)
	}
	var rolledBackResource models.CaptureResource
	if err := infraDB.First(&rolledBackResource, resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := infraDB.First(&blockedTask, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if rolledBackRequest.Status != models.SchemaChangeRequestPending || len(rolledBackRequest.ApprovedMappings) != 0 ||
		rolledBackResource.SchemaRevision != change.FromRevision || blockedTask.Status != models.TaskStatusBlocked ||
		blockedTask.DesiredState != models.TaskDesiredStateRunning {
		t.Fatalf("Infra state did not roll back after target DDL: request=%#v resource=%#v task=%#v", rolledBackRequest, rolledBackResource, blockedTask)
	}

	type concurrentApprovalResult struct {
		view   models.SchemaChangeRequestView
		status int
		body   string
		err    error
	}
	firstApproval := make(chan concurrentApprovalResult, 1)
	go func() {
		view, status, body, err := cdcDataSchemaApprovalRequest(apiRouter, task.ID, []models.SchemaChangeField{approval})
		firstApproval <- concurrentApprovalResult{view: view, status: status, body: body, err: err}
	}()
	select {
	case <-metaScanEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("first concurrent approval did not claim and call Meta scan")
	}
	secondApproval := make(chan concurrentApprovalResult, 1)
	go func() {
		view, status, body, err := cdcDataSchemaApprovalRequest(apiRouter, task.ID, []models.SchemaChangeField{approval})
		secondApproval <- concurrentApprovalResult{view: view, status: status, body: body, err: err}
	}()
	var secondResult concurrentApprovalResult
	select {
	case secondResult = <-secondApproval:
	case <-time.After(10 * time.Second):
		t.Fatal("second concurrent approval did not return while Meta scan claim was running")
	}
	if secondResult.err != nil || secondResult.status != http.StatusOK || secondResult.view.ID != change.ID ||
		secondResult.view.Status != models.SchemaChangeRequestApplied ||
		secondResult.view.MetadataScanStatus != models.SchemaChangeMetadataScanRunning || metaScanCalls.Load() != 1 {
		t.Fatalf("second concurrent schema approval status=%d body=%s result=%#v calls=%d err=%v",
			secondResult.status, secondResult.body, secondResult.view, metaScanCalls.Load(), secondResult.err)
	}
	releaseMetaScan <- struct{}{}
	var firstResult concurrentApprovalResult
	select {
	case firstResult = <-firstApproval:
	case <-time.After(10 * time.Second):
		t.Fatal("first concurrent approval did not finish after Meta scan release")
	}
	change = firstResult.view
	if firstResult.err != nil || firstResult.status != http.StatusOK || change.ID != secondResult.view.ID ||
		change.Status != models.SchemaChangeRequestApplied || change.ToRevision != change.FromRevision+1 ||
		change.MetadataScanStatus != models.SchemaChangeMetadataScanSuccess || change.MetadataScanAttempt != 1 ||
		change.MetadataScanExecutionID != "meta-scan-1" || metaScanCalls.Load() != 1 {
		t.Fatalf("first concurrent schema approval status=%d body=%s result=%#v calls=%d err=%v",
			firstResult.status, firstResult.body, change, metaScanCalls.Load(), firstResult.err)
	}
	if err := infraDB.Where("execution_id = ?", blockedExecution.ExecutionID).First(&blockedExecution).Error; err != nil {
		t.Fatal(err)
	}
	continuousMetadata, _ = blockedExecution.Metadata["continuous"].(map[string]interface{})
	schemaChange, _ = continuousMetadata["schema_change"].(map[string]interface{})
	metadataScan, _ := schemaChange["metadata_scan"].(map[string]interface{})
	if schemaChange["status"] != "applied" || metadataScan["status"] != "success" ||
		metadataScan["execution_id"] != "meta-scan-1" || metadataScan["attempt"] != float64(1) {
		t.Fatalf("applied schema change execution projection=%#v", schemaChange)
	}
	repeated := cdcDataApproveSchemaChangeViaAPI(t, apiRouter, task.ID, approval)
	if repeated.ID != change.ID || repeated.Status != models.SchemaChangeRequestApplied ||
		repeated.MetadataScanStatus != models.SchemaChangeMetadataScanSuccess || repeated.MetadataScanAttempt != 1 || metaScanCalls.Load() != 1 {
		t.Fatalf("idempotent PostgreSQL schema approval=%#v calls=%d, want request %d", repeated, metaScanCalls.Load(), change.ID)
	}
	if err := infraDB.First(&blockedTask, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	var appliedResource models.CaptureResource
	if err := infraDB.First(&appliedResource, resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	var requestCount int64
	if err := infraDB.Model(&models.SchemaChangeRequest{}).Where("capture_resource_id = ?", resource.ID).Count(&requestCount).Error; err != nil {
		t.Fatal(err)
	}
	appliedSpec, err := planner.ParseDatabaseCDCTaskSpec(blockedTask.Config)
	if err != nil {
		t.Fatal(err)
	}
	addedMappingCount := 0
	for _, field := range appliedSpec.Transforms[0].Fields {
		if field.Source == "schema_drift" {
			addedMappingCount++
		}
	}
	if blockedTask.Status != models.TaskStatusIdle || blockedTask.DesiredState != models.TaskDesiredStatePaused {
		t.Fatalf("approved task status=%q desired=%q", blockedTask.Status, blockedTask.DesiredState)
	}
	if appliedResource.SchemaRevision != change.ToRevision || requestCount != 1 || addedMappingCount != 1 {
		t.Fatalf("concurrent approval did not converge: revision=%d requests=%d added_mappings=%d", appliedResource.SchemaRevision, requestCount, addedMappingCount)
	}
	expiredLease := time.Now().Add(-time.Minute)
	if err := infraDB.Model(&models.SchemaChangeRequest{}).Where("id = ?", change.ID).Updates(map[string]interface{}{
		"metadata_scan_status": models.SchemaChangeMetadataScanRunning, "metadata_scan_claim_token": uuid.NewString(),
		"metadata_scan_lease_until": expiredLease, "metadata_scan_execution_id": "", "metadata_scan_error": "",
	}).Error; err != nil {
		t.Fatal(err)
	}
	readOnlyScan := cdcDataGetSchemaChangeViaAPI(t, apiRouter, task.ID)
	if readOnlyScan.MetadataScanStatus != models.SchemaChangeMetadataScanRunning || readOnlyScan.MetadataScanAttempt != 1 || metaScanCalls.Load() != 1 {
		t.Fatalf("schema change GET changed expired Meta scan claim: result=%#v calls=%d", readOnlyScan, metaScanCalls.Load())
	}
	recoveredScan := cdcDataApproveSchemaChangeViaAPI(t, apiRouter, task.ID, approval)
	if recoveredScan.MetadataScanStatus != models.SchemaChangeMetadataScanSuccess || recoveredScan.MetadataScanAttempt != 2 ||
		recoveredScan.MetadataScanExecutionID != "meta-scan-2" || recoveredScan.MetadataScanLeaseUntil != nil || metaScanCalls.Load() != 2 {
		t.Fatalf("expired Meta scan claim recovery=%#v calls=%d", recoveredScan, metaScanCalls.Load())
	}
	if err := infraDB.Where("execution_id = ?", blockedExecution.ExecutionID).First(&blockedExecution).Error; err != nil {
		t.Fatal(err)
	}
	continuousMetadata, _ = blockedExecution.Metadata["continuous"].(map[string]interface{})
	schemaChange, _ = continuousMetadata["schema_change"].(map[string]interface{})
	metadataScan, _ = schemaChange["metadata_scan"].(map[string]interface{})
	if metadataScan["status"] != "success" || metadataScan["execution_id"] != "meta-scan-2" || metadataScan["attempt"] != float64(2) {
		t.Fatalf("reclaimed Meta scan execution projection=%#v", schemaChange)
	}
	if err := businessDB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema=$1 AND table_name=$2 AND column_name='schema_drift' AND is_nullable='YES'
		)`, schema, targetTable).Scan(&targetColumnExists); err != nil || !targetColumnExists {
		t.Fatalf("PostgreSQL additive target column exists=%v err=%v", targetColumnExists, err)
	}

	resumeExecution := mysqlCDCResumeTaskViaAPI(t, apiRouter, task.ID)
	resumeClaim, err := leaseRepo.ClaimNext(ctx, "cdc-worker-c", time.Now(), 30*time.Second)
	if err != nil || resumeClaim == nil || resumeClaim.Execution.ExecutionID != resumeExecution.ExecutionID {
		t.Fatalf("schema resume claim=%#v err=%v", resumeClaim, err)
	}
	resumeCtx, cancelResume := context.WithCancel(ctx)
	resumeDone := make(chan error, 1)
	go func() { resumeDone <- runner.Run(resumeCtx, *resumeClaim) }()
	waitCDCDataRow(t, ctx, resumeDone, businessDB, schema, targetTable, 2, "blocked", true)
	var resumedValue string
	if err := businessDB.QueryRowContext(ctx, `SELECT schema_drift FROM `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(targetTable)+` WHERE id=2`).Scan(&resumedValue); err != nil || resumedValue != "new field" {
		t.Fatalf("resumed PostgreSQL additive value=%q err=%v", resumedValue, err)
	}
	if _, err := businessDB.ExecContext(ctx, `INSERT INTO `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(sourceTable)+` (
		id, name, amount, business_date, business_time, changed_at, changed_at_tz, enabled, payload, ref, schema_drift
	) VALUES (
		3, 'after-additive', 3.5000, DATE '2024-03-04', TIME '05:06:07.008',
		TIMESTAMP '2024-03-04 05:06:07.008', TIMESTAMPTZ '2024-03-04 05:06:07.008900+08', true,
		'{"after":true}'::jsonb, '550e8400-e29b-41d4-a716-446655440002'::uuid, 'continued'
	)`); err != nil {
		t.Fatal(err)
	}
	waitCDCDataRow(t, ctx, resumeDone, businessDB, schema, targetTable, 3, "after-additive", true)
	mysqlCDCPauseTaskViaAPI(t, apiRouter, task.ID)
	cancelResume()
	if err := <-resumeDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("schema resumed runner error=%v", err)
	}
	if err := leaseRepo.Finish(context.Background(), *resumeClaim, commonExecution.ExecutionStatusCancelled, "paused", "", time.Now()); err != nil {
		t.Fatal(err)
	}

	cdcDataStopTaskViaAPI(t, apiRouter, task.ID, task.Name)
	captureStopped = true
	assertCDCDataCaptureCleanup(t, ctx, businessDB, captureRepo, connectClient, task, resource)
}

func cdcDataAPIRouter(taskService *service.TaskService, tenantID, userID uint) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", tenantID)
		c.Set("user_id", userID)
		c.Next()
	})
	handler := transferapi.NewTaskHandler(taskService)
	router.POST("/api/v1/transfer/task-definitions", handler.CreateTask)
	router.POST("/api/v1/transfer/task-definitions/:id/start", handler.StartTask)
	router.POST("/api/v1/transfer/task-definitions/:id/pause", handler.PauseTask)
	router.POST("/api/v1/transfer/task-definitions/:id/resume", handler.ResumeTask)
	router.GET("/api/v1/transfer/task-definitions/:id/schema-change", handler.GetSchemaChange)
	router.POST("/api/v1/transfer/task-definitions/:id/schema-change/approve", handler.ApproveSchemaChange)
	router.POST("/api/v1/transfer/task-definitions/:id/stop", handler.StopTask)
	return router
}

func cdcDataGetSchemaChangeViaAPI(t *testing.T, router http.Handler, taskID uint) models.SchemaChangeRequestView {
	t.Helper()
	response := cdcDataAPIRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/transfer/task-definitions/%d/schema-change", taskID), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("get schema change API status=%d body=%s", response.Code, response.Body.String())
	}
	var result models.SchemaChangeRequestView
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func cdcDataApproveSchemaChangeViaAPI(t *testing.T, router http.Handler, taskID uint, fields ...models.SchemaChangeField) models.SchemaChangeRequestView {
	t.Helper()
	result, status, body, err := cdcDataSchemaApprovalRequest(router, taskID, fields)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("approve schema change API status=%d body=%s", status, body)
	}
	return result
}

func cdcDataSchemaApprovalRequest(router http.Handler, taskID uint, fields []models.SchemaChangeField) (models.SchemaChangeRequestView, int, string, error) {
	encoded, err := json.Marshal(models.ApproveSchemaChangeRequest{Fields: fields})
	if err != nil {
		return models.SchemaChangeRequestView{}, 0, "", err
	}
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/transfer/task-definitions/%d/schema-change/approve", taskID), bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK {
		return models.SchemaChangeRequestView{}, response.Code, body, nil
	}
	var result models.SchemaChangeRequestView
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		return models.SchemaChangeRequestView{}, response.Code, body, err
	}
	return result, response.Code, body, nil
}

func cdcDataCreateTaskViaAPI(t *testing.T, router http.Handler, config models.JSONMap) models.TransferTask {
	t.Helper()
	response := cdcDataAPIRequest(t, router, http.MethodPost, "/api/v1/transfer/task-definitions", models.CreateTaskRequest{
		Name: "cdc-data-e2e", TaskType: commonExecution.TaskTypeSync, Config: config, BatchSize: 100,
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create CDC task API status=%d body=%s", response.Code, response.Body.String())
	}
	var task models.TransferTask
	if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
		t.Fatalf("decode created CDC task: %v; body=%s", err, response.Body.String())
	}
	if task.ID == 0 || task.DesiredState != models.TaskDesiredStateStopped {
		t.Fatalf("created CDC task=%#v", task)
	}
	return task
}

func cdcDataStartTaskViaAPI(t *testing.T, router http.Handler, taskID uint) models.TaskExecution {
	t.Helper()
	response := cdcDataAPIRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/transfer/task-definitions/%d/start", taskID), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("start CDC task API status=%d body=%s", response.Code, response.Body.String())
	}
	var execution models.TaskExecution
	if err := json.Unmarshal(response.Body.Bytes(), &execution); err != nil {
		t.Fatalf("decode started CDC execution: %v; body=%s", err, response.Body.String())
	}
	if execution.ExecutionID == "" || execution.Status != models.ExecutionStatusPending {
		t.Fatalf("started CDC execution=%#v", execution)
	}
	return execution
}

func cdcDataStopTaskViaAPI(t *testing.T, router http.Handler, taskID uint, taskName string) {
	t.Helper()
	response := cdcDataAPIRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/transfer/task-definitions/%d/stop", taskID), models.StopTaskRequest{
		Confirmed: true, ConfirmationText: taskName,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("stop CDC task API status=%d body=%s", response.Code, response.Body.String())
	}
}

func cdcDataAPIRequest(t *testing.T, router http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode API request: %v", err)
		}
		payload = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, payload)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertCDCDataCaptureCleanup(
	t *testing.T,
	ctx context.Context,
	businessDB *sql.DB,
	captures *repository.CaptureRepository,
	connectClient *capture.ConnectClient,
	task models.TransferTask,
	resource *models.CaptureResource,
) {
	t.Helper()
	stopped, err := captures.GetLatest(ctx, task.ID, task.TenantID)
	if err != nil || stopped.Status != models.CaptureStatusStopped {
		t.Fatalf("stopped capture=%#v err=%v", stopped, err)
	}
	if _, err := connectClient.Status(ctx, resource.ConnectorName); !errors.Is(err, capture.ErrConnectorNotFound) {
		t.Fatalf("connector %q still exists after Stop API: %v", resource.ConnectorName, err)
	}
	if resource.PostgreSQL == nil {
		t.Fatal("PostgreSQL capture provider facts are missing")
	}
	var slotExists, publicationExists bool
	if err := businessDB.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name=$1)", resource.PostgreSQL.SlotName).Scan(&slotExists); err != nil {
		t.Fatal(err)
	}
	if err := businessDB.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname=$1)", resource.PostgreSQL.PublicationName).Scan(&publicationExists); err != nil {
		t.Fatal(err)
	}
	if slotExists || publicationExists {
		t.Fatalf("CDC source resources remain after Stop API: slot=%v publication=%v", slotExists, publicationExists)
	}
}

func cdcDataTaskConfig(schema, sourceTable, targetTable string) models.JSONMap {
	return models.JSONMap{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
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
