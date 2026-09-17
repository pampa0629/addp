package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	qualityMigration "github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresAssertionIssuePreservesMultiColumnEvidence(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(qualityRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := qualityMigration.NewRunner(db).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	tenantID := time.Now().UnixNano()%100000000 + 980000000
	plan := createPlanRepositoryTestTask(t, tx, tenantID)
	columns := strings.Repeat("source_column, ", 30) + "last_column"
	observation := models.IssueObservation{TargetKey: strings.Repeat("a", 64), PlanID: plan.ID, RuleKey: "00000000-0000-4000-8000-000000000001", RuleType: "relational_assertion", Severity: "warning", Message: "cross-table consistency", ColumnName: columns, Table: "facts", SchemaName: "public", EngineID: 12, FailedCount: 1, TotalCount: 10, PassRate: 90}
	repo := NewIssueRepository(tx)
	if err := repo.Reconcile(context.Background(), tenantID, "assertion-failure", []models.IssueObservation{observation}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var issue models.Issue
	if err := tx.Where("tenant_id = ? AND plan_id = ?", tenantID, plan.ID).First(&issue).Error; err != nil {
		t.Fatal(err)
	}
	if issue.ColumnName != columns || issue.RuleType != "relational_assertion" || issue.Status != "open" {
		t.Fatalf("lost evidence: %#v", issue)
	}
	observation.Passed = true
	observation.FailedCount = 0
	observation.PassRate = 100
	if err := repo.Reconcile(context.Background(), tenantID, "assertion-recovery", []models.IssueObservation{observation}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := tx.First(&issue, issue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if issue.Status != "resolved" || issue.ColumnName != columns {
		t.Fatalf("issue did not recover with evidence: %#v", issue)
	}
}
