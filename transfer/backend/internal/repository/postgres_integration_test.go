package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type runtimeLeaseMigrationFixture models.RuntimeLease

func (runtimeLeaseMigrationFixture) TableName() string {
	return "transfer_runtime_lease_migration_test.runtime_leases"
}

func TestIntegrationPostgresRuntimeLeaseSQLMigrationMatchesGORMModel(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	connInfo := testpg.ConnInfoFromEnv(t)
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN failed: %v", err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL failed: %v", err)
	}
	const schema = "transfer_runtime_lease_migration_test"
	if err := db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error; err != nil {
		t.Fatalf("drop stale migration test schema: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create migration test schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error })
	if err := db.Exec(`
		CREATE TABLE transfer_runtime_lease_migration_test.runtime_leases (
			id BIGSERIAL PRIMARY KEY,
			task_id BIGINT NOT NULL,
			execution_id VARCHAR(255) NOT NULL,
			owner_instance_id VARCHAR(255) NOT NULL,
			lease_until TIMESTAMPTZ NOT NULL,
			heartbeat_at TIMESTAMPTZ NOT NULL,
			fencing_token BIGINT NOT NULL,
			claimed_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CONSTRAINT uq_transfer_runtime_leases_task UNIQUE (task_id),
			CONSTRAINT uq_transfer_runtime_leases_execution UNIQUE (execution_id)
		);
		CREATE INDEX idx_transfer_runtime_leases_lease_until
			ON transfer_runtime_lease_migration_test.runtime_leases (lease_until);
		CREATE INDEX idx_transfer_runtime_leases_owner
			ON transfer_runtime_lease_migration_test.runtime_leases (owner_instance_id);
	`).Error; err != nil {
		t.Fatalf("create SQL-migrated runtime_leases fixture: %v", err)
	}

	if err := db.AutoMigrate(&runtimeLeaseMigrationFixture{}); err != nil {
		t.Fatalf("AutoMigrate SQL-migrated runtime_leases: %v", err)
	}
	var indexNames []string
	if err := db.Raw(`
		SELECT indexname
		FROM pg_indexes
		WHERE schemaname = ? AND tablename = 'runtime_leases'
		ORDER BY indexname
	`, schema).Scan(&indexNames).Error; err != nil {
		t.Fatalf("list runtime_leases indexes after AutoMigrate: %v", err)
	}
	wantIndexes := []string{
		"idx_transfer_runtime_leases_lease_until",
		"idx_transfer_runtime_leases_owner",
		"runtime_leases_pkey",
		"uq_transfer_runtime_leases_execution",
		"uq_transfer_runtime_leases_task",
	}
	if fmt.Sprint(indexNames) != fmt.Sprint(wantIndexes) {
		t.Fatalf("runtime_leases indexes = %v, want %v", indexNames, wantIndexes)
	}
}

func TestIntegrationPostgresAtomicTaskClaimAndSyncStateCAS(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	connInfo := testpg.ConnInfoFromEnv(t)
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN failed: %v", err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL failed: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatalf("create transfer schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := MigrateCaptureProviderResources(db); err != nil {
		t.Fatalf("migrate legacy capture resources: %v", err)
	}
	if err := db.AutoMigrate(
		&models.TransferTask{}, &models.SyncState{}, &models.RuntimeLease{}, &models.CaptureResource{},
		&models.PostgreSQLCaptureResource{}, &models.MySQLCaptureResource{},
	); err != nil {
		t.Fatalf("migrate transfer integration models: %v", err)
	}

	task := models.TransferTask{
		TenantID: 999999, Name: fmt.Sprintf("atomic-claim-%d", time.Now().UnixNano()),
		TaskType: commonExecution.TaskTypeSync,
		Config: models.JSONMap{
			"runtime": map[string]interface{}{"boundary": "bounded"},
			"load": map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{
				"type": "watermark", "field": "updated_at", "tie_breaker": []string{"id"}, "start": "committed", "end": "execution_upper_bound",
			}},
			"source": map[string]interface{}{"locator": "addp://engine/1/path/public/orders?type=table", "data_type": "table", "representation": "native"},
			"target": map[string]interface{}{"parent_locator": "addp://engine/2/path/public?type=schema", "name": "orders", "data_type": "table", "representation": "native", "policy": map[string]interface{}{"apply_mode": "upsert", "keys": []string{"id"}}},
		},
		BatchSize: 100, Status: models.TaskStatusIdle,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create integration task: %v", err)
	}
	t.Cleanup(func() {
		sourceTaskID := fmt.Sprint(task.ID)
		_ = db.Where("module = ? AND source_task_id = ?", commonExecution.ModuleTransfer, sourceTaskID).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Where("task_id = ?", task.ID).Delete(&models.SyncState{}).Error
		_ = db.Unscoped().Delete(&models.TransferTask{}, task.ID).Error
	})

	repo := NewTaskRepository(db)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			exec := claimTestExecution(task, fmt.Sprintf("pg-atomic-%d-%d", index, time.Now().UnixNano()))
			_, _, err := repo.ClaimExecution(context.Background(), task.ID, task.TenantID, &exec, "addp://engine/1/path/public/orders?type=table")
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrTaskAlreadyRunning):
			conflicts++
		default:
			t.Fatalf("unexpected claim error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claim results success=%d conflict=%d, want 1/1", successes, conflicts)
	}

	var state models.SyncState
	if err := db.Where("task_id = ?", task.ID).First(&state).Error; err != nil {
		t.Fatalf("load sync state: %v", err)
	}
	stateRepo := NewSyncStateRepository(db)
	position := models.JSONMap{"type": "watermark", "version": "v1", "cursor": map[string]interface{}{"values": []string{"2026-07-12T00:00:00Z", "1"}}}
	if err := stateRepo.CommitPosition(context.Background(), state.ID, state.StateVersion, state.FencingToken, position, "pg-atomic-winner"); err != nil {
		t.Fatalf("commit sync position: %v", err)
	}
	committedState, err := stateRepo.GetByID(context.Background(), state.ID)
	if err != nil || committedState.PositionCommittedAt == nil {
		t.Fatalf("committed position time = %#v, error = %v", committedState, err)
	}
	if err := stateRepo.CommitPosition(context.Background(), state.ID, state.StateVersion, state.FencingToken, position, "stale"); !errors.Is(err, ErrSyncStateFenced) {
		t.Fatalf("stale sync commit error = %v, want ErrSyncStateFenced", err)
	}
}

