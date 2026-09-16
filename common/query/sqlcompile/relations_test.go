package sqlcompile

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

func TestRelationsRejectUnsupportedAndUnorderedLimit(t *testing.T) {
	p := resultPlan()
	if _, err := CompileRelations(relationTestRequest(p), nil, fixtureResultDialect{}, nil); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
	p.Assertions = nil
	p.Nodes = p.Nodes[:1]
	p.Nodes = append(p.Nodes, plan.Node{ID: "limited", Op: "limit", Limit: &plan.Limit{Input: "root", Count: 1}})
	p.Root = "limited"
	if _, err := CompileRelations(relationTestRequest(p), expressionTestDialect{}, fixtureResultDialect{}, nil); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
	p.Nodes = p.Nodes[:1]
	p.Root = "root"
	p.Nodes[0] = plan.Node{ID: "root", Op: "scan", Scan: &plan.Scan{Source: "source", Fields: p.Output.Fields}}
	if _, err := CompileRelations(relationTestRequest(p), expressionTestDialect{}, fixtureResultDialect{}, nil); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
	p.Nodes[0] = plan.Node{ID: "root", Op: "date_buckets", DateBuckets: &plan.DateBuckets{Name: "date", Start: plan.Expr{Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeDate, Text: "2026-01-01"}}, End: plan.Expr{Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeDate, Text: "2026-02-01"}}, MaxMonths: 2}}
	p.Output = plan.OutputContract{Fields: []datatype.FieldInfo{{Name: "date", Type: datatype.FieldTypeDate}}, StableKey: []string{"date"}}
	if rendered, err := CompileRelations(relationTestRequest(p), expressionTestDialect{}, fixtureResultDialect{}, nil); err != nil || len(rendered.Evaluations) != 1 || rendered.Evaluations[0].Node != "root" {
		t.Fatal(err)
	}
}

func TestRelationsDeterministicSharedDAGAndInputOwnership(t *testing.T) {
	p := resultPlan()
	first, err := CompileRelations(relationTestRequest(p), expressionTestDialect{}, fixtureResultDialect{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.Nodes[0], p.Nodes[1] = p.Nodes[1], p.Nodes[0]
	second, err := CompileRelations(relationTestRequest(p), expressionTestDialect{}, fixtureResultDialect{}, nil)
	if err != nil || first.SQL != second.SQL {
		t.Fatalf("non-deterministic SQL: %v", err)
	}
	if p.Nodes[0].ID != "bad" {
		t.Fatal("compiler mutated input order")
	}
	p.Assertions = []plan.Assertion{{Violation: "root", Code: "check_root"}}
	p.Nodes = p.Nodes[1:]
	shared, err := CompileRelations(relationTestRequest(p), expressionTestDialect{}, fixtureResultDialect{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(shared.SQL, `"r1" AS (`) != 1 {
		t.Fatal("shared relation defined more than once")
	}
}

type largeLiteralDialect struct{ expressionTestDialect }

func (largeLiteralDialect) Literal(plan.Literal) (string, error) {
	return strings.Repeat("x", plan.MaxBytes/2), nil
}

func TestRelationsBoundCumulativeExpressionExpansion(t *testing.T) {
	f := datatype.FieldInfo{Name: "key", Type: datatype.FieldTypeBigInt}
	literal := plan.Expr{Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeBigInt, Text: "1"}}
	p := plan.Plan{SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile, Root: "out", Output: plan.OutputContract{Fields: []datatype.FieldInfo{f, {Name: "b", Type: f.Type}, {Name: "c", Type: f.Type}}, StableKey: []string{"key"}}, Nodes: []plan.Node{
		{ID: "seed", Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: []datatype.FieldInfo{f}}},
		{ID: "out", Op: "project", Project: &plan.Project{Input: "seed", Columns: []plan.Projection{{Name: "key", Expr: literal}, {Name: "b", Expr: literal}, {Name: "c", Expr: literal}}}},
	}}
	if _, err := CompileRelations(relationTestRequest(p), largeLiteralDialect{}, fixtureResultDialect{}, nil); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
}

func TestRelationsRejectProjectionThatDropsOrdering(t *testing.T) {
	p := resultPlan()
	p.Assertions = nil
	p.Nodes = p.Nodes[:1]
	key := p.Output.Fields[0].Name
	p.Nodes = append(p.Nodes, plan.Node{ID: "sorted", Op: "sort", Sort: &plan.Sort{Input: "root", Keys: []plan.SortKey{{Name: key, Direction: "asc", Nulls: "last"}}}}, plan.Node{ID: "projected", Op: "project", Project: &plan.Project{Input: "sorted", Columns: []plan.Projection{{Name: key, Expr: plan.Expr{Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeBigInt, Text: "1"}}}}}})
	p.Root = "projected"
	if _, err := CompileRelations(relationTestRequest(p), expressionTestDialect{}, fixtureResultDialect{}, nil); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
}

func relationTestRequest(p plan.Plan) plugin.CompileRequest {
	return plugin.CompileRequest{Plan: p, Instance: plugin.AnalyticalInstance{EngineID: 1, Capability: plugin.AnalyticalCapability{Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile}}}}
}
