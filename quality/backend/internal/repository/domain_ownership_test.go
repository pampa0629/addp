package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestDomainOwnershipListsAndCurrentIssues(t *testing.T) {
	testDomainOwnershipListsAndCurrentIssues(t, newPlanDeletionRepositoryTestDB(t))
}

func testDomainOwnershipListsAndCurrentIssues(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	domain, public := int64(654), int64(0)
	rules, plans, issues := NewRuleRepository(db), NewPlanRepository(db), NewIssueRepository(db)
	var ownedPlan models.QualityPlan
	for i, owner := range []*int64{nil, &domain, &domain} {
		tenant := int64(7981)
		if i == 2 {
			tenant++
		}
		rule := models.QualityRule{TenantID: tenant, Code: fmt.Sprintf("owner_%d", i), OwnerDomainID: owner,
			RuleContent: models.RuleContent{Name: "owner", Type: "not_null", Params: json.RawMessage(`{}`)}}
		if err := rules.Create(ctx, &rule); err != nil {
			t.Fatal(err)
		}
		plan := models.QualityPlan{TenantID: tenant, Code: rule.Code, Name: rule.Code, Version: 1, OwnerDomainID: owner, TableBindings: json.RawMessage(`[{"alias":"orders","locator":"addp://engine/12/path/public/orders?type=table"}]`)}
		if err := plans.Create(ctx, &plan); err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{"open", "resolved", "ignored"} {
			issue := models.Issue{TenantID: tenant, PlanID: plan.ID, OwnerDomainID: owner, RuleKey: uuid.NewString(), RuleType: "not_null", Status: status}
			if err := issues.Create(&issue); err != nil {
				t.Fatal(err)
			}
		}
		if i == 1 {
			ownedPlan = plan
		}
	}
	for _, tc := range []struct {
		owner *int64
		count int64
	}{{nil, 2}, {&public, 1}, {&domain, 1}} {
		r, n, err := rules.List(ctx, 7981, "owner", tc.owner, 1, 1)
		if err != nil || n != tc.count || len(r) != 1 {
			t.Fatalf("rules: %d %d %v", n, len(r), err)
		}
		p, n, err := plans.List(ctx, 7981, tc.owner, 1, 1)
		if err != nil || n != tc.count || len(p) != 1 {
			t.Fatalf("plans: %d %d %v", n, len(p), err)
		}
		items, n, err := issues.List(7981, "open", 0, tc.owner, 1, 1)
		if err != nil || n != tc.count || len(items) != 1 {
			t.Fatalf("issues: %d %d %v", n, len(items), err)
		}
	}
	execution := newQualityRepositoryTestExecution("quality-domain-snapshot", int(ownedPlan.TenantID), time.Now().UTC())
	if _, err := plans.CreateExecution(ctx, ownedPlan.ID, ownedPlan.TenantID, execution, models.PlanRunRequest{}); err != nil {
		t.Fatal(err)
	}
	// Simulate a terminal execution before ownership can be edited.
	if err := db.Model(execution).Update("status", commonExecution.ExecutionStatusSuccess).Error; err != nil {
		t.Fatal(err)
	}
	var before []models.Issue
	if err := db.Where("tenant_id = ? AND plan_id = ?", ownedPlan.TenantID, ownedPlan.ID).Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	ownedPlan.OwnerDomainID = nil
	if err := plans.Replace(ctx, &ownedPlan, ownedPlan.Version); err != nil {
		t.Fatal(err)
	}
	for _, original := range before {
		current, err := issues.Get(original.ID, original.TenantID)
		if err != nil || current.OwnerDomainID != nil || !current.UpdatedAt.Equal(original.UpdatedAt) || current.Status != original.Status {
			t.Fatalf("current issue or audit unexpectedly changed: %+v %v", current, err)
		}
	}
	var frozen commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&frozen).Error; err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(frozen.ExecutionConfig["owner_domain_id"]) != "654" {
		t.Fatalf("snapshot mutated: %+v", frozen.ExecutionConfig)
	}
	_, count, err := issues.List(7981, "", 0, &public, 1, 100)
	if err != nil || count != 6 {
		t.Fatalf("public issues=%d err=%v", count, err)
	}
}
