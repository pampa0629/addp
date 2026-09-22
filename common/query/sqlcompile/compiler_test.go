package sqlcompile

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

func TestRelationalCompilerCheckReportsUnsupportedPlanOperation(t *testing.T) {
	p := resultPlan()
	p.Assertions = nil
	p.Nodes = p.Nodes[:1]
	p.Nodes = append(p.Nodes, plan.Node{ID: "limited", Op: "limit", Limit: &plan.Limit{Input: "root", Count: 1}})
	p.Root = "limited"

	c := RelationalCompiler{
		CompilerID: plugin.CompilerIdentity{ID: "test.relational", Version: "1"},
		Expression: expressionTestDialect{},
		Result:     fixtureResultDialect{},
	}
	report, err := c.Check(relationTestRequest(p))
	if err != nil {
		t.Fatal(err)
	}
	if report.Supported || len(report.Diagnostics) != 1 {
		t.Fatalf("unexpected support report: %#v", report)
	}
	diagnostic := report.Diagnostics[0]
	if diagnostic.Code != "unsupported_plan_node" || diagnostic.NodeID != "limited" || diagnostic.Operation != "limit" {
		t.Fatalf("unexpected diagnostic: %#v", diagnostic)
	}
}
