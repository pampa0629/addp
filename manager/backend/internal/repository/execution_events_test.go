package repository

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestManagerExecutionEvents(t *testing.T) {
	runManagerExecutionEvents(t, func(t *testing.T) *gorm.DB {
		db := newTileCacheExecutionRepositoryTestDB(t)
		sqlDB, _ := db.DB()
		t.Cleanup(func() { _ = sqlDB.Close() })
		if err := db.Exec(`CREATE TABLE manager.embedding_tasks (
 id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
 description TEXT, enabled BOOLEAN, created_by INTEGER, schedule TEXT, next_run_at DATETIME,
 config JSON, last_execution_id TEXT, last_execution_status TEXT, last_run_at DATETIME,
 created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
)`).Error; err != nil {
			t.Fatal(err)
		}
		return db
	})
}

func TestIntegrationPostgresManagerExecutionEvents(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-manager-postgres")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.TileCacheTask{}, &models.TileCache{}, &models.EmbeddingTask{}); err != nil {
		t.Fatal(err)
	}
	runManagerExecutionEvents(t, func(t *testing.T) *gorm.DB {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		t.Cleanup(func() {
			if err := tx.Rollback().Error; err != nil {
				t.Error(err)
			}
		})
		return tx
	})
}

func runManagerExecutionEvents(t *testing.T, database func(*testing.T) *gorm.DB) {
	for _, taskType := range ManagerBoundedTaskTypes() {
		for _, status := range []string{"success", "failed", "timeout", "cancelled"} {
			t.Run(taskType+"/"+status, func(t *testing.T) {
				db := database(t)
				item, taskID := seedManagerEventExecution(t, db, taskType)
				queue := NewBoundedExecutionQueueRepository(db)
				_, lease, err := queue.ClaimNext(t.Context(), []string{taskType}, "event-owner", time.Now().UTC(), time.Minute)
				if err != nil || lease == nil || lease.ExecutionID != item.ExecutionID {
					t.Fatalf("claim: %v %v", lease, err)
				}
				assertManagerEvents(t, db, item, "started")
				if taskType == commonExecution.TaskTypeEmbedding {
					if err := NewEmbeddingRepository(db).CompleteAdHocExecution(t.Context(), *lease, status, time.Now().UTC(), nil); err == nil {
						t.Fatal("ad-hoc completion bypassed task summary ownership")
					}
				}
				if err := queue.RenewLease(t.Context(), *lease, time.Now().UTC().Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
				ctx := commonExecution.ContextWithLease(t.Context(), *lease)
				complete := func(l commonExecution.Lease) error {
					fields := map[string]interface{}{"status": status, "metadata": commonModels.JSONMap{"runtime_response": "private-event-sentinel"}}
					switch taskType {
					case commonExecution.TaskTypeDataProfiling:
						return NewDataProfileExecutionRepository(db).updateTerminal(commonExecution.ContextWithLease(ctx, l), item.TenantID, item.ExecutionID, status, map[string]interface{}{"completed_at": time.Now().UTC()})
					case commonExecution.TaskTypePPTXPDFGeneration:
						return NewPPTXPDFRepository(db).CompleteExecutionWithLease(ctx, taskID, uint(item.TenantID), l, 0, nil, fields, time.Now().UTC())
					case commonExecution.TaskTypeEmbedding:
						return NewEmbeddingRepository(db).CompleteTaskExecution(commonExecution.ContextWithLease(ctx, l), taskID, uint(item.TenantID), item.ExecutionID, fields, time.Now().UTC())
					default:
						return newTaskExecutionLifecycle(db).Complete(commonExecution.ContextWithLease(ctx, l), taskID, uint(item.TenantID), item.ExecutionID, time.Now().UTC(), taskExecutionCompletionSpec{TaskModel: &models.TileCacheTask{}, ExecutionFields: fields}, "event-test")
					}
				}
				wrong := *lease
				wrong.Token = uuid.NewString()
				if err := complete(wrong); err == nil {
					t.Fatal("wrong lease completed")
				}
				assertManagerEvents(t, db, item, "started")
				if err := complete(*lease); err != nil {
					t.Fatal(err)
				}
				terminal := status
				if terminal == "success" {
					terminal = "completed"
				}
				assertManagerEvents(t, db, item, "started", terminal)
				if err := complete(*lease); err == nil {
					t.Fatal("terminal execution accepted duplicate completion")
				}
				assertManagerEvents(t, db, item, "started", terminal)
			})
		}
	}
	for _, phase := range []string{"claim", "complete", "recover"} {
		t.Run("storage_rollback/"+phase, func(t *testing.T) {
			db := database(t)
			item, taskID := seedManagerEventExecution(t, db, commonExecution.TaskTypeVectorTileCacheGeneration)
			queue := NewBoundedExecutionQueueRepository(db)
			var lease *commonExecution.Lease
			if phase != "claim" {
				var err error
				_, lease, err = queue.ClaimNext(t.Context(), []string{item.TaskType}, "event-owner", time.Now().UTC(), time.Minute)
				if err != nil || lease == nil {
					t.Fatal(err)
				}
			}
			if phase == "recover" {
				if err := db.Model(item).Update("lease_expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
			}
			artifact := models.TileCache{TenantID: uint(item.TenantID), TaskID: &taskID, LastExecutionID: &item.ExecutionID,
				ItemFingerprint: uuid.NewString(), TileFormat: "pmtiles", SourceVersion: "test", ProfileHash: "test", Status: "generating"}
			if err := db.Create(&artifact).Error; err != nil {
				t.Fatal(err)
			}
			const callback = "test:manager-event-failure"
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*commonExecution.Event); ok {
					tx.AddError(errors.New("event store unavailable"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Callback().Create().Remove(callback) })
			var err error
			switch phase {
			case "claim":
				_, _, err = queue.ClaimNext(t.Context(), []string{item.TaskType}, "event-owner", time.Now().UTC(), time.Minute)
			case "complete":
				err = NewTileCacheRepository(db).CompleteExecution(commonExecution.ContextWithLease(t.Context(), *lease), taskID, uint(item.TenantID), item.ExecutionID, artifact.ID, map[string]interface{}{"status": "ready"}, map[string]interface{}{"status": "success"}, time.Now().UTC())
			case "recover":
				_, err = queue.RecoverExpired(t.Context(), time.Now().UTC(), 100)
			}
			if err == nil {
				t.Fatal("event failure did not roll back operation")
			}
			want := "running"
			if phase == "claim" {
				want = "pending"
				assertManagerEvents(t, db, item)
			} else {
				assertManagerEvents(t, db, item, "started")
			}
			var stored commonExecution.TaskExecution
			var task models.TileCacheTask
			if err := db.First(&stored, item.ID).Error; err != nil || stored.Status != want {
				t.Fatalf("execution escaped rollback: %s %v", stored.Status, err)
			}
			if err := db.First(&task, taskID).Error; err != nil || task.LastExecutionStatus == nil || *task.LastExecutionStatus != want {
				t.Fatalf("summary escaped rollback: %v", err)
			}
			if err := db.First(&artifact, artifact.ID).Error; err != nil || artifact.Status != "generating" {
				t.Fatalf("professional result escaped rollback: %s %v", artifact.Status, err)
			}
		})
	}
	for _, phase := range []string{"owner_failure", "expired", "unleased", "missing_task", "ad_hoc_embedding", "missing_summary"} {
		t.Run(phase, func(t *testing.T) {
			db := database(t)
			taskType := commonExecution.TaskTypeVectorTileCacheGeneration
			if phase == "ad_hoc_embedding" {
				taskType = commonExecution.TaskTypeEmbedding
			}
			item, taskID := seedManagerEventExecution(t, db, taskType)
			queue := NewBoundedExecutionQueueRepository(db)
			if phase == "ad_hoc_embedding" {
				if err := db.Model(item).Update("source_task_id", nil).Error; err != nil {
					t.Fatal(err)
				}
			}
			if phase == "missing_task" {
				if err := db.Unscoped().Delete(&models.TileCacheTask{}, taskID).Error; err != nil {
					t.Fatal(err)
				}
			}
			claimed, lease, err := queue.ClaimNext(t.Context(), []string{taskType}, "event-owner", time.Now().UTC(), time.Minute)
			if phase == "missing_task" {
				if err != nil || claimed != nil || lease != nil {
					t.Fatalf("orphan claim: %v", err)
				}
				assertManagerEvents(t, db, item, "started", "failed")
				return
			}
			if err != nil || lease == nil {
				t.Fatal(err)
			}
			switch phase {
			case "owner_failure":
				err = queue.FailClaimed(t.Context(), claimed, *lease, "manager.execution.test_failed", "private-event-sentinel", time.Now().UTC())
			case "expired":
				if err := db.Model(item).Update("lease_expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
					t.Fatal(err)
				}
				var count int
				count, err = queue.RecoverExpired(t.Context(), time.Now().UTC(), 100)
				if count != 1 {
					t.Fatalf("recovered %d: %v", count, err)
				}
				if err := queue.FailClaimed(t.Context(), claimed, *lease, "late", "late", time.Now().UTC()); err == nil {
					t.Fatal("expired worker wrote a terminal event")
				}
			case "unleased":
				if err := db.Model(item).Updates(map[string]interface{}{"attempt": 0, "lease_token": nil}).Error; err != nil {
					t.Fatal(err)
				}
				_, err = queue.RecoverUnleased(t.Context(), time.Now().UTC(), 100)
				// The original real claim remains; recovery cannot invent a second event.
				assertManagerEvents(t, db, item, "started")
				if err != nil {
					t.Fatal(err)
				}
				return
			case "ad_hoc_embedding":
				err = NewEmbeddingRepository(db).CompleteAdHocExecution(t.Context(), *lease, "success", time.Now().UTC(), nil)
				if err != nil {
					t.Fatal(err)
				}
				assertManagerEvents(t, db, item, "started", "completed")
				return
			case "missing_summary":
				if err := db.Unscoped().Delete(&models.TileCacheTask{}, taskID).Error; err != nil {
					t.Fatal(err)
				}
				err = NewTileCacheRepository(db).CompleteExecution(commonExecution.ContextWithLease(t.Context(), *lease), taskID, uint(item.TenantID), item.ExecutionID, 0, nil, map[string]interface{}{"status": "success"}, time.Now().UTC())
				if err == nil {
					t.Fatal("completion ignored failed summary")
				}
				assertManagerEvents(t, db, item, "started")
				var stored commonExecution.TaskExecution
				if err := db.First(&stored, item.ID).Error; err != nil || stored.Status != "running" {
					t.Fatalf("execution escaped summary rollback: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertManagerEvents(t, db, item, "started", "failed")
		})
	}
}

func seedManagerEventExecution(t *testing.T, db *gorm.DB, taskType string) (*commonExecution.TaskExecution, uint) {
	t.Helper()
	item := newManagerRepositoryTestExecution(uuid.NewString(), 979000031, taskType, time.Now().UTC())
	item.ExecutionConfig = commonModels.JSONMap{"password": "private-event-sentinel"}
	var taskID uint
	if taskType == commonExecution.TaskTypeDataProfiling {
		authorization := int64(17)
		principal, membership, version := int64(31), int64(41), int64(2)
		item.ActorPrincipalID, item.ActorTenantMembershipID, item.IssuedAuthorizationVersion = &principal, &membership, &version
		expires := time.Now().UTC().Add(time.Hour)
		item.ExecutionAuthorizationID, item.AuthorizationExpiresAt = &authorization, &expires
	} else if taskType == commonExecution.TaskTypeEmbedding {
		pending := "pending"
		task := models.EmbeddingTask{TenantID: uint(item.TenantID), Name: uuid.NewString(), LastExecutionID: &item.ExecutionID, LastExecutionStatus: &pending, Config: commonModels.JSONMap{}}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		taskID = task.ID
	} else {
		pending := "pending"
		task := models.TileCacheTask{TenantID: uint(item.TenantID), TaskType: taskType, Name: uuid.NewString(), LastExecutionID: &item.ExecutionID, LastExecutionStatus: &pending, Config: commonModels.JSONMap{}}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		taskID = task.ID
	}
	if taskID != 0 {
		item.SourceTaskID = commonExecution.NewSourceTaskIDFromUint(taskID)
	}
	if err := db.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	return item, taskID
}

func assertManagerEvents(t *testing.T, db *gorm.DB, item *commonExecution.TaskExecution, kinds ...string) {
	t.Helper()
	var events []commonExecution.Event
	if err := db.Where("execution_id = ?", item.ExecutionID).Order("id").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != len(kinds) {
		t.Fatalf("events=%v want=%v", events, kinds)
	}
	for i, event := range events {
		payload, _ := json.Marshal(event)
		if event.Kind != kinds[i] || event.Attempt != 1 || event.TenantID != item.TenantID || event.Module != "manager" || event.TaskType != item.TaskType || event.StepID != "" || len(event.Counters) != 0 || strings.Contains(string(payload), "private-event-sentinel") {
			t.Fatalf("invalid or private lifecycle event: %s", payload)
		}
	}
}