func TestIntegrationPostgresContinuousLeaseFencingAndCancellation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	connInfo := testpg.ConnInfoFromEnv(t)
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN failed: %v", err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL failed: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatalf("create transfer schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.TransferTask{}, &models.RuntimeLease{}); err != nil {
		t.Fatalf("migrate continuous models: %v", err)
	}
	task := models.TransferTask{
		TenantID: 999999, Name: fmt.Sprintf("continuous-lease-%d", time.Now().UnixNano()), TaskType: commonExecution.TaskTypeSync,
		Config: models.JSONMap{"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}}}, BatchSize: 100,
		Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Where("task_id = ?", task.ID).Delete(&models.RuntimeLease{}).Error
		_ = db.Where("task_id = ?", task.ID).Delete(&models.SyncState{}).Error
		_ = db.Where("module = ? AND source_task_id = ?", commonExecution.ModuleTransfer, fmt.Sprint(task.ID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Delete(&models.TransferTask{}, task.ID).Error
	})
	taskRepo := NewTaskRepository(db)
	now := time.Now()
	execution := claimTestExecution(task, uuid.NewString())
	if _, err := taskRepo.StartContinuousExecution(context.Background(), task.ID, task.TenantID, &execution); err != nil {
		t.Fatalf("start continuous execution: %v", err)
	}
	leaseRepo := NewRuntimeLeaseRepository(db, testContinuousRecoveryPolicy())
	claim, err := leaseRepo.ClaimNext(context.Background(), "worker-a", now, 30*time.Second)
	if err != nil || claim == nil {
		t.Fatalf("ClaimNext() claim=%#v error=%v", claim, err)
	}
	if claim.Lease.FencingToken != 1 || claim.Execution.Status != commonExecution.ExecutionStatusRunning {
		t.Fatalf("first claim = %#v", claim)
	}
	if claim.Execution.StartedAt == nil || !claim.Execution.StartedAt.Equal(now) {
		t.Fatalf("first claim started_at = %v, want %v", claim.Execution.StartedAt, now)
	}
	continuousMetadata, ok := claim.Execution.Metadata["continuous"].(map[string]interface{})
	if !ok || continuousMetadata["owner_instance_id"] != "worker-a" || fmt.Sprint(continuousMetadata["fencing_token"]) != "1" || continuousMetadata["heartbeat_at"] == nil {
		t.Fatalf("first claim continuous metadata = %#v", claim.Execution.Metadata)
	}
	diagnostics := ContinuousDiagnostics{
		SampledAt: now.Add(500 * time.Millisecond), Health: "degraded",
		DegradedHorizonSeconds: 21600, CriticalHorizonSeconds: 3600,
		Partitions: map[string]ContinuousPartitionDiagnostics{
			"0": {Partition: "0", EarliestOffset: 0, LatestOffset: 10, Health: "degraded"},
		},
	}
	if err := leaseRepo.RecordDiagnostics(context.Background(), *claim, diagnostics); err != nil {
		t.Fatalf("RecordDiagnostics() error = %v", err)
	}
	syncRepo := NewSyncStateRepository(db)
	partitionState, err := syncRepo.ClaimContinuousPartition(
		context.Background(), task.ID, "addp://engine/30/path/orders.events?type=topic", "0",
		"kafka_offset", "v1", "worker-a", 1,
	)
	if err != nil {
		t.Fatalf("ClaimContinuousPartition() error = %v", err)
	}
	position5 := models.JSONMap{
		"type": "kafka_offset", "version": "v1", "partition": "0",
		"values": map[string]string{"next_offset": "5"},
	}
	if err := syncRepo.CommitContinuousPosition(context.Background(), partitionState.ID, task.ID, 0, 1, "worker-a", position5, execution.ExecutionID); err != nil {
		t.Fatalf("CommitContinuousPosition() error = %v", err)
	}
	if err := leaseRepo.Renew(context.Background(), task.ID, "worker-a", 1, now.Add(time.Second), 30*time.Second); err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
	var renewedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&renewedExecution).Error; err != nil {
		t.Fatalf("load renewed execution: %v", err)
	}
	renewedMetadata, ok := renewedExecution.Metadata["continuous"].(map[string]interface{})
	if !ok || renewedMetadata["owner_instance_id"] != "worker-a" || renewedMetadata["lease_until"] == nil {
		t.Fatalf("renewed continuous metadata = %#v", renewedExecution.Metadata)
	}
	if err := taskRepo.SetContinuousDesiredState(context.Background(), task.ID, task.TenantID, models.TaskDesiredStatePaused, "paused"); err != nil {
		t.Fatalf("pause continuous task: %v", err)
	}
	if err := leaseRepo.RecordDiagnostics(context.Background(), *claim, diagnostics); !errors.Is(err, ErrRuntimeLeaseLost) {
		t.Fatalf("diagnostics after pause error = %v, want ErrRuntimeLeaseLost", err)
	}
	if err := leaseRepo.Renew(context.Background(), task.ID, "worker-a", 1, now.Add(2*time.Second), 30*time.Second); !errors.Is(err, ErrRuntimeLeaseLost) {
		t.Fatalf("renew after pause error = %v, want ErrRuntimeLeaseLost", err)
	}
	if err := syncRepo.CommitContinuousPosition(context.Background(), partitionState.ID, task.ID, 1, 1, "worker-a", position5, execution.ExecutionID); !errors.Is(err, ErrSyncStateFenced) {
		t.Fatalf("stale continuous position commit error = %v, want ErrSyncStateFenced", err)
	}
	if err := leaseRepo.Finish(context.Background(), *claim, commonExecution.ExecutionStatusCancelled, "paused", "", now.Add(2*time.Second)); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}

	second := claimTestExecution(task, uuid.NewString())
	if _, err := taskRepo.StartContinuousExecution(context.Background(), task.ID, task.TenantID, &second); err != nil {
		t.Fatalf("resume continuous execution: %v", err)
	}
	secondClaim, err := leaseRepo.ClaimNext(context.Background(), "worker-b", now.Add(3*time.Second), 30*time.Second)
	if err != nil || secondClaim == nil {
		t.Fatalf("second ClaimNext() claim=%#v error=%v", secondClaim, err)
	}
	if secondClaim.Lease.FencingToken != 2 {
		t.Fatalf("second fencing token = %d, want 2", secondClaim.Lease.FencingToken)
	}
	if err := leaseRepo.RecordDiagnostics(context.Background(), *claim, diagnostics); !errors.Is(err, ErrRuntimeLeaseLost) {
		t.Fatalf("stale diagnostics error = %v, want ErrRuntimeLeaseLost", err)
	}
	var diagnosticsExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&diagnosticsExecution).Error; err != nil {
		t.Fatalf("load diagnostics execution: %v", err)
	}
	diagnosticsMetadata, _ := diagnosticsExecution.Metadata["continuous"].(map[string]interface{})
	storedDiagnostics, _ := diagnosticsMetadata["diagnostics"].(map[string]interface{})
	if storedDiagnostics["health"] != "degraded" {
		t.Fatalf("stored diagnostics = %#v", diagnosticsMetadata["diagnostics"])
	}
	partitionState, err = syncRepo.ClaimContinuousPartition(
		context.Background(), task.ID, "addp://engine/30/path/orders.events?type=topic", "0",
		"kafka_offset", "v1", "worker-b", 2,
	)
	if err != nil {
		t.Fatalf("second ClaimContinuousPartition() error = %v", err)
	}
	if partitionState.FencingToken != 2 || partitionState.StateVersion != 1 {
		t.Fatalf("reclaimed partition state = %#v", partitionState)
	}
	if err := leaseRepo.Renew(context.Background(), task.ID, "worker-a", 1, now.Add(4*time.Second), 30*time.Second); !errors.Is(err, ErrRuntimeLeaseLost) {
		t.Fatalf("stale worker renew error = %v, want ErrRuntimeLeaseLost", err)
	}
	if err := leaseRepo.Finish(context.Background(), *secondClaim, commonExecution.ExecutionStatusFailed, "schema_change_blocked", "schema changed", now.Add(5*time.Second)); err != nil {
		t.Fatalf("finish schema-blocked execution: %v", err)
	}
	var blockedTask models.TransferTask
	if err := db.First(&blockedTask, task.ID).Error; err != nil {
		t.Fatalf("load schema-blocked task: %v", err)
	}
	if blockedTask.Status != models.TaskStatusBlocked || blockedTask.DesiredState != models.TaskDesiredStateRunning {
		t.Fatalf("schema-blocked task status=%q desired=%q", blockedTask.Status, blockedTask.DesiredState)
	}
	blockedExecution := claimTestExecution(task, uuid.NewString())
	if _, err := taskRepo.StartContinuousExecution(context.Background(), task.ID, task.TenantID, &blockedExecution); !errors.Is(err, ErrContinuousTaskBlocked) {
		t.Fatalf("start blocked task error = %v, want ErrContinuousTaskBlocked", err)
	}
	if err := taskRepo.SetContinuousDesiredState(context.Background(), task.ID, task.TenantID, models.TaskDesiredStateStopped, "stopped"); err != nil {
		t.Fatalf("stop blocked task: %v", err)
	}
	if err := db.First(&blockedTask, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if blockedTask.Status != models.TaskStatusIdle || blockedTask.DesiredState != models.TaskDesiredStateStopped {
		t.Fatalf("stopped blocked task status=%q desired=%q", blockedTask.Status, blockedTask.DesiredState)
	}
}

