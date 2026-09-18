package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	qualityMigration "github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresAcceptanceIsVersionedAndSurvivesReconciliation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("standard PostgreSQL gate required")
	}
	db, err := gorm.Open(postgres.Open(qualityRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err = qualityMigration.NewRunner(db).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	tenant := time.Now().UnixNano()%100000000 + 980000000
	defer func() {
		for _, model := range []interface{}{&models.IssueAction{}, &models.Issue{}, &models.PlanCheckItem{}, &models.QualityPlan{}} {
			if err := db.Where("tenant_id=?", tenant).Delete(model).Error; err != nil {
				t.Error(err)
			}
		}
	}()
	plan := models.QualityPlan{TenantID: tenant, Code: "acceptance", Name: "acceptance", Version: 1, TableBindings: []byte(`[{"alias":"a","locator":""}]`)}
	if err = db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	r := NewIssueRepository(db)
	ctx := context.Background()
	o := models.IssueObservation{TargetKey: strings.Repeat("a", 64), PlanID: plan.ID, RuleKey: "00000000-0000-4000-8000-000000000001", RuleType: "foreign_key", Severity: "warning", FailedCount: 1, TotalCount: 2, PassRate: 50, Evidence: &models.FailureEvidence{Scope: strings.Repeat("b", 64), Keys: []string{strings.Repeat("c", 64)}}}
	if err = r.Reconcile(ctx, tenant, "first", []models.IssueObservation{o}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var issue models.Issue
	if err = db.Where("tenant_id=?", tenant).First(&issue).Error; err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := r.UpdateStatus(ctx, issue.ID, tenant, 42, issue.Version, "accepted", "historical review")
			results <- err
		}()
	}
	close(start)
	ok, conflict := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			ok++
		} else if errors.Is(err, ErrVersionConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("concurrent actions=%d conflicts=%d", ok, conflict)
	}
	if err = r.Reconcile(ctx, tenant, "second", []models.IssueObservation{o}, time.Now()); err != nil {
		t.Fatal(err)
	}
	accepted, err := r.Get(issue.ID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status != "accepted" || accepted.AcceptedCount != 1 || accepted.FailedCount != 1 || accepted.PassRate != 50 || len(accepted.History) != 1 {
		t.Fatalf("accepted=%+v", accepted)
	}
	o.Evidence.Keys = []string{strings.Repeat("d", 64)}
	if err = r.Reconcile(ctx, tenant, "third", []models.IssueObservation{o}, time.Now()); err != nil {
		t.Fatal(err)
	}
	opened, err := r.Get(issue.ID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Status != "open" || opened.PendingCount != 1 || opened.AcceptedCount != 0 || len(opened.History) != 2 || opened.History[0].Note != "historical review" {
		t.Fatalf("reopened=%+v", opened)
	}
	if err = NewPlanRepository(db).Delete(ctx, tenant, plan.ID, plan.Version); err != nil {
		t.Fatal(err)
	}
	var history int64
	if err = db.Model(&models.IssueAction{}).Where("tenant_id=? AND issue_id=?", tenant, issue.ID).Count(&history).Error; err != nil {
		t.Fatal(err)
	}
	if history != 2 {
		t.Fatal("plan deletion erased audit")
	}
}
