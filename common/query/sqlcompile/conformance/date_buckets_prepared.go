package conformance

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/query/sqlcompile"
)

// PreparedDateBuckets checks the same range contract through both engines'
// normal prepare/read-set/lineage/execute route, including hidden error rows.
func PreparedDateBuckets(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo) {
	t.Helper()
	engineID := uint(time.Now().UnixNano())
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
	lit := func(typ datatype.FieldType, text string) plan.Expr {
		return plan.Expr{Op: "literal", Literal: &plan.Literal{Type: typ, Text: text}}
	}
	op := func(name string, args ...plan.Expr) plan.Expr { return plan.Expr{Op: name, Args: args} }
	type example struct {
		name, start, end, first string
		max, count              int
		invalid                 bool
		startExpr, endExpr      *plan.Expr
	}
	cases := []example{
		{name: "midmonth", start: "2026-01-15", end: "2026-03-15", first: "2026-01-01", max: 120, count: 3},
		{name: "exclusive month start", start: "2026-01-15", end: "2026-03-01", first: "2026-01-01", max: 120, count: 2},
		{name: "single month", start: "2026-01-15", end: "2026-01-16", first: "2026-01-01", max: 1, count: 1},
		{name: "cross year", start: "2025-12-31", end: "2026-02-02", first: "2025-12-01", max: 3, count: 3},
		{name: "leap February", start: "2024-02-29", end: "2024-03-01", first: "2024-02-01", max: 1, count: 1},
		{name: "nonleap century", start: "1900-02-28", end: "1900-03-02", first: "1900-02-01", max: 2, count: 2},
		{name: "minimum date", start: "0001-01-01", end: "0001-01-02", first: "0001-01-01", max: 120, count: 1},
		{name: "maximum date unused offsets", start: "9999-12-30", end: "9999-12-31", first: "9999-12-01", max: 120, count: 1},
		{name: "maximum date two months", start: "9999-11-30", end: "9999-12-31", first: "9999-11-01", max: 120, count: 2},
		{name: "exactly 120 months", start: "2020-01-15", end: "2030-01-01", first: "2020-01-01", max: 120, count: 120},
		{name: "partial 120th month", start: "2020-01-31", end: "2029-12-02", first: "2020-01-01", max: 120, count: 120},
		{name: "121 months", start: "2020-01-15", end: "2030-01-02", max: 120, invalid: true},
		{name: "smaller node bound", start: "2026-01-31", end: "2026-02-02", max: 1, invalid: true},
		{name: "empty interval", start: "2026-01-01", end: "2026-01-01", max: 120, invalid: true},
		{name: "reversed same month", start: "2026-01-31", end: "2026-01-01", max: 120, invalid: true},
		{name: "reversed minimum end", start: "2026-01-31", end: "0001-01-01", max: 120, invalid: true},
		{name: "full calendar too large", start: "0001-01-01", end: "9999-12-31", max: 120, invalid: true},
	}
	badDate := op("date", lit(datatype.FieldTypeString, "2026-02-30"))
	validDate := lit(datatype.FieldTypeDate, "2026-01-15")
	for _, expression := range []struct {
		name    string
		value   plan.Expr
		invalid bool
	}{
		{"computed month shift", op("add_months", lit(datatype.FieldTypeDate, "2025-12-15"), lit(datatype.FieldTypeInt, "1")), false},
		{"invalid parsed start", badDate, true},
		{"coalesce retains error", op("coalesce", badDate, validDate), true},
		{"case skips error", op("case", lit(datatype.FieldTypeBool, "false"), badDate, validDate), false},
	} {
		x := expression.value
		cases = append(cases, example{name: expression.name, start: "2026-01-15", end: "2026-03-01", first: "2026-01-01", max: 120, count: 2, invalid: expression.invalid, startExpr: &x})
	}
	cases = append(cases, example{name: "invalid parsed end", start: "2026-01-15", end: "2026-03-01", max: 120, invalid: true, endExpr: &badDate})
	for _, tc := range cases {
		for _, parameterized := range []bool{false, true} {
			if parameterized && (tc.startExpr != nil || tc.endExpr != nil) {
				continue
			}
			for _, mode := range []string{"all", "empty", "limit"} {
				t.Run(fmt.Sprintf("%s/parameters=%t/%s", tc.name, parameterized, mode), func(t *testing.T) {
					r := integerRequest(engineID)
					r.Plan.Parameters = nil
					values := map[string]plan.Literal{}
					start, end := lit(datatype.FieldTypeDate, tc.start), lit(datatype.FieldTypeDate, tc.end)
					if parameterized {
						for _, bound := range []struct{ name, value string }{{"start", tc.start}, {"end", tc.end}} {
							r.Plan.Parameters = append(r.Plan.Parameters, plan.Parameter{Name: bound.name, Type: datatype.FieldTypeDate, Required: true})
							values[bound.name] = plan.Literal{Type: datatype.FieldTypeDate, Text: bound.value}
						}
						start, end = plan.Expr{Op: "parameter", Parameter: "start"}, plan.Expr{Op: "parameter", Parameter: "end"}
					}
					if tc.startExpr != nil {
						start = *tc.startExpr
					}
					if tc.endExpr != nil {
						end = *tc.endExpr
					}
					r.Plan.Nodes = []plan.Node{{ID: "months", Op: "date_buckets", DateBuckets: &plan.DateBuckets{Start: start, End: end, Name: "bucket", MaxMonths: tc.max}}}
					r.Plan.Root = "months"
					if mode == "empty" {
						r.Plan.Nodes = append(r.Plan.Nodes, plan.Node{ID: "filtered", Op: "filter", Filter: &plan.Filter{Input: "months", Predicate: lit(datatype.FieldTypeBool, "false")}})
						r.Plan.Root = "filtered"
					} else if mode == "limit" {
						r.Plan.Nodes = append(r.Plan.Nodes, plan.Node{ID: "sorted", Op: "sort", Sort: &plan.Sort{Input: "months", Keys: []plan.SortKey{{Name: "bucket", Direction: "asc", Nulls: "last"}}}}, plan.Node{ID: "limited", Op: "limit", Limit: &plan.Limit{Input: "sorted", Count: 1}})
						r.Plan.Root = "limited"
					}
					r.Plan.Output = plan.OutputContract{Fields: []datatype.FieldInfo{{Name: "bucket", Type: datatype.FieldTypeDate}}, StableKey: []string{"bucket"}}
					compiled, err := provider.AnalyticalCompiler().Compile(r)
					if err != nil {
						t.Fatal(err)
					}
					for i, j := 0, len(r.Plan.Nodes)-1; i < j; i, j = i+1, j-1 {
						r.Plan.Nodes[i], r.Plan.Nodes[j] = r.Plan.Nodes[j], r.Plan.Nodes[i]
					}
					again, err := provider.AnalyticalCompiler().Compile(r)
					if err != nil || again.Template() != compiled.Template() || again.Fingerprint() != compiled.Fingerprint() {
						t.Fatalf("unstable compilation: %v", err)
					}
					req, err := compiled.QueryRequest(values, 10*time.Second)
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
					out, err := prepared.Execute(t.Context())
					if tc.invalid {
						var failure *plugin.AnalyticalEvaluationError
						if !errors.As(err, &failure) || failure.Check.Node != "months" || failure.Check.Code != sqlcompile.EvaluationFailureCode || out != nil {
							t.Fatalf("wrong failure: %#v %v", out, err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					count := tc.count
					if mode == "empty" {
						count = 0
					} else if mode == "limit" {
						count = 1
					}
					if len(out.Rows) != count {
						t.Fatalf("rows=%#v want count=%d", out.Rows, count)
					}
					first, err := time.Parse("2006-01-02", tc.first)
					if err != nil {
						t.Fatal(err)
					}
					for i, row := range out.Rows {
						want := first.AddDate(0, i, 0).Format("2006-01-02")
						if len(row) != 1 || row["bucket"] != want {
							t.Fatalf("row=%#v want=%s", row, want)
						}
					}
				})
			}
		}
	}
}
