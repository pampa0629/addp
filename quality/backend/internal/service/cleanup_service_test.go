package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/addp/quality/internal/testsupport"
	"strconv"
	"strings"
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
	createQualityCleanupPlan(t, db, 7, 13, "task_other")
	createQualityCleanupPlan(t, db, 7, 12, "task-match")
	createQualityCleanupIssue(t, db, 7, 12, "issue-match", "open")
	createQualityCleanupIssue(t, db, 7, 13, "issue-other", "open")

	stats, err := svc.ScanReclaimCandidates(context.Background(), 7, map[string]interface{}{"engine_id": int64(12)})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() error = %v", err)
	}
	if stats.Plans != 1 || stats.Issues != 1 {
		t.Fatalf("stats = %#v, want one rule application, check task and issue", stats)
	}

	stats, err = svc.ScanReclaimCandidates(context.Background(), 7, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() without context error = %v", err)
	}
	if stats.Plans != 0 || stats.Issues != 0 {
		t.Fatalf("stats without lifecycle context = %#v, want empty", stats)
	}
}

func TestQualityCleanupLogicalDisablesEngineBoundState(t *testing.T) {
	t.Parallel()

	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	task := createQualityCleanupPlan(t, db, 7, 12, "task-match")
	issue := createQualityCleanupIssue(t, db, 7, 12, "issue-match", "open")
	resolvedIssue := createQualityCleanupIssue(t, db, 7, 12, "issue-resolved", "resolved")

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"engine_id": int64(12)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.IgnoredIssues != 1 {
		t.Fatalf("stats = %#v, want disabled rule and one ignored issue", stats)
	}

	var updatedTask models.QualityPlan
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

func TestQualityEngineDeletionImpactKeepsPlanRebindable(t *testing.T) {
	t.Parallel()

	candidates := qualityCleanupCandidates{
		plans:  []models.QualityPlan{{ID: 2}},
		issues: []models.Issue{{ID: 3}},
	}
	impact, err := qualityEngineDeletionImpact(candidates, nil)
	if err != nil {
		t.Fatalf("qualityEngineDeletionImpact() error = %v", err)
	}
	if impact.Summary.Rebindable != 1 || impact.Summary.WillDisable != 1 || impact.Summary.WillDelete != 0 {
		t.Fatalf("impact summary = %#v, want one rebindable task and two state records to disable", impact.Summary)
	}
	if impact.ManagementPath != "/quality/plans" {
		t.Fatalf("management path = %q", impact.ManagementPath)
	}
}

func TestQualityEngineDeletionImpactUsesExecutionFacts(t *testing.T) {
	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	task := createQualityCleanupPlan(t, db, 7, 12, "task-match")
	createQualityCleanupExecution(t, db, task, commonExecution.ExecutionStatusRunning)

	activeTaskIDs, err := svc.listActiveQualityCleanupTaskIDs(context.Background(), []models.QualityPlan{task})
	if err != nil {
		t.Fatalf("listActiveQualityCleanupTaskIDs() error = %v", err)
	}
	impact, err := qualityEngineDeletionImpact(qualityCleanupCandidates{plans: []models.QualityPlan{task}}, activeTaskIDs)
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
	if len(stats.Errors) != 1 || stats.IgnoredIssues != 0 {
		t.Fatalf("stats = %#v, want one error and no committed updates", stats)
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
	task := createQualityCleanupPlan(t, db, 7, 12, "task-match")
	createQualityCleanupExecution(t, db, task, commonExecution.ExecutionStatusPending)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"tenant_id": uint(7)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if len(stats.Errors) != 1 {
		t.Fatalf("stats = %#v, want running task error and no committed updates", stats)
	}

}

func TestQualityCleanupLogicalIgnoresStaleRunningSummary(t *testing.T) {
	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	task := createQualityCleanupPlan(t, db, 7, 12, "task-match")
	if err := db.Model(&models.QualityPlan{}).Where("id = ?", task.ID).Update("last_execution_status", "running").Error; err != nil {
		t.Fatalf("mark task summary running: %v", err)
	}

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"tenant_id": uint(7)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if len(stats.Errors) != 0 {
		t.Fatalf("stats = %#v, want cleanup driven by execution facts", stats)
	}

}

func TestQualityCleanupPhysicalDeletesTenantOwnedState(t *testing.T) {
	t.Parallel()

	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	task := createQualityCleanupPlan(t, db, 7, 12, "tenant-task")
	issue := createQualityCleanupIssue(t, db, 7, 12, "tenant-issue", "open")
	otherTenantRule := createQualityCleanupPlan(t, db, 8, 12, "other-rule")

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"tenant_id": uint(7)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DeletedPlans != 1 || stats.DeletedIssues != 1 {
		t.Fatalf("stats = %#v, want tenant-owned quality state deleted", stats)
	}
	for name, id := range map[string]int64{
		"task":  task.ID,
		"issue": issue.ID,
	} {
		var count int64
		var model interface{}
		switch name {
		case "task":
			model = &models.QualityPlan{}
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
	if err := db.First(&models.QualityPlan{}, otherTenantRule.ID).Error; err != nil {
		t.Fatalf("other tenant rule application should remain: %v", err)
	}
}

func TestQualityCleanupPhysicalRollsBackWhenTaskDeleteFails(t *testing.T) {
	db := newQualityCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	task := createQualityCleanupPlan(t, db, 7, 12, "tenant-task")
	issue := createQualityCleanupIssue(t, db, 7, 12, "tenant-issue", "open")
	if err := db.Exec(`CREATE TRIGGER quality.fail_quality_task_delete
		BEFORE DELETE ON quality.plans
		BEGIN SELECT RAISE(ABORT, 'forced task delete failure'); END`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"tenant_id": uint(7)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if len(stats.Errors) != 1 || stats.DeletedIssues != 0 || stats.DeletedPlans != 0 {
		t.Fatalf("stats = %#v, want one error and no committed deletes", stats)
	}
	for name, id := range map[string]int64{"task": task.ID, "issue": issue.ID} {
		var count int64
		var model interface{}
		switch name {
		case "task":
			model = &models.QualityPlan{}
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
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
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
		`CREATE TABLE quality.plans (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			code TEXT NOT NULL, version INTEGER NOT NULL, table_bindings JSON NOT NULL,
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
				plan_id INTEGER NOT NULL,
				rule_key TEXT NOT NULL,
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
	testsupport.EnsureRuleTables(t, db)
	return db
}

func createQualityCleanupPlan(t *testing.T, db *gorm.DB, tenantID int64, engineID int64, name string) models.QualityPlan {
	t.Helper()
	bindings, _ := json.Marshal([]PlanTableBinding{{Alias: "target", Locator: fmt.Sprintf("addp://engine/%d/path/public/%s?type=table", engineID, name)}})
	item := models.QualityPlan{TenantID: tenantID, Name: name, Code: strings.ReplaceAll(name, "-", "_"), Version: 1, CreatedBy: 1, UpdatedBy: 1, TableBindings: bindings, Rules: json.RawMessage(`{"schema_version":"addp.quality.plan-rules/v1","rules":[]}`)}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create check task: %v", err)
	}
	return item
}

func createQualityCleanupIssue(t *testing.T, db *gorm.DB, tenantID int64, engineID int64, name string, status string) models.Issue {
	t.Helper()
	item := models.Issue{
		TenantID:    tenantID,
		ExecutionID: "exec-" + name,
		PlanID:      1,
		RuleKey:     "00000000-0000-4000-8000-000000000001",
		RuleType:    "not_null",
		ColumnName:  "value",
		Table:       name,
		SchemaName:  "public",
		EngineID:    engineID,
		FailedCount: 1,
		TotalCount:  10,
		PassRate:    90,
		Detail:      json.RawMessage(`{}`),
		Status:      status,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return item
}

func createQualityCleanupExecution(t *testing.T, db *gorm.DB, task models.QualityPlan, status string) commonExecution.TaskExecution {
	t.Helper()
	now := time.Now().UTC()
	sourceTaskID := strconv.FormatInt(task.ID, 10)
	item := commonExecution.TaskExecution{TenantID: int(task.TenantID), ExecutionID: "execution-" + sourceTaskID, SourceTaskID: &sourceTaskID, Status: status}
	if err := db.Exec(`INSERT INTO common.task_executions
		(tenant_id, execution_id, module, task_type, source, source_task_id, status, trigger_type, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.TenantID, item.ExecutionID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan,
		commonExecution.ModuleQuality, sourceTaskID, status, commonExecution.TriggerTypeManual, now, now).Error; err != nil {
		t.Fatalf("create task execution: %v", err)
	}
	return item
}
