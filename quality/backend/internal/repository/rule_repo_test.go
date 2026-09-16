package repository

import (
	"context"
	"encoding/json"
	"errors"
	commonExecution "github.com/addp/common/execution"
	qualityMigration "github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
	"time"
)

func TestReusableRuleRevisionIsolation(t *testing.T) {
	testReusableRuleRevisionIsolation(t, newPlanRepositoryTestDB(t))
}
func TestIntegrationPostgresReusableRuleRevisionIsolation(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("PostgreSQL integration is disabled")
	}
	db, err := gorm.Open(postgres.Open(qualityRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err = qualityMigration.NewRunner(db).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	testReusableRuleRevisionIsolation(t, tx)
}
func testReusableRuleRevisionIsolation(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	rr := NewRuleRepository(db)
	pr := NewPlanRepository(db)
	rule := models.QualityRule{TenantID: 7881, Code: "shared_length", RuleContent: models.RuleContent{Name: "length", Type: "length", Params: json.RawMessage(`{"constraint":{"max":64}}`)}, CreatedBy: 1, UpdatedBy: 1}
	if err := rr.Create(ctx, &rule); err != nil {
		t.Fatal(err)
	}
	makePlan := func(code string, n int) *models.QualityPlan {
		p := &models.QualityPlan{TenantID: rule.TenantID, Code: code, Name: code, Version: 1, CreatedBy: 1, UpdatedBy: 1, TableBindings: json.RawMessage(`[{"alias":"target","locator":"addp://engine/12/path/public/orders?type=table"}]`)}
		for i := 0; i < n; i++ {
			p.CheckItems = append(p.CheckItems, models.PlanCheckItem{RuleKey: uuid.NewString(), RuleID: rule.ID, RevisionNo: 1, Severity: "error", Bindings: models.CheckBindings{Table: "target", Column: "id"}})
		}
		if err := pr.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, b := makePlan("first", 2), makePlan("second", 1)
	rule.Params = json.RawMessage(`{"constraint":{"max":32}}`)
	if err := rr.Replace(ctx, &rule, 1); err != nil {
		t.Fatal(err)
	}
	if err := rr.Replace(ctx, &rule, 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale update=%v", err)
	}
	for _, p := range []*models.QualityPlan{a, b} {
		got, err := pr.Get(ctx, p.TenantID, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range got.CheckItems {
			if item.RevisionNo != 1 || item.Rule.LatestRevisionNo != 2 || string(item.Rule.Params) == string(rule.Params) {
				t.Fatalf("pinned rule changed: %+v", item)
			}
		}
	}
	plans, total, err := rr.Plans(ctx, rule.TenantID, rule.ID, 1, 20)
	if err != nil || total != 2 || len(plans) != 2 {
		t.Fatalf("references=%d/%d %v", len(plans), total, err)
	}
	gotRule, err := rr.Get(ctx, rule.TenantID, rule.ID)
	if err != nil || gotRule.PlanCount != 2 {
		t.Fatalf("plan count=%+v %v", gotRule, err)
	}
	if err := rr.Delete(ctx, rule.TenantID, rule.ID, 2); !errors.Is(err, ErrRuleReferenced) {
		t.Fatalf("delete referenced=%v", err)
	}
	a.CheckItems[0].RevisionNo = 2
	if err := pr.Replace(ctx, a, 1); err != nil {
		t.Fatal(err)
	}
	a, err = pr.Get(ctx, a.TenantID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.CheckItems[0].RevisionNo != 2 || a.CheckItems[1].RevisionNo != 1 {
		t.Fatal("explicit upgrade changed unrelated item")
	}
	bad := *a
	bad.CheckItems = append([]models.PlanCheckItem(nil), a.CheckItems...)
	bad.CheckItems[0].RevisionNo = 999
	if err := pr.Replace(ctx, &bad, 2); err == nil {
		t.Fatal("nonexistent revision accepted")
	}
	a, err = pr.Get(ctx, a.TenantID, a.ID)
	if err != nil || a.Version != 2 {
		t.Fatalf("failed replace was not atomic: %+v %v", a, err)
	}
	if err := rr.Resolve(ctx, rule.TenantID+1, a.CheckItems); err == nil {
		t.Fatal("cross-tenant reference accepted")
	}
	execution := newQualityRepositoryTestExecution(uuid.NewString(), int(rule.TenantID), time.Now().UTC())
	if _, err := pr.CreateExecution(ctx, a.ID, a.TenantID, execution); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(execution.ExecutionConfig)
	rule.Params = json.RawMessage(`{"constraint":{"max":8}}`)
	if err := rr.Replace(ctx, &rule, 2); err != nil {
		t.Fatal(err)
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id=?", execution.ExecutionID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(stored.ExecutionConfig)
	if string(before) != string(after) {
		t.Fatal("queued execution changed after rule edit")
	}
	if err := db.Where("tenant_id=? AND execution_id=?", rule.TenantID, execution.ExecutionID).Delete(&commonExecution.TaskExecution{}).Error; err != nil {
		t.Fatal(err)
	}
	// Releasing one plan must not release references in another.
	if err := db.Where("tenant_id=? AND plan_id=?", a.TenantID, a.ID).Delete(&models.PlanCheckItem{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := rr.Delete(ctx, rule.TenantID, rule.ID, 3); !errors.Is(err, ErrRuleReferenced) {
		t.Fatalf("other plan reference lost: %v", err)
	}
	if err := db.Where("tenant_id=? AND plan_id=?", b.TenantID, b.ID).Delete(&models.PlanCheckItem{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := rr.Delete(ctx, rule.TenantID, rule.ID, 3); err != nil {
		t.Fatal(err)
	}
}
