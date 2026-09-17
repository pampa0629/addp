package repository

import (
	"context"
	"errors"
	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
	"testing"
	"time"
)

func TestPlanScopesCanRunTogetherWithoutOverwritingLatestSummary(t *testing.T) {
	db := newPlanRepositoryTestDB(t)
	repo := NewPlanRepository(db)
	ctx := context.Background()
	plan := createPlanRepositoryTestTask(t, db, 7)
	now := time.Now().UTC()
	for i, region := range []string{"east", "west"} {
		e := newQualityRepositoryTestExecution(region, 7, now.Add(time.Duration(i)*time.Millisecond))
		req := models.PlanRunRequest{TableBindings: map[string]string{"orders": "addp://engine/12/path/" + region + "/orders?type=table"}}
		if _, err := repo.CreateExecution(ctx, plan.ID, 7, e, req); err != nil {
			t.Fatal(err)
		}
		if err := repo.AttachPendingAuthorization(ctx, 7, e.ExecutionID, map[string]interface{}{"execution_authorization_id": int64(1)}); err != nil {
			t.Fatal(err)
		}
		duplicate := newQualityRepositoryTestExecution(region+"-duplicate", 7, now)
		if _, err := repo.CreateExecution(ctx, plan.ID, 7, duplicate, req); !errors.Is(err, commonAPI.ErrConflict) {
			t.Fatalf("duplicate scope accepted: %v", err)
		}
	}
	first, _, err := repo.ClaimPendingExecution(ctx, "worker-a", now.Add(time.Second), time.Minute)
	if err != nil || first == nil {
		t.Fatalf("first claim %v", err)
	}
	second, _, err := repo.ClaimPendingExecution(ctx, "worker-b", now.Add(time.Second), time.Minute)
	if err != nil || second == nil || first.ExecutionID == second.ExecutionID {
		t.Fatalf("second claim %v", err)
	}
	for _, e := range []*commonExecution.TaskExecution{second, first} {
		lease, err := commonExecution.LeaseFromExecution(*e)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.CompleteExecutionWithLease(ctx, plan.ID, 7, lease, "success", nil, now.Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := repo.Get(ctx, 7, plan.ID)
	if err != nil || stored.LastExecutionID != "west" || stored.LastExecutionStatus != "success" {
		t.Fatalf("latest summary=%+v %v", stored, err)
	}
	if string(stored.TableBindings) != string(plan.TableBindings) {
		t.Fatal("execution mutated default targets")
	}
}

func TestIssuesAreIsolatedByCompleteTargetScope(t *testing.T) {
	db := newIssueRepositoryTestDB(t)
	repo := NewIssueRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	observation := models.IssueObservation{TargetKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PlanID: 1, RuleKey: "00000000-0000-4000-8000-000000000001", RuleType: "not_null", Severity: "error", Table: "orders", ColumnName: "id", EngineID: 12, TotalCount: 10, FailedCount: 1, PassRate: 90}
	if err := repo.Reconcile(ctx, 7, "east-fail", []models.IssueObservation{observation}, now); err != nil {
		t.Fatal(err)
	}
	observation.TargetKey = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := repo.Reconcile(ctx, 7, "west-fail", []models.IssueObservation{observation}, now); err != nil {
		t.Fatal(err)
	}
	observation.Passed = true
	if err := repo.Reconcile(ctx, 7, "west-pass", []models.IssueObservation{observation}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	issues, total, err := repo.List(7, "open", 0, nil, 1, 20)
	if err != nil || total != 1 || issues[0].LastExecutionID != "east-fail" {
		t.Fatalf("scope isolation: %+v %d %v", issues, total, err)
	}
}
