package conformance

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// PreparedResultRequests exercises result composition through the same native
// compiler and PreparedQuery boundary as all other analytical operations.
func PreparedResultRequests(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo) {
	t.Helper()
	engineID := uint(time.Now().UnixNano())
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
	v := func(typ datatype.FieldType, text string) plan.Literal { return plan.Literal{Type: typ, Text: text} }
	integer := func(text string) plan.Literal { return v(datatype.FieldTypeInt, text) }
	decimal := func(text string) plan.Literal { return v(datatype.FieldTypeDecimal, text) }
	text := func(text string) plan.Literal { return v(datatype.FieldTypeString, text) }
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}, {Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 38, Scale: 18}, {Name: "label", Type: datatype.FieldTypeString, Nullable: true}}
	base := plan.Plan{SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile, Root: "rows", Output: plan.OutputContract{Fields: fields, StableKey: []string{"id"}}, Nodes: []plan.Node{{ID: "rows", Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: fields, Rows: [][]plan.Literal{
		{integer("1"), decimal("0.123456789012345678"), text("A")},
		{integer("2"), decimal("0.123456789012345678"), text("a")},
		{integer("3"), decimal("0.123456789012345677"), text("a ")},
		{integer("4"), decimal("0.123456789012345676"), {Type: datatype.FieldTypeString, Null: true}},
	}}}}}
	for _, tc := range []struct {
		name    string
		request plan.ResultRequest
		ids     []int64
	}{
		{"lookahead", plan.ResultRequest{Select: []string{"label"}, Limit: 1}, []int64{1, 2}},
		{"mixed order tie continuation", plan.ResultRequest{Select: []string{"label"}, Limit: 1, OrderBy: []plan.SortKey{{Name: "amount", Direction: "desc"}}, After: []plan.Literal{decimal("0.123456789012345678"), integer("1")}}, []int64{2, 3}},
		{"next decimal continuation", plan.ResultRequest{Select: []string{"label"}, Limit: 1, OrderBy: []plan.SortKey{{Name: "amount", Direction: "desc"}}, After: []plan.Literal{decimal("0.123456789012345678"), integer("2")}}, []int64{3, 4}},
		{"exact decimal filter", plan.ResultRequest{Limit: 9, Filter: &plan.ResultFilter{Field: "amount", Op: "gt", Values: []plan.Literal{decimal("0.123456789012345677")}}}, []int64{1, 2}},
		{"exact text IN", plan.ResultRequest{Limit: 9, Filter: &plan.ResultFilter{Field: "label", Op: "in", Values: []plan.Literal{text("A"), text("a ")}}}, []int64{1, 3}},
		{"contains text", plan.ResultRequest{Limit: 9, Filter: &plan.ResultFilter{Field: "label", Op: "contains", Values: []plan.Literal{text("a")}}}, []int64{2, 3}},
		{"contains continuation", plan.ResultRequest{Limit: 1, After: []plan.Literal{integer("2")}, Filter: &plan.ResultFilter{Field: "label", Op: "contains", Values: []plan.Literal{text("a")}}}, []int64{3}},
		{"contains empty excludes NULL", plan.ResultRequest{Limit: 9, Filter: &plan.ResultFilter{Field: "label", Op: "contains", Values: []plan.Literal{text("")}}}, []int64{1, 2, 3}},
		{"null filter", plan.ResultRequest{Limit: 9, Filter: &plan.ResultFilter{Field: "label", Op: "is_null"}}, []int64{4}},
		{"bounded boolean filter", plan.ResultRequest{Limit: 9, Filter: &plan.ResultFilter{Op: "and", Children: []plan.ResultFilter{{Op: "not_null", Field: "label"}, {Op: "not", Children: []plan.ResultFilter{{Field: "id", Op: "in", Values: []plan.Literal{integer("1"), integer("3")}}}}}}}, []int64{2}},
		{"empty page", plan.ResultRequest{Limit: 1, After: []plan.Literal{integer("4")}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped, err := plan.ApplyResultRequest(base, tc.request)
			if err != nil {
				t.Fatal(err)
			}
			out, err := executeResultRequest(t, provider, conn, engineID, wrapped)
			if err != nil {
				t.Fatal(err)
			}
			var ids []int64
			for _, row := range out.Rows {
				ids = append(ids, row["id"].(int64))
			}
			if !reflect.DeepEqual(ids, tc.ids) {
				t.Fatalf("ids=%v want=%v", ids, tc.ids)
			}
			for _, row := range out.Rows {
				if len(row) != len(wrapped.Plan.Output.Fields) {
					t.Fatalf("projection leaked columns: %#v", row)
				}
			}
		})
	}
	// Filtering aggregated output must not change the aggregation's input set.
	t.Run("aggregation before result filter", func(t *testing.T) {
		p := base
		p.Nodes = append(append([]plan.Node(nil), base.Nodes...), plan.Node{ID: "counts", Op: "aggregate", Aggregate: &plan.Aggregate{Input: "rows", Measures: []plan.Measure{{Name: "count", Op: "count_rows"}}}})
		p.Root = "counts"
		p.Output = plan.OutputContract{Fields: []datatype.FieldInfo{{Name: "count", Type: datatype.FieldTypeBigInt}}, StableKey: []string{"count"}}
		wrapped, err := plan.ApplyResultRequest(p, plan.ResultRequest{Limit: 1, Filter: &plan.ResultFilter{Op: "eq", Field: "count", Values: []plan.Literal{v(datatype.FieldTypeBigInt, "4")}}})
		if err != nil {
			t.Fatal(err)
		}
		out, err := executeResultRequest(t, provider, conn, engineID, wrapped)
		if err != nil || out == nil || len(out.Rows) != 1 || out.Rows[0]["count"] != int64(4) {
			t.Fatalf("output=%#v error=%v", out, err)
		}
	})
	// Reuse every known failure fixture, then remove all visible rows or limit
	// the first page. Neither operation may suppress an independent failure.
	for _, tc := range relationCases() {
		if tc.failure == "" && tc.assertionFailure == "" {
			continue
		}
		for _, empty := range []bool{false, true} {
			name := tc.name + "/limited"
			if empty {
				name = tc.name + "/filtered empty"
			}
			t.Run(name, func(t *testing.T) {
				p := plan.Plan{SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile, Nodes: tc.nodes, Root: tc.root, Output: relationOutput(t, tc), Assertions: tc.assertions}
				request := plan.ResultRequest{Limit: 1}
				if empty {
					request.Filter = &plan.ResultFilter{Op: "is_null", Field: p.Output.StableKey[0]}
				}
				wrapped, err := plan.ApplyResultRequest(p, request)
				if err != nil {
					t.Fatal(err)
				}
				out, err := executeResultRequest(t, provider, conn, engineID, wrapped)
				if tc.failure != "" {
					var failure *plugin.AnalyticalEvaluationError
					if !errors.As(err, &failure) || failure.Check.Node != tc.failure || out != nil {
						t.Fatalf("failure lost: %#v %v", out, err)
					}
				} else {
					var failure *plugin.AnalyticalAssertionError
					if !errors.As(err, &failure) || failure.Code != tc.assertionFailure || out != nil {
						t.Fatalf("assertion lost: %#v %v", out, err)
					}
				}
			})
		}
	}
}

func executeResultRequest(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo, engineID uint, wrapped plan.ResultPlan) (*plugin.QueryResult, error) {
	t.Helper()
	r := integerRequest(engineID)
	r.Plan = wrapped.Plan
	compiled, err := provider.AnalyticalCompiler().Compile(r)
	if err != nil {
		t.Fatal(err)
	}
	req, err := compiled.QueryRequest(wrapped.Values, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.PrepareQuery(t.Context(), conn, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepared.ReadSet(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = prepared.OutputLineage(t.Context()); err != nil {
		t.Fatal(err)
	}
	return prepared.Execute(t.Context())
}
