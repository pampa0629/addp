package plan_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/query/plan"
)

func TestResultRequestPreservesPlanAndAssertions(t *testing.T) {
	p := fixture()
	p.Nodes = append(p.Nodes, plan.Node{ID: "result_0", Op: "filter", Filter: &plan.Filter{Input: p.Root, Predicate: op("is_null", column(p.Root, "value"))}})
	p.Assertions = []plan.Assertion{{Violation: "result_0", Code: "required_value"}}
	before, err := plan.CanonicalJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	req := plan.ResultRequest{Select: []string{"value"}, Limit: 10, After: []plan.Literal{{Type: datatype.FieldTypeString, Text: "sensitive cursor"}}, Filter: &plan.ResultFilter{Field: "value", Op: "gt", Values: []plan.Literal{{Type: datatype.FieldTypeDecimal, Text: "0.123456789012345678"}}}}
	r, err := plan.ApplyResultRequest(p, req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Plan.Assertions, p.Assertions) || !reflect.DeepEqual(r.HiddenFields, []string{"id"}) || r.OrderBy[0].Name != "id" || len(r.Values) != 2 {
		t.Fatalf("wrong result: %#v", r)
	}
	for _, n := range r.Plan.Nodes {
		if n.Limit != nil && n.Limit.Count != 11 {
			t.Fatal("missing lookahead row")
		}
	}
	after, _ := plan.CanonicalJSON(p)
	if string(before) != string(after) {
		t.Fatal("modified frozen plan")
	}
	wrapped, _ := plan.CanonicalJSON(r.Plan)
	if strings.Contains(string(wrapped), "sensitive cursor") || strings.Contains(string(wrapped), "0.123456789012345678") {
		t.Fatal("runtime values embedded in plan")
	}
	for i := range r.Plan.Nodes {
		if r.Plan.Nodes[i].Scan != nil {
			r.Plan.Nodes[i].Scan.Fields[0].Name = "changed"
		}
	}
	r.Plan.Assertions[0].Code = "changed"
	r.Plan.Output.StableKey[0] = "changed"
	r.SelectedFields[0] = "changed"
	if p.Nodes[0].Scan.Fields[0].Name != "id" || p.Assertions[0].Code != "required_value" || p.Output.StableKey[0] != "id" || req.Select[0] != "value" {
		t.Fatal("mutable alias retained")
	}
}

func TestResultRequestValidation(t *testing.T) {
	s := func(text string) plan.Literal { return plan.Literal{Type: datatype.FieldTypeString, Text: text} }
	cases := map[string]plan.ResultRequest{
		"zero limit": {}, "limit overflow": {Limit: plan.MaxLimit},
		"unknown field":       {Limit: 1, Select: []string{"unknown"}},
		"duplicate selection": {Limit: 1, Select: []string{"id", "id"}},
		"nullable order":      {Limit: 1, OrderBy: []plan.SortKey{{Name: "value", Direction: "asc"}}},
		"unknown order":       {Limit: 1, OrderBy: []plan.SortKey{{Name: "absent", Direction: "asc"}}},
		"invalid direction":   {Limit: 1, OrderBy: []plan.SortKey{{Name: "id", Direction: "random"}}},
		"duplicate order":     {Limit: 1, OrderBy: []plan.SortKey{{Name: "id", Direction: "asc"}, {Name: "id", Direction: "desc"}}},
		"invalid nulls":       {Limit: 1, OrderBy: []plan.SortKey{{Name: "id", Direction: "asc", Nulls: "native"}}},
		"cursor size":         {Limit: 1, After: []plan.Literal{s("a"), s("b")}},
		"cursor type":         {Limit: 1, After: []plan.Literal{{Type: datatype.FieldTypeInt, Text: "1"}}},
		"cursor null":         {Limit: 1, After: []plan.Literal{{Type: datatype.FieldTypeString, Null: true}}},
	}
	for name, f := range map[string]plan.ResultFilter{
		"unknown filter":       {Op: "like", Field: "id", Values: []plan.Literal{s("a%")}},
		"empty in":             {Op: "in", Field: "id"},
		"null comparison":      {Op: "eq", Field: "id", Values: []plan.Literal{{Type: datatype.FieldTypeString, Null: true}}},
		"wrong type":           {Op: "eq", Field: "value", Values: []plan.Literal{s("1")}},
		"mixed payload":        {Op: "and", Field: "id", Children: []plan.ResultFilter{{Op: "is_null", Field: "value"}}},
		"empty boolean":        {Op: "or"},
		"mixed leaf":           {Op: "is_null", Field: "value", Children: []plan.ResultFilter{{Op: "is_null", Field: "value"}}},
		"null values":          {Op: "is_null", Field: "value", Values: []plan.Literal{s("a")}},
		"multiple comparison":  {Op: "eq", Field: "id", Values: []plan.Literal{s("a"), s("b")}},
		"unknown filter field": {Op: "is_null", Field: "absent"},
	} {
		cases[name] = plan.ResultRequest{Limit: 1, Filter: &f}
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := plan.ApplyResultRequest(fixture(), req); err == nil {
				t.Fatal("invalid result request accepted")
			}
		})
	}
	cyclic := plan.ResultFilter{Op: "not", Children: make([]plan.ResultFilter, 1)}
	cyclic.Children[0] = cyclic
	if _, err := plan.ApplyResultRequest(fixture(), plan.ResultRequest{Limit: 1, Filter: &cyclic}); err == nil {
		t.Fatal("cyclic filter accepted")
	}
}

