package service

import (
	"context"
	"encoding/json"
	"errors"
	commonAPI "github.com/addp/common/api"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"testing"
	"time"
)

func TestPlanExecutionRejectsInvalidOrigins(t *testing.T) {
	svc := NewPlanService(nil, time.Minute)
	for _, origin := range []struct{ source, parent, trigger string }{
		{"quality", "", "manual"}, {"orchestrator", "", "manual"}, {"orchestrator", "not-uuid", "manual"}, {"other", "", "manual"}, {"orchestrator", "00000000-0000-4000-8000-000000000001", "retry"},
	} {
		if _, err := svc.Execute(context.Background(), 7, 1, origin.trigger, origin.source, origin.parent, models.PlanRunRequest{}); !errors.Is(err, commonAPI.ErrBadRequest) {
			t.Fatalf("origin %#v: %v", origin, err)
		}
	}
}

func TestPlanExecutionSnapshotRequiresVersionAndPositiveBudget(t *testing.T) {
	config := commonModels.JSONMap{"schema_version": planExecutionConfigVersion, "task_version": 1, "check_timeout_ms": 45000,
		"table_bindings": []PlanTableBinding{{Alias: "orders", Locator: "addp://engine/12/path/public/orders?type=table"}},
		"rules":          json.RawMessage(`{"schema_version":"addp.quality.plan-rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"not_null","severity":"error","params":{"table":"orders","column":"id"}}]}`)}
	config["target_key"], _ = models.PlanTargetKey(config["table_bindings"].([]PlanTableBinding))
	got, err := decodePlanExecutionConfig(config)
	if err != nil || got.CheckTimeoutMS != 45000 {
		t.Fatalf("%#v %v", got, err)
	}
	for key, value := range map[string]interface{}{"schema_version": "old", "task_version": 0, "check_timeout_ms": 0, "unknown": true} {
		copy := commonModels.JSONMap{}
		for k, v := range config {
			copy[k] = v
		}
		copy[key] = value
		if _, err := decodePlanExecutionConfig(copy); err == nil {
			t.Fatalf("accepted invalid %s", key)
		}
	}
}

func TestPlanFailureAndDeadlineReasons(t *testing.T) {
	err := failExecution(planRuleFailedCode, errors.New("private diagnostic"))
	if executionFailureCode(err) != planRuleFailedCode {
		t.Fatal(err)
	}
	if executionFailureCode(errors.New("unknown")) != qualityExecutionFailedCode {
		t.Fatal("unknown failure code")
	}
	if !errors.Is(executionErrorForDeadline(nil, true), context.DeadlineExceeded) {
		t.Fatal("deadline must override a completed result")
	}
}

func TestPinnedStandardConstraintMayChangePresentationNotConstraint(t *testing.T) {
	old := PlanRule{RuleKey: "key", Type: "length", Source: &RuleSource{ElementID: 1, ElementRevisionID: 2, RuleKey: "standard-key"}, Params: json.RawMessage(`{"table":"t","column":"c","constraint":{"min":1}}`)}
	doc := PlanRuleDocument{Rules: []PlanRule{old}}
	next := old
	next.Name = "updated"
	next.Disabled = true
	next.Severity = "warning"
	next.Params = json.RawMessage(`{"constraint":{"min":1},"table":"other","column":"name"}`)
	if !unchangedStandardConstraint(next, doc) {
		t.Fatal("pinned constraint unexpectedly follows upstream")
	}
	next.Params = json.RawMessage(`{"table":"t","column":"c","constraint":{"min":2}}`)
	if unchangedStandardConstraint(next, doc) {
		t.Fatal("constraint modification retained provenance")
	}
}
