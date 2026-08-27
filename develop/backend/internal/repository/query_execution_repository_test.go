package repository

import (
	"context"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestQueryExecutionRepositoryClaimsOnlyOrchestratorQueries(t *testing.T) {
	db := newQueryExecutionTestDB(t)
	now := time.Now().UTC()
	task := createQueryExecutionTestTask(t, db)
	for index, source := range []string{commonExecution.ModuleDevelop, commonExecution.ModuleOrchestrator} {
		executionID := uuid.NewString()
		execution := commonExecution.TaskExecution{
			TenantID: 7, ExecutionID: executionID, Module: commonExecution.ModuleDevelop,
			TaskType: commonExecution.TaskTypeQuery, Source: source,
			SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), Status: commonExecution.ExecutionStatusPending,
			ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, TriggerType: commonExecution.TriggerTypeManual,
			ExecutionConfig: commonModels.JSONMap{}, MaxAttempts: 3,
			CreatedAt: now.Add(time.Duration(index) * time.Second), UpdatedAt: now,
		}
		if err := db.Create(&execution).Error; err != nil {
			t.Fatalf("create query execution: %v", err)
		}
	}
	claimed, lease, err := NewQueryExecutionRepository(db).ClaimNext(context.Background(), "query-worker", now, time.Minute)
	if err != nil || claimed == nil || lease == nil {
		t.Fatalf("ClaimNext = %#v %#v, %v", claimed, lease, err)
	}
	if claimed.Source != commonExecution.ModuleOrchestrator || claimed.Attempt != 1 {
		t.Fatalf("claimed execution = %#v", claimed)
	}
}

func TestQueryExecutionRepositoryFailsAllExpiredQueries(t *testing.T) {
	for name, relationResult := range map[string]bool{"ordinary": false, "relation_result": true} {
		t.Run(name, func(t *testing.T) {
			db := newQueryExecutionTestDB(t)
			now := time.Now().UTC()
			task := createQueryExecutionTestTask(t, db)
			executionID := uuid.NewString()
			leaseToken := uuid.NewString()
			leaseOwner := "query-worker-1"
			expiresAt := now.Add(-time.Minute)
			authorizationExpiresAt := now.Add(time.Hour)
			authorizationID := int64(81)
			principalID, membershipID, version := int64(11), int64(12), int64(2)
			config := commonModels.JSONMap{"content": commonModels.JSONMap{"query_type": "sql", "query": "SELECT 1"}}
			if relationResult {
				config["content"] = commonModels.JSONMap{
					"query_type": "sql", "query": "SELECT * FROM addp_input.source",
					"relation_inputs": []interface{}{"source"},
				}
				config["runtime_inputs"] = commonModels.JSONMap{
					"input_locators": commonModels.JSONMap{"source": "addp://engine/9/path/public/source?type=table"},
					"target_locator": "addp://engine/9/path/public/result?type=table",
				}
			}
			execution := commonExecution.TaskExecution{
				TenantID: 7, ExecutionID: executionID, Module: commonExecution.ModuleDevelop,
				TaskType: commonExecution.TaskTypeQuery, Source: commonExecution.ModuleOrchestrator,
				SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), Status: commonExecution.ExecutionStatusRunning,
				ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, TriggerType: commonExecution.TriggerTypeManual,
				ExecutionConfig: config, Attempt: 1, MaxAttempts: 3,
				LeaseToken: &leaseToken, LeaseOwner: &leaseOwner, LeaseExpiresAt: &expiresAt,
				ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
				IssuedAuthorizationVersion: &version, ExecutionAuthorizationID: &authorizationID,
				AuthorizationExpiresAt: &authorizationExpiresAt, StartedAt: &expiresAt,
				CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute),
			}
			if err := db.Create(&execution).Error; err != nil {
				t.Fatalf("create expired query execution: %v", err)
			}
			if err := db.Model(&models.DevTask{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
				"last_execution_id": executionID, "last_execution_status": commonExecution.ExecutionStatusRunning,
			}).Error; err != nil {
				t.Fatalf("set task execution summary: %v", err)
			}
			count, err := NewQueryExecutionRepository(db).RecoverExpired(context.Background(), now, 10)
			if err != nil || count != 1 {
				t.Fatalf("RecoverExpired = %d, %v", count, err)
			}
			var recovered commonExecution.TaskExecution
			if err := db.Where("execution_id = ?", executionID).First(&recovered).Error; err != nil {
				t.Fatalf("load recovered execution: %v", err)
			}
			if recovered.Status != commonExecution.ExecutionStatusFailed || recovered.CompletedAt == nil || recovered.LeaseToken != nil {
				t.Fatalf("expired query was not failed closed = %#v", recovered)
			}
		})
	}
}

func newQueryExecutionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatalf("attach develop schema: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure execution store: %v", err)
	}
	if err := db.Exec(`CREATE TABLE develop.dev_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
		display_name TEXT, dev_type TEXT NOT NULL, content JSON NOT NULL, execution_config JSON, editor_layout JSON,
		timeout INTEGER, status TEXT, last_execution_id TEXT, last_execution_status TEXT,
		last_run_at DATETIME, description TEXT, tags TEXT, created_by INTEGER, updated_by INTEGER,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create dev_tasks: %v", err)
	}
	return db
}

func createQueryExecutionTestTask(t *testing.T, db *gorm.DB) *models.DevTask {
	t.Helper()
	task := &models.DevTask{
		TenantID: 7, Name: "query task", DevType: commonExecution.TaskTypeQuery,
		Content:         models.DevTaskContent{"query_type": "sql", "query": "SELECT 1"},
		ExecutionConfig: models.DevTaskContent{"engine_id": 9}, Status: "active", Timeout: 60,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("create query task: %v", err)
	}
	return task
}