func TestIntegrationPostgresContinuousAutomaticRecoveryCreatesNewExecution(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	connInfo := testpg.ConnInfoFromEnv(t)
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN failed: %v", err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL failed: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatalf("create transfer schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.TransferTask{}, &models.RuntimeLease{}); err != nil {
		t.Fatalf("migrate continuous models: %v", err)
	}

	task := models.TransferTask{
		TenantID: 999999, Name: fmt.Sprintf("continuous-recovery-%d", time.Now().UnixNano()), TaskType: commonExecution.TaskTypeSync,
		Config: models.JSONMap{"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}}}, BatchSize: 100,
		Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Where("task_id = ?", task.ID).Delete(&models.RuntimeLease{}).Error
		_ = db.Where("module = ? AND source_task_id = ?", commonExecution.ModuleTransfer, fmt.Sprint(task.ID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Delete(&models.TransferTask{}, task.ID).Error
	})

	taskRepo := NewTaskRepository(db)
	leaseRepo := NewRuntimeLeaseRepository(db, testContinuousRecoveryPolicy())
	now := time.Now()
	firstExecution := claimTestExecution(task, uuid.NewString())
	firstExecution.ExecutionConfig = task.Config
	if _, err := taskRepo.StartContinuousExecution(context.Background(), task.ID, task.TenantID, &firstExecution); err != nil {
		t.Fatalf("start continuous execution: %v", err)
	}
	firstClaim, err := leaseRepo.ClaimNext(context.Background(), "worker-a", now, 30*time.Second)
	if err != nil || firstClaim == nil {
		t.Fatalf("first ClaimNext() claim=%#v error=%v", firstClaim, err)
	}
	if err := leaseRepo.Finish(context.Background(), *firstClaim, commonExecution.ExecutionStatusCancelled, "worker_shutdown", "", now.Add(time.Second)); err != nil {
		t.Fatalf("finish worker shutdown execution: %v", err)
	}

	type claimResult struct {
		claim *RuntimeLeaseClaim
		err   error
	}
	recoveryStart := make(chan struct{})
	recoveryResults := make(chan claimResult, 2)
	for _, owner := range []string{"worker-b", "worker-c"} {
		go func(owner string) {
			<-recoveryStart
			claim, claimErr := leaseRepo.ClaimNext(context.Background(), owner, now.Add(2*time.Second), 30*time.Second)
			recoveryResults <- claimResult{claim: claim, err: claimErr}
		}(owner)
	}
	close(recoveryStart)
	var secondClaim *RuntimeLeaseClaim
	emptyClaims := 0
	for range 2 {
		result := <-recoveryResults
		if result.err != nil {
			t.Fatalf("worker-shutdown recovery ClaimNext() error=%v", result.err)
		}
		if result.claim == nil {
			emptyClaims++
			continue
		}
		if secondClaim != nil {
			t.Fatalf("worker-shutdown recovery created multiple claims: %s and %s", secondClaim.Execution.ExecutionID, result.claim.Execution.ExecutionID)
		}
		secondClaim = result.claim
	}
	if secondClaim == nil || emptyClaims != 1 {
		t.Fatalf("worker-shutdown recovery claim=%#v empty_claims=%d, want one claim and one empty result", secondClaim, emptyClaims)
	}
	if secondClaim.Execution.ExecutionID == firstClaim.Execution.ExecutionID {
		t.Fatalf("worker-shutdown recovery reused execution %s", secondClaim.Execution.ExecutionID)
	}
	if secondClaim.Execution.TriggerType != firstClaim.Execution.TriggerType || secondClaim.Execution.Metadata["recovery_reason"] != "worker_shutdown" {
		t.Fatalf("worker-shutdown recovery execution = %#v", secondClaim.Execution)
	}
	if secondClaim.Lease.FencingToken != 2 {
		t.Fatalf("worker-shutdown recovery fencing token = %d, want 2", secondClaim.Lease.FencingToken)
	}

	leaseExpiryDetection, err := leaseRepo.ClaimNext(context.Background(), "worker-d", now.Add(33*time.Second), 30*time.Second)
	if err != nil || leaseExpiryDetection != nil {
		t.Fatalf("lease-expiry detection ClaimNext() claim=%#v error=%v, want backoff", leaseExpiryDetection, err)
	}
	thirdClaim, err := leaseRepo.ClaimNext(context.Background(), "worker-d", now.Add(34*time.Second), 30*time.Second)
	if err != nil || thirdClaim == nil {
		t.Fatalf("lease-expiry recovery after backoff ClaimNext() claim=%#v error=%v", thirdClaim, err)
	}
	if thirdClaim.Execution.ExecutionID == secondClaim.Execution.ExecutionID {
		t.Fatalf("lease-expiry recovery reused execution %s", thirdClaim.Execution.ExecutionID)
	}
	if thirdClaim.Execution.TriggerType != secondClaim.Execution.TriggerType || thirdClaim.Execution.Metadata["recovery_reason"] != "lease_expired" {
		t.Fatalf("lease-expiry recovery execution = %#v", thirdClaim.Execution)
	}
	if thirdClaim.Lease.FencingToken != 3 {
		t.Fatalf("lease-expiry recovery fencing token = %d, want 3", thirdClaim.Lease.FencingToken)
	}
	var expiredExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", secondClaim.Execution.ExecutionID).First(&expiredExecution).Error; err != nil {
		t.Fatalf("load expired execution: %v", err)
	}
	if expiredExecution.Status != commonExecution.ExecutionStatusFailed || expiredExecution.Metadata["stop_reason"] != "lease_expired" {
		t.Fatalf("expired execution status=%q metadata=%#v", expiredExecution.Status, expiredExecution.Metadata)
	}
}

