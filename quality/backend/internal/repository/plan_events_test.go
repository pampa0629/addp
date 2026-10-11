package repository

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	qualityMigration "github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestQualityExecutionEventsAreAtomicAndFenced(t *testing.T) {
	testQualityExecutionEvents(t, newPlanDeletionRepositoryTestDB(t), 71)
}

// The standard Quality PostgreSQL gate discovers this prefix in repository tests.
func TestIntegrationPostgresQualityExecutionEvents(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(qualityRepositoryIntegrationDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := qualityMigration.NewRunner(db).Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()%100000000 + 950000000
	t.Cleanup(func() {
		for _, model := range []interface{}{&commonExecution.TaskExecution{}, &models.Issue{}, &models.QualityPlan{}} {
			if err := db.Where("tenant_id = ?", tenantID).Delete(model).Error; err != nil {
				t.Error(err)
			}
		}
	})
	testQualityExecutionEvents(t, db, tenantID)
}

func testQualityExecutionEvents(t *testing.T, db *gorm.DB, tenantID int64) {
	t.Helper()
	repo := NewPlanRepository(db)
	plan := createPlanRepositoryTestTask(t, db, tenantID)
	ctx := t.Context()
	for _, status := range []string{"success", "failed", "timeout", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			now := time.Now().UTC()
			item := newQualityRepositoryTestExecution(uuid.NewString(), int(tenantID), now)
			item.ExecutionConfig = commonModels.JSONMap{"private": "quality-event-sensitive-sentinel"}
			if _, err := repo.CreateExecution(ctx, plan.ID, tenantID, item, models.PlanRunRequest{}); err != nil {
				t.Fatal(err)
			}
			if err := repo.AttachPendingAuthorization(ctx, tenantID, item.ExecutionID, map[string]interface{}{
				"actor_principal_id": int64(1), "actor_tenant_membership_id": int64(1), "issued_authorization_version": int64(1),
				"execution_authorization_id": now.UnixNano(), "authorization_expires_at": now.Add(time.Hour),
			}); err != nil {
				t.Fatal(err)
			}
			// Failure to persist the started event rolls back claim and owner summary.
			injected := errors.New("injected diagnostic write failure")
			callbackName := "quality_event_create_failure"
			if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
				if _, ok := tx.Statement.Model.(*commonExecution.Event); ok {
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			_, _, err := repo.ClaimPendingExecution(ctx, "quality-events-worker", now, time.Hour)
			_ = db.Callback().Create().Remove(callbackName)
			if !errors.Is(err, injected) {
				t.Fatalf("claim did not report event storage failure: %v", err)
			}
			assertQualityEventKinds(t, db, item.ExecutionID)
			assertQualityExecutionState(t, db, item.ExecutionID, "pending", 0)
			claimed, _, err := repo.ClaimPendingExecution(ctx, "quality-events-worker", now, time.Hour)
			if err != nil || claimed == nil || claimed.Attempt != 1 {
				t.Fatalf("claim after rollback: %v", err)
			}
			lease, err := commonExecution.LeaseFromExecution(*claimed)
			if err != nil {
				t.Fatal(err)
			}
			assertQualityEventKinds(t, db, item.ExecutionID, "started")
			if err := repo.UpdateExecutionProgress(ctx, lease, 25); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"token", "owner", "attempt", "tenant"} {
				stale := lease
				switch field {
				case "token":
					stale.Token = uuid.NewString()
				case "owner":
					stale.Owner = "stale-worker"
				case "attempt":
					stale.Attempt++
				case "tenant":
					stale.TenantID++
				}
				if err := repo.UpdateExecutionProgress(ctx, stale, 75); !errors.Is(err, commonAPI.ErrConflict) {
					t.Fatalf("%s progress accepted: %v", field, err)
				}
				if err := repo.CompleteExecutionWithLease(ctx, plan.ID, tenantID, stale, status, nil, now); !errors.Is(err, commonAPI.ErrConflict) {
					t.Fatalf("%s completion accepted: %v", field, err)
				}
			}
			if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", item.ExecutionID).Update("lease_expires_at", now.Add(-time.Minute)).Error; err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateExecutionProgress(ctx, lease, 75); !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("expired progress accepted: %v", err)
			}
			if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", item.ExecutionID).Update("lease_expires_at", now.Add(time.Hour)).Error; err != nil {
				t.Fatal(err)
			}
			// A later owner-summary failure rolls back event, terminal state and issues.
			callbackName = "quality_summary_update_failure"
			if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Table == (models.QualityPlan{}).TableName() {
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			observation := models.IssueObservation{TargetKey: strings.Repeat("a", 64), PlanID: plan.ID,
				RuleKey: "00000000-0000-4000-8000-000000000001", RuleType: "not_null", Severity: "error",
				Table: "orders", SchemaName: "public", EngineID: 12, ColumnName: "id", FailedCount: 1, TotalCount: 2, PassRate: 50}
			err = repo.CompleteExecutionWithLease(ctx, plan.ID, tenantID, lease, status, nil, now, observation)
			_ = db.Callback().Update().Remove(callbackName)
			if !errors.Is(err, injected) {
				t.Fatalf("completion did not report owner failure: %v", err)
			}
			assertQualityEventKinds(t, db, item.ExecutionID, "started", "progress")
			assertQualityExecutionState(t, db, item.ExecutionID, "running", 25)
			var issueCount int64
			if err := db.Model(&models.Issue{}).Where("tenant_id = ?", tenantID).Count(&issueCount).Error; err != nil || issueCount != 0 {
				t.Fatalf("issue reconciliation escaped rollback: count=%d err=%v", issueCount, err)
			}
			if err := repo.CompleteExecutionWithLease(ctx, plan.ID, tenantID, lease, status, nil, now); err != nil {
				t.Fatal(err)
			}
			kind := status
			if kind == "success" {
				kind = "completed"
			}
			assertQualityEventKinds(t, db, item.ExecutionID, "started", "progress", kind)
			if err := repo.CompleteExecutionWithLease(ctx, plan.ID, tenantID, lease, status, nil, now); !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("duplicate completion accepted: %v", err)
			}
			allowed := commonExecution.WithReadScopes(ctx, []commonExecution.ReadScope{{Module: "quality", TenantID: int(tenantID), PrincipalID: 1, Grants: []commonExecution.ReadGrant{{TaskType: "quality_plan", TaskHistory: true}}}})
			reader := commonExecution.NewTaskExecutionRepository(db)
			page, err := reader.ListEvents(allowed, item.ExecutionID, int(tenantID), 0, 2)
			if err != nil || len(page.Items) != 2 || !page.HasMore || page.Items[1].Counters["progress"] != float64(25) {
				t.Fatalf("authorized event page: %+v, %v", page, err)
			}
			next, err := reader.ListEvents(allowed, item.ExecutionID, int(tenantID), page.NextCursor, 2)
			if err != nil || len(next.Items) != 1 || next.HasMore || next.Items[0].Kind != kind {
				t.Fatalf("event cursor: %+v, %v", next, err)
			}
			denied := commonExecution.WithReadScopes(ctx, []commonExecution.ReadScope{})
			if _, err := reader.ListEvents(denied, item.ExecutionID, int(tenantID), 0, 100); err == nil {
				t.Fatal("reader without Owner scope received events")
			}
			if _, err := reader.ListEvents(allowed, item.ExecutionID, int(tenantID)+1, 0, 100); err == nil {
				t.Fatal("cross-tenant reader received events")
			}
			raw, _ := json.Marshal(page)
			for _, private := range []string{"quality-event-sensitive-sentinel", lease.Token, "execution_config", "authorization", "fencing_token"} {
				if strings.Contains(string(raw), private) {
					t.Fatal("event response contains a private execution field")
				}
			}
		})
	}
	t.Run("retry_attempts", func(t *testing.T) {
		now := time.Now().UTC()
		item := newQualityRepositoryTestExecution(uuid.NewString(), int(tenantID), now)
		item.MaxAttempts = 2
		if _, err := repo.CreateExecution(ctx, plan.ID, tenantID, item, models.PlanRunRequest{}); err != nil {
			t.Fatal(err)
		}
		if err := repo.AttachPendingAuthorization(ctx, tenantID, item.ExecutionID, map[string]interface{}{
			"actor_principal_id": int64(1), "actor_tenant_membership_id": int64(1), "issued_authorization_version": int64(1),
			"execution_authorization_id": now.UnixNano(), "authorization_expires_at": now.Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
		first, _, err := repo.ClaimPendingExecution(ctx, "first-worker", now, time.Hour)
		if err != nil || first == nil {
			t.Fatalf("first claim: %v", err)
		}
		firstLease, err := commonExecution.LeaseFromExecution(*first)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.UpdateExecutionProgress(ctx, firstLease, 33); err != nil {
			t.Fatal(err)
		}
		expire := func() {
			t.Helper()
			if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", item.ExecutionID).Update("lease_expires_at", now.Add(-time.Minute)).Error; err != nil {
				t.Fatal(err)
			}
			if err := repo.RecoverExpiredExecutions(ctx, now); err != nil {
				t.Fatal(err)
			}
		}
		expire()
		second, _, err := repo.ClaimPendingExecution(ctx, "replacement-worker", now, time.Hour)
		if err != nil || second == nil || second.Attempt != 2 || second.Progress != 0 {
			t.Fatalf("replacement did not reset progress: %v, %v", second, err)
		}
		if err := repo.UpdateExecutionProgress(ctx, firstLease, 66); !errors.Is(err, commonAPI.ErrConflict) {
			t.Fatalf("old attempt appended progress: %v", err)
		}
		expire()
		if err := repo.RecoverExpiredExecutions(ctx, now); err != nil {
			t.Fatal(err)
		}
		assertQualityExecutionState(t, db, item.ExecutionID, "failed", 0)
		assertQualityEventKinds(t, db, item.ExecutionID, "started", "progress", "started", "failed")
		var events []commonExecution.Event
		if err := db.Where("execution_id = ?", item.ExecutionID).Order("id").Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		for index, attempt := range []int{1, 1, 2, 2} {
			if events[index].Attempt != attempt {
				t.Fatalf("event %d lost attempt identity", index)
			}
		}
	})
}

func assertQualityExecutionState(t *testing.T, db *gorm.DB, id, status string, progress int) {
	t.Helper()
	var item commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", id).First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != status || item.Progress != progress {
		t.Fatalf("execution state=%s progress=%d, want %s/%d", item.Status, item.Progress, status, progress)
	}
}