func TestResultRequestTypedParametersAndStableShape(t *testing.T) {
	request := plan.ResultRequest{Limit: 3, Filter: &plan.ResultFilter{Op: "and", Children: []plan.ResultFilter{
		{Op: "not", Children: []plan.ResultFilter{{Op: "is_null", Field: "value"}}},
		{Op: "in", Field: "id", Values: []plan.Literal{{Type: datatype.FieldTypeString, Text: "x"}, {Type: datatype.FieldTypeString, Text: "y"}}},
	}}}
	one, err := plan.ApplyResultRequest(fixture(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Filter.Children[1].Values[0].Text = "different"
	two, err := plan.ApplyResultRequest(fixture(), request)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := plan.Fingerprint(one.Plan)
	b, _ := plan.Fingerprint(two.Plan)
	if a != b || reflect.DeepEqual(one.Values, two.Values) {
		t.Fatal("runtime values changed plan shape or bindings were lost")
	}
}

func TestResultRequestKeepsOwnerParametersAndEnforcesSharedBudget(t *testing.T) {
	p := fixture()
	p.Parameters = []plan.Parameter{{Name: "result_0", Type: datatype.FieldTypeString, Required: true}}
	p.Nodes = append(p.Nodes, plan.Node{ID: "owner_filter", Op: "filter", Filter: &plan.Filter{Input: p.Root, Predicate: op("eq", column(p.Root, "id"), plan.Expr{Op: "parameter", Parameter: "result_0"})}})
	p.Root = "owner_filter"
	values := make([]plan.Literal, plan.MaxParameters-1)
	for i := range values {
		values[i] = plan.Literal{Type: datatype.FieldTypeString, Text: "a"}
	}
	request := plan.ResultRequest{Limit: 1, Filter: &plan.ResultFilter{Op: "in", Field: "id", Values: values}}
	r, err := plan.ApplyResultRequest(p, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, replaced := r.Values["result_0"]; replaced || !reflect.DeepEqual(r.Plan.Parameters[0], p.Parameters[0]) {
		t.Fatal("owner parameter overwritten")
	}
	request.After = []plan.Literal{{Type: datatype.FieldTypeString, Text: "a"}}
	if _, err := plan.ApplyResultRequest(p, request); err == nil {
		t.Fatal("cursor bypassed shared parameter budget")
	}
}

func TestResultContainsBindsLiteralAndPreservesSource(t *testing.T) {
	p := fixture()
	before, _ := plan.CanonicalJSON(p)
	r, err := plan.ApplyResultRequest(p, plan.ResultRequest{Limit: 2, Filter: &plan.ResultFilter{Field: "id", Op: "contains", Values: []plan.Literal{{Type: datatype.FieldTypeString, Text: "literal%_"}}}})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := plan.CanonicalJSON(p)
	encoded, _ := plan.CanonicalJSON(r.Plan)
	if string(before) != string(after) || strings.Contains(string(encoded), "literal%_") || len(r.Values) != 1 {
		t.Fatal("source mutated or literal embedded")
	}
}
