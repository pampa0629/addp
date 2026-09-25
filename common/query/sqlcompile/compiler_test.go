package sqlcompile

import (
	"errors"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

type schemaRejectingScanDialect struct{}

func (schemaRejectingScanDialect) ValidateSchema([]datatype.FieldInfo) error {
	return plugin.ErrAnalyticalUnsupported
}
func (schemaRejectingScanDialect) Table(plugin.SourceBinding) (string, error) {
	return "", plugin.ErrAnalyticalUnsupported
}
func (schemaRejectingScanDialect) Column(plugin.ColumnBinding, string) (CheckedExpression, error) {
	return CheckedExpression{}, plugin.ErrAnalyticalUnsupported
}

func TestRelationalCompilerCheckAttributesSchemaRejectionDeterministically(t *testing.T) {
	p := resultPlan()
	c := RelationalCompiler{
		CompilerID: plugin.CompilerIdentity{ID: "test.relational", Version: "1"},
		Expression: expressionTestDialect{},
		Result:     fixtureResultDialect{},
		Scan:       schemaRejectingScanDialect{},
	}
	for i := 0; i < 2; i++ {
		report, err := c.Check(relationTestRequest(p))
		if err != nil || report.Supported || len(report.Diagnostics) != 1 {
			t.Fatalf("support=%#v err=%v", report, err)
		}
		diagnostic := report.Diagnostics[0]
		if diagnostic.Code != "unsupported_plan_node" || diagnostic.NodeID != "bad" || diagnostic.Operation != "constant_rows" {
			t.Fatalf("unexpected diagnostic: %#v", diagnostic)
		}
		if _, err := c.Compile(relationTestRequest(p)); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
			t.Fatalf("compile accepted unsupported schema: %v", err)
		}
		p.Nodes[0], p.Nodes[1] = p.Nodes[1], p.Nodes[0]
	}
}

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
