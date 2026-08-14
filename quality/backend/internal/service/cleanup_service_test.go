package service

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
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

	stats, err := svc.ScanReclaimCandidates(context.Background(), 7, map[string]interface{}{"engine_id": int64(12)})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() error = %v", err)
	}
	if stats.RuleApplications != 1 || stats.CheckTasks != 1 || stats.Issues != 1 {
		t.Fatalf("stats = %#v, want one rule application, check task and issue", stats)
	}

	stats, err = svc.ScanReclaimCandidates(context.Background(), 7, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() without context error = %v", err)
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
	if stats.DisabledRuleApps != 1 || stats.IgnoredIssues != 1 {
		t.Fatalf("stats = %#v, want disabled rule and one ignored issue", stats)
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
	var updatedIssue models.Issue
	if err := db.First(&updatedIssue, issue.ID).Error; err != nil {
		t.Fatalf("load issue: %v", err)
	}
	if updatedIssue.Status != "ignored" {
		t.Fatalf("issue status = %q, want ignored", updatedIssue.Status)
	}
	if updatedIssue.ResolvedAt == nil || updatedIssue.ResolvedBy != nil || updatedIssue.ResolutionNote != "" {
		t.Fatalf("ignored issue audit fields = %#v, want automatic cleanup timestamp", updatedIssue)
	}
	var unchangedIssue models.Issue
	if err := db.First(&unchangedIssue, resolvedIssue.ID).Error; err != nil {
		t.Fatalf("load resolved issue: %v", err)
	}
	if unchangedIssue.Status != "resolved" {
		t.Fatalf("resolved issue status = %q, want resolved", unchangedIssue.Status)
	}
}

func TestQualityEngineDeletionImpactKeepsCheckTaskRebindable(t *testing.T) {
	t.Parallel()

	candidates := qualityCleanupCandidates{
		ruleApplications: []models.RuleApplication{{ID: 1}},
		checkTasks:       []models.CheckTask{{ID: 2}},
		issues:           []models.Issue{{ID: 3}},
	}
	impact, err := qualityEngineDeletionImpact(candidates, nil)
	if err != nil {
		t.Fatalf("qualityEngineDeletionImpact() error = %v", err)
	}
	if impact.Summary.Rebindable != 1 || impact.Summary.WillDisable != 2 || impact.Summary.WillDelete != 0 {
		t.Fatalf("impact summary = %#v, want one rebindable task and two state records to disable", impact.Summary)
	}
	if impact.ManagementPath != "/quality/check-tasks" {
		t.Fatalf("management path = %q", impact.ManagementPath)
	}
}

func TestQualityEngineDeletionImpactUsesExecutionFacts(t *testing.T) {
	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	task := createQualityCleanupCheckTask(t, db, 7, 12, "task-match")
	createQualityCleanupExecution(t, db, task, commonExecution.ExecutionStatusRunning)

	activeTaskIDs, err := svc.listActiveQualityCleanupTaskIDs(context.Background(), []models.CheckTask{task})
	if err != nil {
		t.Fatalf("listActiveQualityCleanupTaskIDs() error = %v", err)
	}
	impact, err := qualityEngineDeletionImpact(qualityCleanupCandidates{checkTasks: []models.CheckTask{task}}, activeTaskIDs)
	if err != nil {
		t.Fatalf("qualityEngineDeletionImpact() error = %v", err)
	}
	if impact.Summary.Rebindable != 1 || impact.Summary.Running != 1 {
		t.Fatalf("impact summary = %#v, want rebindable and running facts", impact.Summary)
	}
}

func TestQualityCleanupLogicalRollsBackWhenIssueUpdateFails(t *testing.T) {
	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	rule := createQualityCleanupRuleApplication(t, db, 7, 12, "rule-match")
	issue := createQualityCleanupIssue(t, db, 7, 12, "issue-match", "open")
	if err := db.Exec(`CREATE TRIGGER quality.fail_quality_issue_ignore
		BEFORE UPDATE OF status ON quality.issues
		WHEN NEW.status = 'ignored'
		BEGIN SELECT RAISE(ABORT, 'forced issue update failure'); END`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"engine_id": int64(12)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if len(stats.Errors) != 1 || stats.DisabledRuleApps != 0 || stats.IgnoredIssues != 0 {
		t.Fatalf("stats = %#v, want one error and no committed updates", stats)
	}
	var storedRule models.RuleApplication
	if err := db.First(&storedRule, rule.ID).Error; err != nil {
		t.Fatalf("load rule application: %v", err)
	}
	if !storedRule.Enabled {
		t.Fatal("rule application was disabled despite transaction rollback")
	}
	var storedIssue models.Issue
	if err := db.First(&storedIssue, issue.ID).Error; err != nil {
		t.Fatalf("load issue: %v", err)
	}
	if storedIssue.Status != "open" {
		t.Fatalf("issue status = %q, want open after rollback", storedIssue.Status)
	}
}

func TestQualityCleanupLogicalRejectsActiveExecutionWhenSummaryIsStale(t *testing.T) {
	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	rule := createQualityCleanupRuleApplication(t, db, 7, 12, "rule-match")
	task := createQualityCleanupCheckTask(t, db, 7, 12, "task-match")
	createQualityCleanupExecution(t, db, task, commonExecution.ExecutionStatusPending)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"tenant_id": uint(7)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if len(stats.Errors) != 1 || stats.DisabledRuleApps != 0 {
		t.Fatalf("stats = %#v, want running task error and no committed updates", stats)
	}
	var storedRule models.RuleApplication
	if err := db.First(&storedRule, rule.ID).Error; err != nil {
		t.Fatalf("load rule application: %v", err)
	}
	if !storedRule.Enabled {
		t.Fatal("rule application was disabled while task was running")
	}
}

func TestQualityCleanupLogicalIgnoresStaleRunningSummary(t *testing.T) {
	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	rule := createQualityCleanupRuleApplication(t, db, 7, 12, "rule-match")
	task := createQualityCleanupCheckTask(t, db, 7, 12, "task-match")
	if err := db.Model(&models.CheckTask{}).Where("id = ?", task.ID).Update("last_execution_status", "running").Error; err != nil {
		t.Fatalf("mark task summary running: %v", err)
	}

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"tenant_id": uint(7)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if len(stats.Errors) != 0 || stats.DisabledRuleApps != 1 {
		t.Fatalf("stats = %#v, want cleanup driven by execution facts", stats)
	}
	var storedRule models.RuleApplication
	if err := db.First(&storedRule, rule.ID).Error; err != nil {
		t.Fatalf("load rule application: %v", err)
	}
	if storedRule.Enabled {
		t.Fatal("stale task summary incorrectly blocked cleanup")
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

func TestQualityCleanupPhysicalRollsBackWhenTaskDeleteFails(t *testing.T) {
	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	rule := createQualityCleanupRuleApplication(t, db, 7, 12, "tenant-rule")
	task := createQualityCleanupCheckTask(t, db, 7, 12, "tenant-task")
	issue := createQualityCleanupIssue(t, db, 7, 12, "tenant-issue", "open")
	if err := db.Exec(`CREATE TRIGGER quality.fail_quality_task_delete
		BEFORE DELETE ON quality.check_tasks
		BEGIN SELECT RAISE(ABORT, 'forced task delete failure'); END`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"tenant_id": uint(7)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if len(stats.Errors) != 1 || stats.DeletedIssues != 0 || stats.DeletedCheckTasks != 0 || stats.DeletedRuleApplications != 0 {
		t.Fatalf("stats = %#v, want one error and no committed deletes", stats)
	}
	for name, id := range map[string]int64{"rule": rule.ID, "task": task.ID, "issue": issue.ID} {
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
		if count != 1 {
			t.Fatalf("%s count = %d, want 1 after rollback", name, count)
		}
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
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
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
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			last_run_at DATETIME,
			last_execution_id TEXT,
			last_execution_status TEXT
		)`,
		`CREATE TABLE quality.issues (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			execution_id TEXT NOT NULL,
			last_execution_id TEXT NOT NULL DEFAULT '',
			rule_application_id INTEGER NOT NULL,
			rule_type TEXT NOT NULL,
			severity TEXT NOT NULL DEFAULT 'error',
			message TEXT,
			column_name TEXT NOT NULL,
			table_name TEXT NOT NULL,
			schema_name TEXT,
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
		RuleConfig: json.RawMessage(`{"schema_version":"addp.quality.rules/v1","rules":[]}`),
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

func createQualityCleanupExecution(t *testing.T, db *gorm.DB, task models.CheckTask, status string) commonExecution.TaskExecution {
	t.Helper()
	now := time.Now().UTC()
	sourceTaskID := strconv.FormatInt(task.ID, 10)
	item := commonExecution.TaskExecution{TenantID: int(task.TenantID), ExecutionID: "execution-" + sourceTaskID, SourceTaskID: &sourceTaskID, Status: status}
	if err := db.Exec(`INSERT INTO common.task_executions
		(tenant_id, execution_id, module, task_type, source, source_task_id, status, trigger_type, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.TenantID, item.ExecutionID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck,
		commonExecution.ModuleQuality, sourceTaskID, status, commonExecution.TriggerTypeManual, now, now).Error; err != nil {
		t.Fatalf("create task execution: %v", err)
	}
	return item
}
