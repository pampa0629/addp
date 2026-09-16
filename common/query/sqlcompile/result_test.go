package sqlcompile

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

type fixtureResultDialect struct{ reject bool }

func (fixtureResultDialect) QuoteIdentifier(s string) string { return "[" + s + "]" }
func (d fixtureResultDialect) TypedNull(datatype.FieldInfo) (string, error) {
	if d.reject {
		return "", plugin.ErrAnalyticalUnsupported
	}
	return "CAST(NULL AS BIGINT)", nil
}
func (fixtureResultDialect) OrderTerms(s string, _ datatype.FieldInfo, k plan.SortKey) ([]string, error) {
	return []string{s + " " + strings.ToUpper(k.Direction)}, nil
}

func resultPlan() plan.Plan {
	fields := []datatype.FieldInfo{{Name: "__addp_record", Type: datatype.FieldTypeBigInt}}
	return plan.Plan{SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile, Root: "root",
		Output: plan.OutputContract{Fields: fields, StableKey: []string{"__addp_record"}},
		Nodes: []plan.Node{{ID: "root", Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: fields}},
			{ID: "bad", Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: fields}}},
		Assertions: []plan.Assertion{{Violation: "bad", Code: "invalid"}},
	}
}

func TestResultEnvelopePreservesIndependentChecks(t *testing.T) {
	p := resultPlan()
	query, err := RenderResult(p, map[plan.NodeID]string{"root": "rows", "bad": "violations"}, []plan.SortKey{{Name: "__addp_record", Direction: "desc", Nulls: "last"}}, fixtureResultDialect{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"CAST(NULL AS BIGINT) AS [__addp_record]", "FROM [rows] UNION ALL", "EXISTS (SELECT 1 FROM [violations])", "THEN 'fail:1' ELSE 'ok:1'", "'complete' AS [__addp_record_0]", "[__addp_result].[__addp_record] DESC"} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("missing %q: %s", fragment, query)
		}
	}
	if strings.Contains(query, "LIMIT") {
		t.Fatal("result envelope must not paginate checks")
	}
}

func TestResultEnvelopeRejectsUnresolvedOrUnsupportedInputs(t *testing.T) {
	p := resultPlan()
	for _, relations := range []map[plan.NodeID]string{{"root": "rows"}, {"root": "same", "bad": "same"}, {"root": "rows", "bad": "bad; DROP TABLE"}} {
		if _, err := RenderResult(p, relations, nil, fixtureResultDialect{}, nil); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
			t.Fatal(err)
		}
	}
	relations := map[plan.NodeID]string{"root": "rows", "bad": "violations"}
	if _, err := RenderResult(p, relations, nil, fixtureResultDialect{reject: true}, nil); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
	if _, err := RenderResult(p, relations, []plan.SortKey{{Name: "missing", Direction: "asc", Nulls: "last"}}, fixtureResultDialect{}, nil); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
}
