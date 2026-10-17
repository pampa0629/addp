package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonlogger "github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestQueryExecutionEvents(t *testing.T) {
	runExecutionEventCases(t, func(t *testing.T) *gorm.DB { return newQueryExecutionTestDB(t) })
}

// The same owner transaction assertions run against SQLite and PostgreSQL.
func runExecutionEventCases(t *testing.T, database func(*testing.T) *gorm.DB) {
	for _, taskType := range executionTaskTypes {
		t.Run(taskType, func(t *testing.T) { runTaskEventCases(t, database, taskType) })
	}
}

func runTaskEventCases(t *testing.T, database func(*testing.T) *gorm.DB, taskType string) {
	t.Helper()
	for _, status := range []string{"success", "failed", "cancelled", "timeout"} {
		t.Run(status, func(t *testing.T) {
			db := database(t)
			var trace bytes.Buffer
			db = db.Session(&gorm.Session{Logger: commonlogger.WithoutSQLValues(gormlogger.New(
				log.New(&trace, "", 0), gormlogger.Config{LogLevel: gormlogger.Info},
			))})
			repo, item, task := seedExecutionEvent(t, db, taskType)
			claimed, lease := claimExecutionEvent(t, repo, taskType)
			assertExecutionEvents(t, db, taskType, item.ExecutionID, "started")
			if err := repo.AttachAuthorizationWithLease(t.Context(), *lease, 73, time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if err := repo.Renew(t.Context(), *lease, time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			assertExecutionEvents(t, db, taskType, item.ExecutionID, "started")
			if err := repo.UpdateProgressWithLease(t.Context(), *lease, 30, "query-private-sentinel"); err != nil {
				t.Fatal(err)
			}
			fields := map[string]interface{}{"metadata": commonModels.JSONMap{"result": "query-private-sentinel"}}
			if err := repo.CompleteWithLease(t.Context(), claimed, *lease, status, time.Now(), fields); err != nil {
				t.Fatal(err)
			}
			var persisted commonExecution.TaskExecution
			if err := db.Where("execution_id = ?", item.ExecutionID).First(&persisted).Error; err != nil || persisted.Metadata["result"] != "query-private-sentinel" {
				t.Fatal("filtered SQL logging changed persisted execution metadata")
			}
			if strings.Contains(trace.String(), "query-private-sentinel") || strings.Contains(trace.String(), "protected_query_history") {
				t.Fatal("execution completion SQL trace exposed private metadata")
			}
			if !strings.Contains(trace.String(), "UPDATE") || !strings.Contains(trace.String(), "task_executions") {
				t.Fatal("execution completion SQL trace lost its operation")
			}
			terminal := status
			if status == "success" {
				terminal = "completed"
			}
			assertExecutionEvents(t, db, taskType, item.ExecutionID, "started", "progress", terminal)
			assertQueryEventState(t, db, item.ExecutionID, task.ID, status, 30)
			if err := repo.CompleteWithLease(t.Context(), claimed, *lease, status, time.Now(), nil); err == nil {
				t.Fatal("terminal execution accepted a second completion")
			}
			assertExecutionEvents(t, db, taskType, item.ExecutionID, "started", "progress", terminal)
		})
	}
	t.Run("lease_identity", func(t *testing.T) {
		db := database(t)
		repo, item, task := seedExecutionEvent(t, db, taskType)
		claimed, lease := claimExecutionEvent(t, repo, taskType)
		for _, mutate := range []func(*commonExecution.Lease){
			func(l *commonExecution.Lease) { l.TenantID++ },
			func(l *commonExecution.Lease) { l.Token = uuid.NewString() },
			func(l *commonExecution.Lease) { l.Owner = "foreign-owner" },
			func(l *commonExecution.Lease) { l.Attempt++ },
		} {
			wrong := *lease
			mutate(&wrong)
			if err := repo.UpdateProgressWithLease(t.Context(), wrong, 30, "private"); err == nil {
				t.Fatal("wrong lease wrote progress")
			}
			if err := repo.CompleteWithLease(t.Context(), claimed, wrong, "success", time.Now(), nil); err == nil {
				t.Fatal("wrong lease completed execution")
			}
		}
		wrongExecution := *claimed
		wrongExecution.Module = "meta"
		if err := repo.CompleteWithLease(t.Context(), &wrongExecution, *lease, "success", time.Now(), nil); err == nil {
			t.Fatal("completion accepted a foreign execution summary")
		}
		for _, progress := range []int{-1, 101} {
			if err := repo.UpdateProgressWithLease(t.Context(), *lease, progress, "private"); err == nil {
				t.Fatal("invalid progress was accepted")
			}
		}
		assertExecutionEvents(t, db, taskType, item.ExecutionID, "started")
		assertQueryEventState(t, db, item.ExecutionID, task.ID, "running", 0)
	})
	for _, operation := range []string{"claim", "progress", "complete", "recover"} {
		t.Run("event_failure_"+operation, func(t *testing.T) {
			db := database(t)
			repo, item, task := seedExecutionEvent(t, db, taskType)
			var claimed *commonExecution.TaskExecution
			var lease *commonExecution.Lease
			if operation != "claim" {
				claimed, lease = claimExecutionEvent(t, repo, taskType)
			}
			if operation == "recover" {
				if err := db.Model(item).Update("lease_expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
			}
			name := "test:query-event-failure-" + uuid.NewString()
			if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*commonExecution.Event); ok {
					tx.AddError(errors.New("event storage unavailable"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Callback().Create().Remove(name) })
			var err error
			switch operation {
			case "claim":
				_, _, err = repo.ClaimNext(
					taskType, t.Context(), "query-event-owner", time.Now(), time.Minute, nil)
			case "progress":
				err = repo.UpdateProgressWithLease(t.Context(), *lease, 30, "private")
			case "complete":
				err = repo.CompleteWithLease(t.Context(), claimed, *lease, "success", time.Now(), nil)
			case "recover":
				_, err = repo.RecoverExpired(t.Context(), time.Now(), 1)
			}
			if err == nil {
				t.Fatal("event failure did not fail the transaction")
			}
			if operation == "claim" {
				assertExecutionEvents(t, db, taskType, item.ExecutionID)
				var stored commonExecution.TaskExecution
				var storedTask models.DevTask
				if err := db.First(&stored, item.ID).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.First(&storedTask, task.ID).Error; err != nil {
					t.Fatal(err)
				}
				if stored.Status != "pending" || stored.Attempt != 0 || stored.LeaseToken != nil || storedTask.LastExecutionID != nil {
					t.Fatal("claim or task summary escaped rollback")
				}
			} else {
				assertExecutionEvents(t, db, taskType, item.ExecutionID, "started")
				assertQueryEventState(t, db, item.ExecutionID, task.ID, "running", 0)
			}
		})
	}
	t.Run("task_summary_failure", func(t *testing.T) {
		db := database(t)
		repo, item, task := seedExecutionEvent(t, db, taskType)
		claimed, lease := claimExecutionEvent(t, repo, taskType)
		if err := db.Model(task).Update("last_execution_id", uuid.NewString()).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.CompleteWithLease(t.Context(), claimed, *lease, "success", time.Now(), nil); err == nil {
			t.Fatal("summary failure did not fail completion")
		}
		assertExecutionEvents(t, db, taskType, item.ExecutionID, "started")
		var stored commonExecution.TaskExecution
		if err := db.First(&stored, item.ID).Error; err != nil || stored.Status != "running" || stored.LeaseToken == nil {
			t.Fatalf("completion escaped summary rollback: status=%s err=%v", stored.Status, err)
		}
	})
	t.Run("expired_recovery", func(t *testing.T) {
		db := database(t)
		repo, item, task := seedExecutionEvent(t, db, taskType)
		claimed, lease := claimExecutionEvent(t, repo, taskType)
		if err := db.Model(item).Update("lease_expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.UpdateProgressWithLease(t.Context(), *lease, 30, "private"); err == nil {
			t.Fatal("expired worker wrote progress")
		}
		if err := repo.CompleteWithLease(t.Context(), claimed, *lease, "success", time.Now(), nil); err == nil {
			t.Fatal("expired worker completed execution")
		}
		if count, err := repo.RecoverExpired(t.Context(), time.Now(), 1); err != nil || count != 1 {
			t.Fatalf("recover=%d err=%v", count, err)
		}
		assertExecutionEvents(t, db, taskType, item.ExecutionID, "started", "failed")
		assertQueryEventState(t, db, item.ExecutionID, task.ID, "failed", 0)
		if count, err := repo.RecoverExpired(t.Context(), time.Now(), 1); err != nil || count != 0 {
			t.Fatalf("duplicate recover=%d err=%v", count, err)
		}
		assertExecutionEvents(t, db, taskType, item.ExecutionID, "started", "failed")
	})
}

func seedExecutionEvent(t *testing.T, db *gorm.DB, taskType string) (*ExecutionRepository, *commonExecution.TaskExecution, *models.DevTask) {
	t.Helper()
	task := createQueryExecutionTestTask(t, db)
	if err := db.Model(task).Update("dev_type", taskType).Error; err != nil {
		t.Fatal(err)
	}
	principal, membership, version := int64(11), int64(12), int64(2)
	item := &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: uuid.NewString(), Module: "develop", TaskType: taskType, Source: "develop",
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), Status: "pending",
		ExecutionBoundary: "bounded", TriggerType: "manual", CreatedAt: time.Unix(1, 0), UpdatedAt: time.Now(),
		ExecutionConfig:  commonModels.JSONMap{"query": "query-private-sentinel"},
		ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version,
	}
	if err := db.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	return NewExecutionRepository(db), item, task
}

func claimExecutionEvent(t *testing.T, repo *ExecutionRepository, taskType string) (*commonExecution.TaskExecution, *commonExecution.Lease) {
	t.Helper()
	item, lease, err := repo.ClaimNext(
		taskType, t.Context(), "query-event-owner", time.Now(), time.Minute, nil)
	if err != nil || item == nil || lease == nil {
		t.Fatalf("claim item=%v lease=%v err=%v", item != nil, lease != nil, err)
	}
	return item, lease
}

func assertExecutionEvents(t *testing.T, db *gorm.DB, taskType, id string, kinds ...string) {
	t.Helper()
	var events []commonExecution.Event
	if err := db.Where("execution_id = ?", id).Order("id ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != len(kinds) {
		t.Fatalf("event count=%d want=%d", len(events), len(kinds))
	}
	for index, event := range events {
		if event.Kind != kinds[index] || event.ExecutionID != id || event.TenantID != 7 || event.Module != "develop" || event.TaskType != taskType || event.Attempt != 1 || event.StepID != "" {
			t.Fatalf("unexpected event identity/kind at %d", index)
		}
		if event.Kind == "progress" {
			progress, ok := event.Counters.GetInt("progress")
			if !ok || progress != 30 || len(event.Counters) != 1 {
				t.Fatal("progress event contains unexpected counters")
			}
		} else if len(event.Counters) != 0 {
			t.Fatal("state event contains counters")
		}
		data, err := json.Marshal(event)
		if err != nil || strings.Contains(string(data), "query-private-sentinel") || strings.Contains(string(data), "query-event-owner") {
			t.Fatal("event contains private facts")
		}
	}
}

func assertQueryEventState(t *testing.T, db *gorm.DB, id string, taskID uint, status string, progress int) {
	t.Helper()
	var item commonExecution.TaskExecution
	var task models.DevTask
	if err := db.Where("execution_id = ?", id).First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&task, taskID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != status || item.Progress != progress || task.LastExecutionID == nil || *task.LastExecutionID != id || task.LastExecutionStatus != status {
		t.Fatalf("state/summary mismatch: status=%s progress=%d task_status=%s", item.Status, item.Progress, task.LastExecutionStatus)
	}
}
