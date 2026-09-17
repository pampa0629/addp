package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
)

func TestDeletePlanDeletionRemovesCurrentIssueAndPreservesCompletedExecution(t *testing.T) {
	db := newPlanDeletionRepositoryTestDB(t)
	repo := NewPlanRepository(db)
	application := createPlanRepositoryTestTask(t, db, 7)
	createPlanDeletionRepositoryTestIssue(t, db, application)
	createPlanDeletionRepositoryTestExecution(t, db, application, commonExecution.ExecutionStatusSuccess)

	if err := repo.Delete(context.Background(), application.TenantID, application.ID, application.Version); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	assertPlanDeletionRepositoryCount(t, db, &models.QualityPlan{}, "id = ?", application.ID, 0)
	assertPlanDeletionRepositoryCount(t, db, &models.Issue{}, "tenant_id = ? AND plan_id = ?", application.TenantID, application.ID, 0)
	assertPlanDeletionRepositoryCount(t, db, &commonExecution.TaskExecution{}, "tenant_id = ?", application.TenantID, 1)
}

func TestDeletePlanDeletionRejectsActiveExecutionSnapshot(t *testing.T) {
	for _, status := range []string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning} {
		t.Run(status, func(t *testing.T) {
			db := newPlanDeletionRepositoryTestDB(t)
			repo := NewPlanRepository(db)
			application := createPlanRepositoryTestTask(t, db, 8)
			createPlanDeletionRepositoryTestIssue(t, db, application)
			createPlanDeletionRepositoryTestExecution(t, db, application, status)

			err := repo.Delete(context.Background(), application.TenantID, application.ID, application.Version)
			if !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("Delete error = %v, want conflict", err)
			}
			assertPlanDeletionRepositoryCount(t, db, &models.QualityPlan{}, "id = ?", application.ID, 1)
			assertPlanDeletionRepositoryCount(t, db, &models.Issue{}, "tenant_id = ? AND plan_id = ?", application.TenantID, application.ID, 1)
		})
	}
}

func TestDeletePlanDeletionIsTenantScoped(t *testing.T) {
	db := newPlanDeletionRepositoryTestDB(t)
	repo := NewPlanRepository(db)
	application := createPlanRepositoryTestTask(t, db, 9)

	err := repo.Delete(context.Background(), 10, application.ID, application.Version)
	if !errors.Is(err, commonAPI.ErrNotFound) {
		t.Fatalf("Delete error = %v, want not found", err)
	}
	assertPlanDeletionRepositoryCount(t, db, &models.QualityPlan{}, "id = ?", application.ID, 1)
}

func newPlanDeletionRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newPlanRepositoryTestDB(t)
	if err := db.Exec(`CREATE TABLE quality.issues (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		owner_domain_id INTEGER,
		execution_id TEXT NOT NULL,
		last_execution_id TEXT NOT NULL,
			plan_id INTEGER NOT NULL,
			target_key TEXT,
			rule_key TEXT NOT NULL,
		rule_type TEXT NOT NULL,
		severity TEXT NOT NULL,
		message TEXT,
		column_name TEXT NOT NULL,
		table_name TEXT NOT NULL,
		schema_name TEXT NOT NULL,
		engine_id INTEGER NOT NULL,
		failed_count INTEGER NOT NULL,
		total_count INTEGER NOT NULL,
		pass_rate REAL NOT NULL,
		detail JSON,
		status TEXT NOT NULL,
		resolved_at DATETIME,
		resolved_by INTEGER,
		resolution_note TEXT,
		last_observed_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME,
		UNIQUE(tenant_id,plan_id,target_key,rule_key)
	)`).Error; err != nil {
		t.Fatalf("create quality issues test table: %v", err)
	}
	return db
}

func createPlanDeletionRepositoryTestIssue(t *testing.T, db *gorm.DB, application models.QualityPlan) {
	t.Helper()
	now := time.Now().UTC()
	issue := models.Issue{
		TenantID: application.TenantID, ExecutionID: "first", LastExecutionID: "latest", PlanID: application.ID,
		RuleKey: "00000000-0000-4000-8000-000000000001", RuleType: "not_null", Severity: "error", ColumnName: "id", Table: "orders",
		SchemaName: "public", EngineID: 12, FailedCount: 1, TotalCount: 1,
		PassRate: 0, Detail: json.RawMessage(`{}`), Status: "open", LastObservedAt: &now,
	}
	if err := db.Create(&issue).Error; err != nil {
		t.Fatalf("create issue: %v", err)
	}
}

func createPlanDeletionRepositoryTestExecution(t *testing.T, db *gorm.DB, application models.QualityPlan, status string) {
	t.Helper()
	now := time.Now().UTC()
	execution := newQualityRepositoryTestExecution("rule-application-"+status, int(application.TenantID), now)
	execution.Status = status
	sourceTaskID := fmt.Sprint(application.ID)
	execution.SourceTaskID = &sourceTaskID
	if err := db.Create(execution).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
}

func assertPlanDeletionRepositoryCount(t *testing.T, db *gorm.DB, model interface{}, query string, args ...interface{}) {
	t.Helper()
	want := args[len(args)-1].(int)
	args = args[:len(args)-1]
	var count int64
	if err := db.Model(model).Where(query, args...).Count(&count).Error; err != nil {
		t.Fatalf("count %T: %v", model, err)
	}
	if count != int64(want) {
		t.Fatalf("count %T = %d, want %d", model, count, want)
	}
}
