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
	if err := db.AutoMigrate(&models.TransferTask{}, &models.SyncState{}, &models.RuntimeLease{}); err != nil {
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
		Config: models.JSONMap{"runtime": map[string]interface{}{"boundary": "continuous"}}, BatchSize: 100,
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
	leaseRepo := NewRuntimeLeaseRepository(db)
	claim, err := leaseRepo.ClaimNext(context.Background(), "worker-a", now, 30*time.Second)
	if err != nil || claim == nil {
		t.Fatalf("ClaimNext() claim=%#v error=%v", claim, err)
	}
	if claim.Lease.FencingToken != 1 || claim.Execution.Status != commonExecution.ExecutionStatusRunning {
		t.Fatalf("first claim = %#v", claim)
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
	if err := taskRepo.SetContinuousDesiredState(context.Background(), task.ID, task.TenantID, models.TaskDesiredStatePaused, "paused"); err != nil {
		t.Fatalf("pause continuous task: %v", err)
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
}
