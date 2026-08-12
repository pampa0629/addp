package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/quality/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestIssueReconcileIsIdempotentAndReopensResolvedIssues(t *testing.T) {
	db := newIssueRepositoryTestDB(t)
	repo := NewIssueRepository(db)
	failed := models.IssueObservation{
		RuleApplicationID: 12, RuleType: "not_null", Severity: "error", Message: "required",
		ColumnName: "email", Table: "users", SchemaName: "public", EngineID: 3,
		FailedCount: 2, TotalCount: 10, PassRate: 80,
	}
	firstSeen := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	if err := repo.Reconcile(context.Background(), 7, "execution-1", []models.IssueObservation{failed}, firstSeen); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	var issue models.Issue
	if err := db.Where("tenant_id = ? AND rule_application_id = ?", 7, failed.RuleApplicationID).First(&issue).Error; err != nil {
		t.Fatalf("load first issue: %v", err)
	}
	if issue.Status != "open" || issue.ExecutionID != "execution-1" || issue.LastExecutionID != "execution-1" {
		t.Fatalf("first issue = %#v", issue)
	}
	issueID := issue.ID

	failed.FailedCount = 3
	failed.PassRate = 70
	secondSeen := firstSeen.Add(time.Hour)
	if err := repo.Reconcile(context.Background(), 7, "execution-2", []models.IssueObservation{failed}, secondSeen); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	var count int64
	if err := db.Model(&models.Issue{}).Where("tenant_id = ? AND rule_application_id = ?", 7, failed.RuleApplicationID).Count(&count).Error; err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if count != 1 {
		t.Fatalf("issue count = %d, want 1", count)
	}
	issue = models.Issue{}
	if err := db.First(&issue, issueID).Error; err != nil {
		t.Fatalf("reload updated issue: %v", err)
	}
	if issue.Status != "open" || issue.ExecutionID != "execution-1" || issue.LastExecutionID != "execution-2" || issue.FailedCount != 3 || issue.PassRate != 70 {
		t.Fatalf("updated issue = %#v", issue)
	}

	passed := failed
	passed.Passed = true
	resolvedAt := secondSeen.Add(time.Hour)
	if err := repo.Reconcile(context.Background(), 7, "execution-3", []models.IssueObservation{passed}, resolvedAt); err != nil {
		t.Fatalf("resolve Reconcile: %v", err)
	}
	issue = models.Issue{}
	if err := db.First(&issue, issueID).Error; err != nil {
		t.Fatalf("reload resolved issue: %v", err)
	}
	if issue.Status != "resolved" || issue.LastExecutionID != "execution-3" || issue.ResolvedAt == nil || issue.ResolvedBy != nil || issue.ResolutionNote != "" {
		t.Fatalf("resolved issue = %#v", issue)
	}

	if err := repo.Reconcile(context.Background(), 7, "execution-4", []models.IssueObservation{failed}, resolvedAt.Add(time.Hour)); err != nil {
		t.Fatalf("reopen Reconcile: %v", err)
	}
	issue = models.Issue{}
	if err := db.First(&issue, issueID).Error; err != nil {
		t.Fatalf("reload reopened issue: %v", err)
	}
	if issue.Status != "open" || issue.ExecutionID != "execution-1" || issue.LastExecutionID != "execution-4" || issue.ResolvedAt != nil || issue.ResolutionNote != "" {
		t.Fatalf("reopened issue = %#v", issue)
	}
}

func TestIssueUpdateStatusRequiresNoteAndOpenState(t *testing.T) {
	db := newIssueRepositoryTestDB(t)
	repo := NewIssueRepository(db)
	issue := models.Issue{TenantID: 8, ExecutionID: "execution-1", LastExecutionID: "execution-1", RuleApplicationID: 13, RuleType: "unique", Severity: "error", ColumnName: "id", Table: "users", SchemaName: "public", EngineID: 3, Status: "open"}
	if err := repo.Create(&issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := repo.UpdateStatus(context.Background(), issue.ID, issue.TenantID, 99, "resolved", "  "); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("blank note error = %v, want bad request", err)
	}
	if err := repo.UpdateStatus(context.Background(), issue.ID, issue.TenantID, 99, "resolved", "fixed source data"); err != nil {
		t.Fatalf("resolve issue: %v", err)
	}
	issue = models.Issue{}
	if err := db.First(&issue, 1).Error; err != nil {
		t.Fatalf("reload manually resolved issue: %v", err)
	}
	if issue.ResolvedAt == nil || issue.ResolvedBy == nil || *issue.ResolvedBy != 99 || issue.ResolutionNote != "fixed source data" {
		t.Fatalf("manual resolution audit = %#v", issue)
	}
	if err := repo.UpdateStatus(context.Background(), issue.ID, issue.TenantID, 99, "ignored", "second transition"); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("second transition error = %v, want conflict", err)
	}
	if err := repo.UpdateStatus(context.Background(), issue.ID, issue.TenantID, 99, "invalid", "note"); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("invalid status error = %v, want bad request", err)
	}
}

func newIssueRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS quality").Error; err != nil {
		t.Fatalf("attach quality schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE quality.issues (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		execution_id TEXT NOT NULL,
		last_execution_id TEXT NOT NULL,
		rule_application_id INTEGER NOT NULL,
		rule_type TEXT NOT NULL,
		severity TEXT NOT NULL DEFAULT 'error',
		message TEXT,
		column_name TEXT NOT NULL,
		table_name TEXT NOT NULL,
		schema_name TEXT,
		engine_id INTEGER NOT NULL,
		failed_count INTEGER NOT NULL DEFAULT 0,
		total_count INTEGER NOT NULL DEFAULT 0,
		pass_rate REAL NOT NULL DEFAULT 100,
		detail BLOB,
		status TEXT NOT NULL DEFAULT 'open',
		resolved_at DATETIME,
		resolved_by INTEGER,
		resolution_note TEXT,
		last_observed_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME,
		UNIQUE (tenant_id, rule_application_id)
	)`).Error; err != nil {
		t.Fatalf("create quality issues table: %v", err)
	}
	return db
}
