package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestIntegrationPostgresContinuousExecutionEvents(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres with the verified test PostgreSQL mapping")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.TransferTask{}, &models.RuntimeLease{}, &models.RuntimeSession{}, &models.CleanupOperation{}, &models.SchemaChangeRequest{}, &models.CaptureResource{}); err != nil {
		t.Fatal(err)
	}
	repo := NewRuntimeLeaseRepository(db, testContinuousRecoveryPolicy())

	t.Run("record_failure_code_is_persisted_and_safely_projected", func(t *testing.T) {
		for _, code := range []string{"invalid_json_object", "multiple_json_values", "unknown_source_field", "missing_source_key", "missing_required_field", "incompatible_field_type", "null_target_key"} {
			t.Run(code, func(t *testing.T) {
				claim := continuousEventFixture(t, db, true)
				if err := db.Model(&claim.Task).Update("desired_state", models.TaskDesiredStateStopped).Error; err != nil {
					t.Fatal(err)
				}
				if err := repo.Finish(t.Context(), claim, commonExecution.ExecutionStatusFailed, "", code, "record validation failed"); err != nil {
					t.Fatal(err)
				}
				var stored commonExecution.TaskExecution
				if err := db.First(&stored, claim.Execution.ID).Error; err != nil {
					t.Fatal(err)
				}
				if stored.ErrorDetails["code"] != code || stored.Status != commonExecution.ExecutionStatusFailed {
					t.Fatalf("stored failure=%#v status=%s", stored.ErrorDetails, stored.Status)
				}
				projected := commonExecution.Observe(&stored).ErrorDetails
				if projected["code"] != code || projected["message"] != nil || len(projected) != 2 {
					t.Fatalf("unsafe failure projection=%#v", projected)
				}
				events := continuousStoredEvents(t, db, claim.Execution.ExecutionID)
				if len(events) != 1 || events[0].Kind != "failed" || len(events[0].Counters) != 0 || events[0].StepID != "" {
					t.Fatalf("record failure events=%#v", events)
				}
			})
		}
	})

	t.Run("claim_progress_pause_terminal_and_closed_payload", func(t *testing.T) {
		claim := continuousEventFixture(t, db, false)
		started, err := repo.ClaimNext(t.Context(), claim.Lease.OwnerInstanceID, time.Minute)
		if err != nil || started == nil || started.Execution.ExecutionID != claim.Execution.ExecutionID || started.Execution.Attempt != 1 {
			t.Fatalf("claim=%#v err=%v", started, err)
		}
		claim = *started
		progress := continuousEventProgress(12)
		if err := repo.RecordProgress(t.Context(), claim, progress); err != nil {
			t.Fatal(err)
		}
		if err := repo.Renew(t.Context(), claim.Task.ID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken, time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := NewTaskRepository(db).SetContinuousDesiredState(t.Context(), claim.Task.ID, claim.Task.TenantID, models.TaskDesiredStatePaused, "paused"); err != nil {
			t.Fatal(err)
		}
		if err := repo.RecordProgress(t.Context(), claim, progress); !errors.Is(err, ErrRuntimeLeaseLost) {
			t.Fatalf("paused progress=%v", err)
		}
		if err := repo.Finish(t.Context(), claim, commonExecution.ExecutionStatusCancelled, "paused", "", "private-runtime-response"); err != nil {
			t.Fatal(err)
		}
		if err := repo.Finish(t.Context(), claim, commonExecution.ExecutionStatusCancelled, "paused", "", ""); !errors.Is(err, ErrRuntimeLeaseLost) {
			t.Fatalf("duplicate terminal=%v", err)
		}
		events := continuousStoredEvents(t, db, claim.Execution.ExecutionID)
		if len(events) != 3 || events[0].Kind != "started" || events[1].Kind != "progress" || events[2].Kind != "cancelled" {
			t.Fatalf("events=%#v", events)
		}
		for _, event := range events {
			if event.TenantID != int(claim.Task.TenantID) || event.Module != "transfer" || event.TaskType != "sync" || event.Attempt != 1 || event.StepID != "" {
				t.Fatalf("identity=%#v", event)
			}
			if len(event.Counters) > 2 {
				t.Fatalf("unclosed counters=%#v", event)
			}
			for key := range event.Counters {
				if key != "records_read" && key != "records_written" {
					t.Fatalf("private field=%s", key)
				}
			}
			if strings.Contains(fmt.Sprint(event), "private-") {
				t.Fatalf("private event payload=%#v", event)
			}
		}
		var stored commonExecution.TaskExecution
		if err := db.First(&stored, claim.Execution.ID).Error; err != nil || stored.Metadata["continuous"] == nil || stored.RecordsWritten == nil || *stored.RecordsWritten != 12 {
			t.Fatalf("overview=%#v err=%v", stored, err)
		}
	})

	t.Run("reject_mismatched_claim_and_expired_worker", func(t *testing.T) {
		claim := continuousEventFixture(t, db, true)
		other := continuousEventFixture(t, db, true)
		cases := []func(*RuntimeLeaseClaim){
			func(c *RuntimeLeaseClaim) { c.Lease.OwnerInstanceID = "other-owner" },
			func(c *RuntimeLeaseClaim) { c.Lease.FencingToken++ },
			func(c *RuntimeLeaseClaim) { c.Task.TenantID++ },
			func(c *RuntimeLeaseClaim) {
				c.Execution.ExecutionID = other.Execution.ExecutionID
				c.Lease.ExecutionID = other.Execution.ExecutionID
			},
			func(c *RuntimeLeaseClaim) { c.Lease.ExecutionID = other.Execution.ExecutionID },
		}
		for i, mutate := range cases {
			bad := claim
			mutate(&bad)
			if err := repo.RecordProgress(t.Context(), bad, continuousEventProgress(9)); !errors.Is(err, ErrRuntimeLeaseLost) {
				t.Fatalf("progress case %d=%v", i, err)
			}
			if err := repo.Finish(t.Context(), bad, "success", "", "", ""); !errors.Is(err, ErrRuntimeLeaseLost) {
				t.Fatalf("finish case %d=%v", i, err)
			}
		}
		for _, field := range []string{"module", "task_type", "execution_boundary", "tenant_id"} {
			value := interface{}("foreign")
			original := interface{}(map[string]string{"module": "transfer", "task_type": "sync", "execution_boundary": "continuous"}[field])
			if field == "tenant_id" {
				value, original = claim.Task.TenantID+1, claim.Task.TenantID
			}
			if err := db.Model(&claim.Execution).Update(field, value).Error; err != nil {
				t.Fatal(err)
			}
			if err := repo.RecordProgress(t.Context(), claim, continuousEventProgress(9)); !errors.Is(err, ErrRuntimeLeaseLost) {
				t.Fatalf("foreign %s=%v", field, err)
			}
			if err := db.Model(&claim.Execution).Update(field, original).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Model(&claim.Lease).Update("lease_until", time.Now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		for name, err := range map[string]error{
			"progress": repo.RecordProgress(t.Context(), claim, continuousEventProgress(9)),
			"finish":   repo.Finish(t.Context(), claim, "success", "", "", ""),
			"renew":    repo.Renew(t.Context(), claim.Task.ID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken, time.Minute),
		} {
			if !errors.Is(err, ErrRuntimeLeaseLost) {
				t.Fatalf("expired %s=%v", name, err)
			}
		}
		if events := continuousStoredEvents(t, db, claim.Execution.ExecutionID); len(events) != 0 {
			t.Fatalf("rejected writes produced events=%#v", events)
		}
	})

	t.Run("event_storage_failure_rolls_back_claim_progress_and_finish", func(t *testing.T) {
		active := continuousEventFixture(t, db, true)
		pending := continuousEventFixture(t, db, false)
		const callback = "test:reject-continuous-event"
		if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*commonExecution.Event); ok {
				tx.AddError(errors.New("event storage unavailable"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Callback().Create().Remove(callback) })
		if err := repo.RecordProgress(t.Context(), active, continuousEventProgress(3)); err == nil {
			t.Fatal("progress ignored event failure")
		}
		if err := repo.Finish(t.Context(), active, "success", "", "", ""); err == nil {
			t.Fatal("finish ignored event failure")
		}
		if _, err := repo.ClaimNext(t.Context(), pending.Lease.OwnerInstanceID, time.Minute); err == nil {
			t.Fatal("claim ignored event failure")
		}
		var stored commonExecution.TaskExecution
		if err := db.First(&stored, active.Execution.ID).Error; err != nil || stored.Status != "running" || stored.RecordsWritten != nil {
			t.Fatalf("active rollback=%#v err=%v", stored, err)
		}
		var lease models.RuntimeLease
		if err := db.First(&lease, active.Lease.ID).Error; err != nil || lease.OwnerInstanceID != active.Lease.OwnerInstanceID {
			t.Fatalf("lease rollback=%#v err=%v", lease, err)
		}
		stored = commonExecution.TaskExecution{}
		if err := db.First(&stored, pending.Execution.ID).Error; err != nil || stored.Status != "pending" || stored.Attempt != 0 {
			t.Fatalf("pending rollback=%#v err=%v", stored, err)
		}
		if events := continuousStoredEvents(t, db, active.Execution.ExecutionID); len(events) != 0 {
			t.Fatalf("rollback events=%#v", events)
		}
	})

	t.Run("daily_budget_serializes_and_does_not_block_progress_or_terminal", func(t *testing.T) {
		claim := continuousEventFixture(t, db, true)
		seed := make([]commonExecution.Event, commonExecution.MaxDailyAttemptEvents-1)
		for i := range seed {
			seed[i] = commonExecution.Event{ExecutionID: claim.Execution.ExecutionID, TenantID: int(claim.Task.TenantID), Module: "transfer", TaskType: "sync", Attempt: 1, OccurredAt: time.Now().UTC(), Kind: "progress", Counters: commonModels.JSONMap{}}
		}
		if err := db.CreateInBatches(seed, 100).Error; err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		outcomes := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				outcomes <- repo.RecordProgress(t.Context(), claim, continuousEventProgress(5))
			}()
		}
		wg.Wait()
		close(outcomes)
		for err := range outcomes {
			if err != nil {
				t.Fatal(err)
			}
		}
		events := continuousStoredEvents(t, db, claim.Execution.ExecutionID)
		if len(events) != commonExecution.MaxDailyAttemptEvents || events[len(events)-1].Kind != "truncated" {
			t.Fatalf("daily events=%d last=%#v", len(events), events[len(events)-1])
		}
		if err := db.Model(&claim.Lease).Update("lease_until", time.Now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.RecordProgress(t.Context(), claim, continuousEventProgress(6)); !errors.Is(err, ErrRuntimeLeaseLost) {
			t.Fatalf("full budget bypassed fencing=%v", err)
		}
		if err := db.Model(&claim.Lease).Update("lease_until", time.Now().Add(time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
		terminal := continuousEventFixture(t, db, true)
		full := make([]commonExecution.Event, commonExecution.MaxDailyAttemptEvents)
		for i := range full {
			full[i] = commonExecution.Event{ExecutionID: terminal.Execution.ExecutionID, TenantID: int(terminal.Task.TenantID), Module: "transfer", TaskType: "sync", Attempt: 1, OccurredAt: time.Now().UTC(), Kind: "progress", Counters: commonModels.JSONMap{}}
		}
		if err := db.CreateInBatches(full, 100).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.Finish(t.Context(), terminal, "success", "", "", ""); err != nil {
			t.Fatal(err)
		}
		var completed commonExecution.TaskExecution
		if err := db.First(&completed, terminal.Execution.ID).Error; err != nil || completed.Status != "success" {
			t.Fatalf("quota terminal=%#v err=%v", completed, err)
		}
		if events := continuousStoredEvents(t, db, terminal.Execution.ExecutionID); len(events) != commonExecution.MaxDailyAttemptEvents {
			t.Fatalf("quota terminal events=%d", len(events))
		}
		// Move yesterday's owned facts without changing the write clock; a new UTC day has a fresh budget.
		if err := db.Model(&commonExecution.Event{}).Where("execution_id = ?", claim.Execution.ExecutionID).Update("occurred_at", time.Now().UTC().Add(-24*time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.RecordProgress(t.Context(), claim, continuousEventProgress(7)); err != nil {
			t.Fatal(err)
		}
		if err := repo.Finish(t.Context(), claim, "success", "", "", ""); err != nil {
			t.Fatal(err)
		}
		events = continuousStoredEvents(t, db, claim.Execution.ExecutionID)
		if len(events) != commonExecution.MaxDailyAttemptEvents+2 || events[len(events)-1].Kind != "completed" {
			t.Fatalf("new-day terminal events=%d", len(events))
		}
	})

	t.Run("expiry_while_waiting_for_execution_lock_is_rejected", func(t *testing.T) {
		for _, operation := range []string{"progress", "finish", "renew"} {
			t.Run(operation, func(t *testing.T) {
				claim := continuousEventFixture(t, db, true)
				blocker := db.Begin()
				if blocker.Error != nil {
					t.Fatal(blocker.Error)
				}
				defer blocker.Rollback()
				var locked commonExecution.TaskExecution
				if err := blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, claim.Execution.ID).Error; err != nil {
					t.Fatal(err)
				}
				waiting := make(chan struct{}, 1)
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				const callback = "test:continuous-lock-wait"
				if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Table == "task_executions" && tx.Statement.Context == ctx {
						select {
						case waiting <- struct{}{}:
						default:
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
				defer db.Callback().Query().Remove(callback)
				result := make(chan error, 1)
				go func() {
					switch operation {
					case "progress":
						result <- repo.RecordProgress(ctx, claim, continuousEventProgress(99))
					case "finish":
						result <- repo.Finish(ctx, claim, "success", "", "", "")
					case "renew":
						result <- repo.Renew(ctx, claim.Task.ID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken, time.Minute)
					}
				}()
				select {
				case <-waiting:
				case <-ctx.Done():
					t.Fatal("writer did not reach the locked execution")
				}
				if err := db.Model(&claim.Lease).Update("lease_until", time.Now().Add(-time.Second)).Error; err != nil {
					t.Fatal(err)
				}
				if err := blocker.Commit().Error; err != nil {
					t.Fatal(err)
				}
				if err := <-result; !errors.Is(err, ErrRuntimeLeaseLost) {
					t.Fatalf("late %s=%v", operation, err)
				}
				if events := continuousStoredEvents(t, db, claim.Execution.ExecutionID); len(events) != 0 {
					t.Fatalf("late events=%#v", events)
				}
			})
		}
	})

	t.Run("pending_cancel_and_unowned_stop_preserve_overview", func(t *testing.T) {
		pending := continuousEventFixture(t, db, false)
		if err := NewTaskRepository(db).SetContinuousDesiredState(t.Context(), pending.Task.ID, pending.Task.TenantID, models.TaskDesiredStateStopped, "stopped"); err != nil {
			t.Fatal(err)
		}
		events := continuousStoredEvents(t, db, pending.Execution.ExecutionID)
		if len(events) != 0 {
			t.Fatalf("pending events=%#v", events)
		}
		active := continuousEventFixture(t, db, true)
		if err := db.Model(&active.Task).Update("desired_state", models.TaskDesiredStateStopped).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&active.Lease).Update("lease_until", time.Now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		if err := NewTaskRepository(db).FinalizeContinuousStop(t.Context(), active.Task.ID, active.Task.TenantID, "stopped", time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := NewTaskRepository(db).FinalizeContinuousStop(t.Context(), active.Task.ID, active.Task.TenantID, "stopped", time.Now()); err != nil {
			t.Fatal(err)
		}
		events = continuousStoredEvents(t, db, active.Execution.ExecutionID)
		if len(events) != 1 || events[0].Kind != "cancelled" {
			t.Fatalf("stop events=%#v", events)
		}
		if err := db.Model(&commonExecution.Event{}).Where("execution_id = ?", active.Execution.ExecutionID).Update("occurred_at", time.Now().Add(-31*24*time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := commonExecution.PruneEvents(t.Context(), db, time.Now(), 1000); err != nil {
			t.Fatal(err)
		}
		var overview commonExecution.TaskExecution
		if err := db.First(&overview, active.Execution.ID).Error; err != nil || overview.Status != "cancelled" {
			t.Fatalf("prune overview=%#v err=%v", overview, err)
		}
	})

	t.Run("paused_expired_owner_converges_without_recovery", func(t *testing.T) {
		claim := continuousEventFixture(t, db, true)
		if err := NewTaskRepository(db).SetContinuousDesiredState(t.Context(), claim.Task.ID, claim.Task.TenantID, models.TaskDesiredStatePaused, "paused"); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&claim.Lease).Update("lease_until", time.Now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if recovered, err := repo.ClaimNext(t.Context(), "supervisor", time.Minute); err != nil || recovered != nil {
				t.Fatalf("paused recovery=%#v err=%v", recovered, err)
			}
		}
		var task models.TransferTask
		if err := db.First(&task, claim.Task.ID).Error; err != nil || task.Status != models.TaskStatusIdle || task.DesiredState != models.TaskDesiredStatePaused {
			t.Fatalf("paused task=%#v err=%v", task, err)
		}
		events := continuousStoredEvents(t, db, claim.Execution.ExecutionID)
		if len(events) != 1 || events[0].Kind != "cancelled" {
			t.Fatalf("paused events=%#v", events)
		}
	})

	t.Run("physical_cleanup_keeps_execution_and_event", func(t *testing.T) {
		claim := continuousEventFixture(t, db, true)
		if err := db.AutoMigrate(&models.DeadLetter{}, &models.SyncState{}); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&claim.Task).Update("desired_state", models.TaskDesiredStateStopped).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&claim.Lease).Update("lease_until", time.Now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		// Expiry alone is insufficient; the original invocation must attest
		// actual runner completion before private state can be removed.
		if _, err := NewTaskResourceRepository(db).DeleteTaskAndPrivateState(t.Context(), claim.Task.TenantID, claim.Task.ID, true, TaskDefinitionDeletePhysical, time.Now()); !errors.Is(err, ErrTaskDeletionRuntimeActive) {
			t.Fatalf("expired owner cleanup=%v", err)
		}
		if err := NewRuntimeLeaseRepository(db, testContinuousRecoveryPolicy()).AcknowledgeStopped(t.Context(), claim); err != nil {
			t.Fatal(err)
		}
		stats, err := NewTaskResourceRepository(db).DeleteTaskAndPrivateState(t.Context(), claim.Task.TenantID, claim.Task.ID, true, TaskDefinitionDeletePhysical, time.Now())
		if err != nil || stats.CancelledExecutions != 1 || stats.RuntimeLeases != 1 {
			t.Fatalf("cleanup=%#v err=%v", stats, err)
		}
		var item commonExecution.TaskExecution
		if err := db.First(&item, claim.Execution.ID).Error; err != nil || item.Status != "cancelled" {
			t.Fatalf("cleanup overview=%#v err=%v", item, err)
		}
		events := continuousStoredEvents(t, db, item.ExecutionID)
		if len(events) != 1 || events[0].Kind != "cancelled" {
			t.Fatalf("cleanup events=%#v", events)
		}
	})
}

func continuousEventFixture(t *testing.T, db *gorm.DB, active bool) RuntimeLeaseClaim {
	t.Helper()
	task := models.TransferTask{TenantID: 999998, Name: "continuous-events-" + uuid.NewString(), TaskType: "sync", Config: models.JSONMap{"runtime": map[string]interface{}{"boundary": "continuous"}}, Status: models.TaskStatusIdle, DesiredState: models.TaskDesiredStateRunning}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	item := claimTestExecution(task, uuid.NewString())
	item.Metadata = commonModels.JSONMap{"private_context": "private-runtime-response"}
	if active {
		now := time.Now()
		item.Status = "running"
		item.StartedAt = &now
		item.Attempt = 1
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&task).Update("last_execution_id", item.ExecutionID).Error; err != nil {
		t.Fatal(err)
	}
	lease := models.RuntimeLease{TaskID: task.ID, ExecutionID: item.ExecutionID, OwnerInstanceID: "private-worker-" + uuid.NewString(), FencingToken: 1, ClaimedAt: time.Now(), HeartbeatAt: time.Now(), LeaseUntil: time.Now().Add(time.Minute)}
	if active {
		if err := db.Create(&lease).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.RuntimeSession{TaskID: task.ID, TenantID: task.TenantID, ExecutionID: item.ExecutionID, OwnerInstanceID: lease.OwnerInstanceID, FencingToken: lease.FencingToken, StartedAt: lease.ClaimedAt}).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanupTaskExecutionEvents(t, db, task.ID)
		for _, model := range []interface{}{&models.RuntimeLease{}, &models.RuntimeSession{}, &models.CleanupOperation{}, &models.SchemaChangeRequest{}} {
			if err := db.Where("task_id = ?", task.ID).Delete(model).Error; err != nil {
				t.Error(err)
			}
		}
		if err := db.Where("module = ? AND source_task_id = ?", "transfer", fmt.Sprint(task.ID)).Delete(&commonExecution.TaskExecution{}).Error; err != nil {
			t.Error(err)
		}
		if err := db.Unscoped().Delete(&models.TransferTask{}, task.ID).Error; err != nil {
			t.Error(err)
		}
		for _, model := range []interface{}{&models.RuntimeLease{}, &commonExecution.TaskExecution{}} {
			var count int64
			query := db.Model(model).Where("task_id = ?", task.ID)
			if _, ok := model.(*commonExecution.TaskExecution); ok {
				query = db.Model(model).Where("module = ? AND source_task_id = ?", "transfer", fmt.Sprint(task.ID))
			}
			if err := query.Count(&count).Error; err != nil || count != 0 {
				t.Errorf("fixture residual=%d err=%v", count, err)
			}
		}
	})
	return RuntimeLeaseClaim{Task: task, Execution: item, Lease: lease}
}

func continuousEventProgress(count int64) ContinuousProgress {
	return ContinuousProgress{RecordsRead: count, RecordsWritten: count, Partition: "private-partition", Position: models.JSONMap{"private-checkpoint": "private-business-value"}, CommittedAt: time.Now()}
}

func continuousStoredEvents(t *testing.T, db *gorm.DB, id string) []commonExecution.Event {
	t.Helper()
	var events []commonExecution.Event
	if err := db.Where("execution_id = ?", id).Order("id ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	return events
}

func cleanupTaskExecutionEvents(t *testing.T, db *gorm.DB, taskID uint) {
	t.Helper()
	ids := db.Model(&commonExecution.TaskExecution{}).Select("execution_id").Where("module = ? AND source_task_id = ?", "transfer", fmt.Sprint(taskID))
	if err := db.Where("execution_id IN (?)", ids).Delete(&commonExecution.Event{}).Error; err != nil {
		t.Error(err)
	}
	var count int64
	if err := db.Model(&commonExecution.Event{}).Where("execution_id IN (?)", ids).Count(&count).Error; err != nil || count != 0 {
		t.Errorf("event residual=%d err=%v", count, err)
	}
}