func TestIntegrationPostgresContinuousRecoveryBackoffAndCircuitBreaker(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	connInfo := testpg.ConnInfoFromEnv(t)
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN failed: %v", err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL failed: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatalf("create transfer schema: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure execution store: %v", err)
	}
	if err := db.AutoMigrate(&models.TransferTask{}, &models.RuntimeLease{}); err != nil {
		t.Fatalf("migrate continuous models: %v", err)
	}

	task := models.TransferTask{
		TenantID: 999999, Name: fmt.Sprintf("continuous-circuit-%d", time.Now().UnixNano()), TaskType: commonExecution.TaskTypeSync,
		Config: models.JSONMap{"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}}}, BatchSize: 100,
		Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateStopped,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Where("task_id = ?", task.ID).Delete(&models.RuntimeLease{}).Error
		_ = db.Where("module = ? AND source_task_id = ?", commonExecution.ModuleTransfer, fmt.Sprint(task.ID)).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Unscoped().Delete(&models.TransferTask{}, task.ID).Error
	})

	taskRepo := NewTaskRepository(db)
	leaseRepo := NewRuntimeLeaseRepository(db, testContinuousRecoveryPolicy())
	now := time.Now()
	initial := claimTestExecution(task, uuid.NewString())
	initial.ExecutionConfig = task.Config
	if _, err := taskRepo.StartContinuousExecution(context.Background(), task.ID, task.TenantID, &initial); err != nil {
		t.Fatalf("start continuous execution: %v", err)
	}
	first, err := leaseRepo.ClaimNext(context.Background(), "worker-a", now, 30*time.Second)
	if err != nil || first == nil {
		t.Fatalf("first ClaimNext() claim=%#v error=%v", first, err)
	}
	if err := leaseRepo.Finish(context.Background(), *first, commonExecution.ExecutionStatusFailed, "", "source failed", now.Add(time.Second)); err != nil {
		t.Fatalf("finish first failure: %v", err)
	}
	assertPendingRecoveryMetadata(t, db, task.ID, 1, recoveryCircuitClosed, now.Add(2*time.Second))
	if claim, err := leaseRepo.ClaimNext(context.Background(), "worker-b", now.Add(1500*time.Millisecond), 30*time.Second); err != nil || claim != nil {
		t.Fatalf("claim before first backoff claim=%#v error=%v", claim, err)
	}
	second, err := leaseRepo.ClaimNext(context.Background(), "worker-b", now.Add(2*time.Second), 30*time.Second)
	if err != nil || second == nil {
		t.Fatalf("second ClaimNext() claim=%#v error=%v", second, err)
	}
	if err := leaseRepo.Finish(context.Background(), *second, commonExecution.ExecutionStatusFailed, "", "target failed", now.Add(3*time.Second)); err != nil {
		t.Fatalf("finish second failure: %v", err)
	}
	assertPendingRecoveryMetadata(t, db, task.ID, 2, recoveryCircuitClosed, now.Add(5*time.Second))
	if claim, err := leaseRepo.ClaimNext(context.Background(), "worker-c", now.Add(4*time.Second), 30*time.Second); err != nil || claim != nil {
		t.Fatalf("claim before second backoff claim=%#v error=%v", claim, err)
	}
	third, err := leaseRepo.ClaimNext(context.Background(), "worker-c", now.Add(5*time.Second), 30*time.Second)
	if err != nil || third == nil {
		t.Fatalf("third ClaimNext() claim=%#v error=%v", third, err)
	}
	if err := leaseRepo.Finish(context.Background(), *third, commonExecution.ExecutionStatusFailed, "", "apply failed", now.Add(6*time.Second)); err != nil {
		t.Fatalf("finish third failure: %v", err)
	}
	assertPendingRecoveryMetadata(t, db, task.ID, 3, recoveryCircuitOpen, now.Add(16*time.Second))
	if claim, err := leaseRepo.ClaimNext(context.Background(), "worker-d", now.Add(15*time.Second), 30*time.Second); err != nil || claim != nil {
		t.Fatalf("claim while circuit open claim=%#v error=%v", claim, err)
	}
	halfOpen, err := leaseRepo.ClaimNext(context.Background(), "worker-d", now.Add(16*time.Second), 30*time.Second)
	if err != nil || halfOpen == nil {
		t.Fatalf("half-open ClaimNext() claim=%#v error=%v", halfOpen, err)
	}
	if halfOpen.Execution.Metadata["recovery_circuit_state"] != recoveryCircuitHalfOpen {
		t.Fatalf("half-open execution metadata=%#v", halfOpen.Execution.Metadata)
	}
	if err := leaseRepo.Finish(context.Background(), *halfOpen, commonExecution.ExecutionStatusFailed, "", "half-open failed", now.Add(17*time.Second)); err != nil {
		t.Fatalf("finish half-open failure: %v", err)
	}
	assertPendingRecoveryMetadata(t, db, task.ID, 3, recoveryCircuitOpen, now.Add(27*time.Second))

	probe, err := leaseRepo.ClaimNext(context.Background(), "worker-e", now.Add(27*time.Second), 30*time.Second)
	if err != nil || probe == nil {
		t.Fatalf("second half-open ClaimNext() claim=%#v error=%v", probe, err)
	}
	if err := leaseRepo.RecordProgress(context.Background(), *probe, ContinuousProgress{
		RecordsRead: 1, RecordsWritten: 1, Partition: "0",
		Position:    models.JSONMap{"type": "kafka_offset", "version": "v1", "values": map[string]string{"next_offset": "1"}},
		CommittedAt: now.Add(27500 * time.Millisecond),
	}); err != nil {
		t.Fatalf("record successful progress: %v", err)
	}
	var progressedProbe commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", probe.Execution.ExecutionID).First(&progressedProbe).Error; err != nil {
		t.Fatalf("load progressed half-open probe: %v", err)
	}
	if recoveryMetadataInt(progressedProbe.Metadata["recovery_attempt"]) != 3 ||
		recoveryMetadataInt(progressedProbe.Metadata["recovery_consecutive_failures"]) != 0 ||
		progressedProbe.Metadata["recovery_backoff_seconds"] != float64(10) ||
		progressedProbe.Metadata["recovery_circuit_state"] != recoveryCircuitClosed {
		t.Fatalf("progressed probe recovery metadata=%#v", progressedProbe.Metadata)
	}
	if err := leaseRepo.Finish(context.Background(), *probe, commonExecution.ExecutionStatusFailed, "", "failure after progress", now.Add(28*time.Second)); err != nil {
		t.Fatalf("finish failure after progress: %v", err)
	}
	assertPendingRecoveryMetadata(t, db, task.ID, 1, recoveryCircuitClosed, now.Add(29*time.Second))
}

