package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/addp/common/events"
	"github.com/addp/quality/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestQualityCleanupScanFindsEngineBoundState(t *testing.T) {
	t.Parallel()

	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	createQualityCleanupRuleApplication(t, db, 7, 12, "rule-match")
	createQualityCleanupRuleApplication(t, db, 7, 13, "rule-other")
	createQualityCleanupCheckTask(t, db, 7, 12, "task-match")
	createQualityCleanupIssue(t, db, 7, 12, "issue-match", "open")
	createQualityCleanupIssue(t, db, 7, 13, "issue-other", "open")

	stats, err := svc.ScanGarbage(context.Background(), 7, map[string]interface{}{"engine_id": int64(12)})
	if err != nil {
		t.Fatalf("ScanGarbage() error = %v", err)
	}
	if stats.RuleApplications != 1 || stats.CheckTasks != 1 || stats.Issues != 1 {
		t.Fatalf("stats = %#v, want one rule application, check task and issue", stats)
	}

	stats, err = svc.ScanGarbage(context.Background(), 7, nil)
	if err != nil {
		t.Fatalf("ScanGarbage() without context error = %v", err)
	}
	if stats.RuleApplications != 0 || stats.CheckTasks != 0 || stats.Issues != 0 {
		t.Fatalf("stats without lifecycle context = %#v, want empty", stats)
	}
}

func TestQualityCleanupLogicalDisablesEngineBoundState(t *testing.T) {
	t.Parallel()

	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	rule := createQualityCleanupRuleApplication(t, db, 7, 12, "rule-match")
	task := createQualityCleanupCheckTask(t, db, 7, 12, "task-match")
	issue := createQualityCleanupIssue(t, db, 7, 12, "issue-match", "open")
	resolvedIssue := createQualityCleanupIssue(t, db, 7, 12, "issue-resolved", "resolved")

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"engine_id": int64(12)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DisabledRuleApps != 1 || stats.DisabledCheckTasks != 1 || stats.IgnoredIssues != 1 {
		t.Fatalf("stats = %#v, want disabled rule/task and one ignored issue", stats)
	}

	var updatedRule models.RuleApplication
	if err := db.First(&updatedRule, rule.ID).Error; err != nil {
		t.Fatalf("load rule application: %v", err)
	}
	if updatedRule.Enabled {
		t.Fatal("rule application should be disabled")
	}
	var updatedTask models.CheckTask
	if err := db.First(&updatedTask, task.ID).Error; err != nil {
		t.Fatalf("load check task: %v", err)
	}
	if updatedTask.Enabled || updatedTask.NextRunAt != nil {
		t.Fatalf("check task enabled=%v next_run_at=%v, want disabled and unscheduled", updatedTask.Enabled, updatedTask.NextRunAt)
	}
	var updatedIssue models.Issue
	if err := db.First(&updatedIssue, issue.ID).Error; err != nil {
		t.Fatalf("load issue: %v", err)
	}
	if updatedIssue.Status != "ignored" {
		t.Fatalf("issue status = %q, want ignored", updatedIssue.Status)
	}
	var unchangedIssue models.Issue
	if err := db.First(&unchangedIssue, resolvedIssue.ID).Error; err != nil {
		t.Fatalf("load resolved issue: %v", err)
	}
	if unchangedIssue.Status != "resolved" {
		t.Fatalf("resolved issue status = %q, want resolved", unchangedIssue.Status)
	}
}

func TestQualityCleanupPhysicalDeletesTenantOwnedState(t *testing.T) {
	t.Parallel()

	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	rule := createQualityCleanupRuleApplication(t, db, 7, 12, "tenant-rule")
	task := createQualityCleanupCheckTask(t, db, 7, 12, "tenant-task")
	issue := createQualityCleanupIssue(t, db, 7, 12, "tenant-issue", "open")
	otherTenantRule := createQualityCleanupRuleApplication(t, db, 8, 12, "other-rule")

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"tenant_id": uint(7)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DeletedRuleApplications != 1 || stats.DeletedCheckTasks != 1 || stats.DeletedIssues != 1 {
		t.Fatalf("stats = %#v, want tenant-owned quality state deleted", stats)
	}
	for name, id := range map[string]int64{
		"rule":  rule.ID,
		"task":  task.ID,
		"issue": issue.ID,
	} {
		var count int64
		var model interface{}
		switch name {
		case "rule":
			model = &models.RuleApplication{}
		case "task":
			model = &models.CheckTask{}
		case "issue":
			model = &models.Issue{}
		}
		if err := db.Model(model).Where("id = ?", id).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		if count != 0 {
			t.Fatalf("%s should be deleted", name)
		}
	}
	if err := db.First(&models.RuleApplication{}, otherTenantRule.ID).Error; err != nil {
		t.Fatalf("other tenant rule application should remain: %v", err)
	}
}

func newQualityCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS quality").Error; err != nil {
		t.Fatalf("attach quality schema: %v", err)
	}
	statements := []string{
		`CREATE TABLE quality.rule_applications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			element_id INTEGER NOT NULL,
			engine_id INTEGER NOT NULL,
			schema_name TEXT,
			table_name TEXT NOT NULL,
			column_name TEXT NOT NULL,
			rule_config JSON NOT NULL,
			enabled BOOLEAN,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE quality.check_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			engine_id INTEGER NOT NULL,
			schema_name TEXT,
			table_name TEXT,
			enabled BOOLEAN,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			last_run_at DATETIME,
			next_run_at DATETIME,
			last_execution_id TEXT,
			last_execution_status TEXT
		)`,
		`CREATE TABLE quality.issues (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			execution_id TEXT NOT NULL,
			rule_application_id INTEGER NOT NULL,
			rule_type TEXT NOT NULL,
			column_name TEXT NOT NULL,
			table_name TEXT NOT NULL,
			schema_name TEXT,
			engine_id INTEGER NOT NULL,
			failed_count INTEGER NOT NULL,
			total_count INTEGER NOT NULL,
			pass_rate REAL NOT NULL,
			detail JSON,
			status TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	return db
}

func createQualityCleanupRuleApplication(t *testing.T, db *gorm.DB, tenantID int64, engineID int64, name string) models.RuleApplication {
	t.Helper()
	item := models.RuleApplication{
		TenantID:   tenantID,
		ElementID:  1,
		EngineID:   engineID,
		SchemaName: "public",
		Table:      name,
		ColumnName: "value",
		RuleConfig: json.RawMessage(`{"type":"not_null"}`),
		Enabled:    true,
		CreatedBy:  1,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create rule application: %v", err)
	}
	return item
}

func createQualityCleanupCheckTask(t *testing.T, db *gorm.DB, tenantID int64, engineID int64, name string) models.CheckTask {
	t.Helper()
	item := models.CheckTask{
		TenantID:   tenantID,
		Name:       name,
		EngineID:   engineID,
		SchemaName: "public",
		Table:      name,
		Enabled:    true,
		CreatedBy:  1,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create check task: %v", err)
	}
	return item
}

func createQualityCleanupIssue(t *testing.T, db *gorm.DB, tenantID int64, engineID int64, name string, status string) models.Issue {
	t.Helper()
	item := models.Issue{
		TenantID:          tenantID,
		ExecutionID:       "exec-" + name,
		RuleApplicationID: 1,
		RuleType:          "not_null",
		ColumnName:        "value",
		Table:             name,
		SchemaName:        "public",
		EngineID:          engineID,
		FailedCount:       1,
		TotalCount:        10,
		PassRate:          90,
		Detail:            json.RawMessage(`{}`),
		Status:            status,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return item
}
