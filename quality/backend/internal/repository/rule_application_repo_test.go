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

func TestDeleteRuleApplicationRemovesCurrentIssueAndPreservesCompletedExecution(t *testing.T) {
	db := newRuleApplicationRepositoryTestDB(t)
	repo := NewRuleApplicationRepository(db)
	application := createRuleApplicationRepositoryTestApplication(t, db, 7)
	createRuleApplicationRepositoryTestTask(t, db, application)
	createRuleApplicationRepositoryTestIssue(t, db, application)
	createRuleApplicationRepositoryTestExecution(t, db, application, commonExecution.ExecutionStatusSuccess)

	if err := repo.Delete(context.Background(), application.ID, application.TenantID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	assertRuleApplicationRepositoryCount(t, db, &models.RuleApplication{}, "id = ?", application.ID, 0)
	assertRuleApplicationRepositoryCount(t, db, &models.Issue{}, "tenant_id = ? AND rule_application_id = ?", application.TenantID, application.ID, 0)
	assertRuleApplicationRepositoryCount(t, db, &commonExecution.TaskExecution{}, "tenant_id = ?", application.TenantID, 1)
}

func TestDeleteRuleApplicationRejectsActiveExecutionSnapshot(t *testing.T) {
	for _, status := range []string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning} {
		t.Run(status, func(t *testing.T) {
			db := newRuleApplicationRepositoryTestDB(t)
			repo := NewRuleApplicationRepository(db)
			application := createRuleApplicationRepositoryTestApplication(t, db, 8)
			createRuleApplicationRepositoryTestTask(t, db, application)
			createRuleApplicationRepositoryTestIssue(t, db, application)
			createRuleApplicationRepositoryTestExecution(t, db, application, status)

			err := repo.Delete(context.Background(), application.ID, application.TenantID)
			if !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("Delete error = %v, want conflict", err)
			}
			assertRuleApplicationRepositoryCount(t, db, &models.RuleApplication{}, "id = ?", application.ID, 1)
			assertRuleApplicationRepositoryCount(t, db, &models.Issue{}, "tenant_id = ? AND rule_application_id = ?", application.TenantID, application.ID, 1)
		})
	}
}

func TestDeleteRuleApplicationIsTenantScoped(t *testing.T) {
	db := newRuleApplicationRepositoryTestDB(t)
	repo := NewRuleApplicationRepository(db)
	application := createRuleApplicationRepositoryTestApplication(t, db, 9)

	err := repo.Delete(context.Background(), application.ID, 10)
	if !errors.Is(err, commonAPI.ErrNotFound) {
		t.Fatalf("Delete error = %v, want not found", err)
	}
	assertRuleApplicationRepositoryCount(t, db, &models.RuleApplication{}, "id = ?", application.ID, 1)
}

func createRuleApplicationRepositoryTestTask(t *testing.T, db *gorm.DB, application models.RuleApplication) {
	t.Helper()
	task := models.CheckTask{
		TenantID: application.TenantID, Name: "quality-task", EngineID: application.EngineID,
		SchemaName: application.SchemaName, Table: application.Table, CreatedBy: 1,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create check task: %v", err)
	}
}

func newRuleApplicationRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newCheckTaskRepositoryTestDB(t)
	if err := db.Exec(`CREATE TABLE quality.issues (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		execution_id TEXT NOT NULL,
		last_execution_id TEXT NOT NULL,
			rule_application_id INTEGER NOT NULL,
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
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create quality issues test table: %v", err)
	}
	return db
}

func createRuleApplicationRepositoryTestApplication(t *testing.T, db *gorm.DB, tenantID int64) models.RuleApplication {
	t.Helper()
	application := models.RuleApplication{
		TenantID: tenantID, ElementID: 11, EngineID: 12, SchemaName: "public", Table: "orders", ColumnName: "amount",
		RuleConfig: json.RawMessage(`{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","enabled":true,"severity":"error","message":"","params":{}}]}`),
		Enabled:    true, CreatedBy: 1,
	}
	if err := db.Create(&application).Error; err != nil {
		t.Fatalf("create rule application: %v", err)
	}
	return application
}

func createRuleApplicationRepositoryTestIssue(t *testing.T, db *gorm.DB, application models.RuleApplication) {
	t.Helper()
	now := time.Now().UTC()
	issue := models.Issue{
		TenantID: application.TenantID, ExecutionID: "first", LastExecutionID: "latest", RuleApplicationID: application.ID,
		RuleKey: "00000000-0000-4000-8000-000000000001", RuleType: "not_null", Severity: "error", ColumnName: application.ColumnName, Table: application.Table,
		SchemaName: application.SchemaName, EngineID: application.EngineID, FailedCount: 1, TotalCount: 1,
		PassRate: 0, Detail: json.RawMessage(`{}`), Status: "open", LastObservedAt: &now,
	}
	if err := db.Create(&issue).Error; err != nil {
		t.Fatalf("create issue: %v", err)
	}
}

func createRuleApplicationRepositoryTestExecution(t *testing.T, db *gorm.DB, application models.RuleApplication, status string) {
	t.Helper()
	now := time.Now().UTC()
	execution := newQualityRepositoryTestExecution("rule-application-"+status, int(application.TenantID), now)
	execution.Status = status
	var task models.CheckTask
	if err := db.Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?", application.TenantID, application.EngineID, application.SchemaName, application.Table).First(&task).Error; err != nil {
		t.Fatalf("load task for execution: %v", err)
	}
	sourceTaskID := fmt.Sprint(task.ID)
	execution.SourceTaskID = &sourceTaskID
	execution.ExecutionConfig = map[string]interface{}{
		"rule_applications": []interface{}{map[string]interface{}{"id": float64(application.ID)}},
	}
	if err := db.Create(execution).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
}

func assertRuleApplicationRepositoryCount(t *testing.T, db *gorm.DB, model interface{}, query string, args ...interface{}) {
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