func assertPendingRecoveryMetadata(t *testing.T, db *gorm.DB, taskID uint, wantAttempt int, wantCircuit string, wantNotBefore time.Time) {
	t.Helper()
	var execution commonExecution.TaskExecution
	if err := db.Where("module = ? AND source_task_id = ? AND status = ?", commonExecution.ModuleTransfer, fmt.Sprint(taskID), commonExecution.ExecutionStatusPending).
		Order("created_at DESC, id DESC").First(&execution).Error; err != nil {
		t.Fatalf("load pending recovery execution: %v", err)
	}
	if recoveryMetadataInt(execution.Metadata["recovery_attempt"]) != wantAttempt ||
		recoveryMetadataInt(execution.Metadata["recovery_consecutive_failures"]) != wantAttempt ||
		execution.Metadata["recovery_circuit_state"] != wantCircuit {
		t.Fatalf("pending recovery metadata=%#v, want attempt=%d circuit=%s", execution.Metadata, wantAttempt, wantCircuit)
	}
	if execution.StartedAt != nil {
		t.Fatalf("pending recovery started_at=%v, want nil", execution.StartedAt)
	}
	storedNotBefore, ok := execution.Metadata["recovery_not_before"].(string)
	if !ok {
		t.Fatalf("pending recovery_not_before=%#v", execution.Metadata["recovery_not_before"])
	}
	parsed, err := time.Parse(time.RFC3339Nano, storedNotBefore)
	if err != nil || !parsed.Equal(wantNotBefore) {
		t.Fatalf("pending recovery_not_before=%q parsed=%v error=%v, want %v", storedNotBefore, parsed, err, wantNotBefore)
	}
}

func testContinuousRecoveryPolicy() ContinuousRecoveryPolicy {
	return ContinuousRecoveryPolicy{
		InitialBackoff: time.Second, MaxBackoff: 4 * time.Second, MaxFailures: 3,
		CircuitOpenTime: 10 * time.Second, StabilityWindow: 30 * time.Second,
	}
}
