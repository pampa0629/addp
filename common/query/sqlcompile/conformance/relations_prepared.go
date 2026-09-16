package conformance

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/query/sqlcompile"
)

type relationExample struct {
	name             string
	nodes            []plan.Node
	root             plan.NodeID
	key              []string
	want             []map[string]any
	failure          plan.NodeID
	assertions       []plan.Assertion
	assertionFailure string
}

func relationCases() []relationExample {
	col := func(id plan.NodeID, name string) plan.Expr {
		return plan.Expr{Op: "column", Column: &plan.ColumnRef{Input: id, Name: name}}
	}
	lit := func(typ datatype.FieldType, s string) plan.Expr {
		return plan.Expr{Op: "literal", Literal: &plan.Literal{Type: typ, Text: s}}
	}
	op := func(name string, args ...plan.Expr) plan.Expr { return plan.Expr{Op: name, Args: args} }
	field := func(name string, typ datatype.FieldType, nullable bool) datatype.FieldInfo {
		return datatype.FieldInfo{Name: name, Type: typ, Nullable: nullable}
	}
	integer := func(s string) plan.Literal { return plan.Literal{Type: datatype.FieldTypeInt, Text: s} }
	text := func(s string) plan.Literal { return plan.Literal{Type: datatype.FieldTypeString, Text: s} }
	null := plan.Literal{Type: datatype.FieldTypeString, Null: true}
	rows := func(id plan.NodeID, fields []datatype.FieldInfo, data ...[]plan.Literal) plan.Node {
		return plan.Node{ID: id, Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: fields, Rows: data}}
	}
	project := func(id, input plan.NodeID, columns ...plan.Projection) plan.Node {
		return plan.Node{ID: id, Op: "project", Project: &plan.Project{Input: input, Columns: columns}}
	}
	count := func(id, input plan.NodeID) plan.Node {
		return plan.Node{ID: id, Op: "aggregate", Aggregate: &plan.Aggregate{Input: input, Measures: []plan.Measure{{Name: "count", Op: "count_rows"}}}}
	}
	source := rows("source", []datatype.FieldInfo{field("id", datatype.FieldTypeInt, false), field("label", datatype.FieldTypeString, true)}, []plan.Literal{integer("1"), text("A")}, []plan.Literal{integer("2"), text("a")}, []plan.Literal{integer("3"), text("b")}, []plan.Literal{integer("4"), null})
	sorted := plan.Node{ID: "sorted", Op: "sort", Sort: &plan.Sort{Input: "source", Keys: []plan.SortKey{{Name: "label", Direction: "desc", Nulls: "first"}, {Name: "id", Direction: "asc", Nulls: "last"}}}}
	projected := project("renamed", "sorted", plan.Projection{Name: "key", Expr: col("sorted", "id")}, plan.Projection{Name: "value", Expr: col("sorted", "label")})
	filtered := plan.Node{ID: "filtered", Op: "filter", Filter: &plan.Filter{Input: "renamed", Predicate: op("gt", col("renamed", "key"), lit(datatype.FieldTypeInt, "1"))}}
	limited := plan.Node{ID: "limited", Op: "limit", Limit: &plan.Limit{Input: "filtered", Count: 2}}
	cases := []relationExample{{name: "sort survives rename filter and limit", nodes: []plan.Node{limited, filtered, projected, sorted, source}, root: "limited", key: []string{"key"}, want: []map[string]any{{"key": int64(4), "value": nil}, {"key": int64(3), "value": "b"}}}}
	left := rows("left", []datatype.FieldInfo{field("lid", datatype.FieldTypeInt, false)}, []plan.Literal{integer("1")}, []plan.Literal{integer("2")})
	right := rows("right", []datatype.FieldInfo{field("rid", datatype.FieldTypeInt, false), field("fk", datatype.FieldTypeInt, false)}, []plan.Literal{integer("10"), integer("1")})
	on := op("eq", col("left", "lid"), col("right", "fk"))
	for _, kind := range []string{"inner", "left", "cross"} {
		join := plan.Node{ID: "joined", Op: "join", Join: &plan.Join{Left: "left", Right: "right", Kind: kind, On: &on}}
		expected := []map[string]any{{"lid": int64(1), "rid": int64(10), "fk": int64(1)}}
		if kind == "left" {
			expected = append(expected, map[string]any{"lid": int64(2), "rid": nil, "fk": nil})
		}
		if kind == "cross" {
			join.Join.On = nil
			expected = append(expected, map[string]any{"lid": int64(2), "rid": int64(10), "fk": int64(1)})
		}
		cases = append(cases, relationExample{name: kind + " join", nodes: []plan.Node{left, right, join}, root: "joined", key: []string{"lid"}, want: expected})
	}
	other := rows("other", []datatype.FieldInfo{field("other_id", datatype.FieldTypeInt, false), field("other_label", datatype.FieldTypeString, true)}, []plan.Literal{integer("5"), text("other")})
	union := plan.Node{ID: "union", Op: "union_all", UnionAll: &plan.UnionAll{Inputs: []plan.NodeID{"source", "other", "source"}}}
	cases = append(cases, relationExample{name: "union by position preserves duplicates", nodes: []plan.Node{source, other, union, count("counted", "union")}, root: "counted", key: []string{"count"}, want: []map[string]any{{"count": int64(9)}}})
	labels := rows("labels", []datatype.FieldInfo{field("label", datatype.FieldTypeString, true)}, []plan.Literal{text("A")}, []plan.Literal{text("a")}, []plan.Literal{text("a ")}, []plan.Literal{null}, []plan.Literal{null})
	distinct := plan.Node{ID: "unique", Op: "distinct", Distinct: &plan.Unary{Input: "labels"}}
	cases = append(cases, relationExample{name: "distinct exact text and NULL", nodes: []plan.Node{labels, distinct, count("counted", "unique")}, root: "counted", key: []string{"count"}, want: []map[string]any{{"count": int64(4)}}})
	groupedSource := rows("groups", []datatype.FieldInfo{field("key", datatype.FieldTypeString, false), field("value", datatype.FieldTypeString, true)}, []plan.Literal{text("A"), null}, []plan.Literal{text("a"), text("x")}, []plan.Literal{text("a"), null}, []plan.Literal{text("a "), text("x")})
	measure := col("groups", "value")
	grouped := plan.Node{ID: "grouped", Op: "aggregate", Aggregate: &plan.Aggregate{Input: "groups", Groups: []plan.Projection{{Name: "key", Expr: col("groups", "key")}}, Measures: []plan.Measure{{Name: "rows", Op: "count_rows"}, {Name: "values", Op: "count_value", Value: &measure}}}}
	cases = append(cases, relationExample{name: "group exact text and count NULL", nodes: []plan.Node{groupedSource, grouped}, root: "grouped", key: []string{"key"}, want: []map[string]any{{"key": "A", "rows": int64(1), "values": int64(0)}, {"key": "a", "rows": int64(2), "values": int64(1)}, {"key": "a ", "rows": int64(1), "values": int64(1)}}})
	empty := rows("empty", []datatype.FieldInfo{field("id", datatype.FieldTypeInt, false)})
	cases = append(cases, relationExample{name: "global count empty relation", nodes: []plan.Node{empty, count("counted", "empty")}, root: "counted", key: []string{"count"}, want: []map[string]any{{"count": int64(0)}}})
	nullGrouped := plan.Node{ID: "null_group", Op: "aggregate", Aggregate: &plan.Aggregate{Input: "labels", Groups: []plan.Projection{{Name: "key", Expr: col("labels", "label")}}, Measures: []plan.Measure{{Name: "rows", Op: "count_rows"}}}}
	cases = append(cases, relationExample{name: "group NULL as one group", nodes: []plan.Node{labels, nullGrouped, count("counted", "null_group")}, root: "counted", key: []string{"count"}, want: []map[string]any{{"count": int64(4)}}})
	unknown := plan.Node{ID: "unknown", Op: "filter", Filter: &plan.Filter{Input: "source", Predicate: op("eq", col("source", "label"), plan.Expr{Op: "literal", Literal: &null})}}
	cases = append(cases, relationExample{name: "filter unknown is not true", nodes: []plan.Node{source, unknown}, root: "unknown", key: []string{"id"}})
	bad := op("integer", lit(datatype.FieldTypeDecimal, "1.5"))
	badPredicate := op("and", lit(datatype.FieldTypeBool, "false"), op("is_null", bad))
	errorFilter := plan.Node{ID: "error_filter", Op: "filter", Filter: &plan.Filter{Input: "source", Predicate: badPredicate}}
	cases = append(cases, relationExample{name: "filter cannot hide its own error", nodes: []plan.Node{source, errorFilter}, root: "error_filter", key: []string{"id"}, failure: "error_filter"})
	badMeasure := plan.Node{ID: "error_count", Op: "aggregate", Aggregate: &plan.Aggregate{Input: "source", Measures: []plan.Measure{{Name: "count", Op: "count_value", Value: &bad}}}}
	cases = append(cases, relationExample{name: "count cannot hide invalid NULL placeholder", nodes: []plan.Node{source, badMeasure}, root: "error_count", key: []string{"count"}, failure: "error_count"})
	badGroup := plan.Node{ID: "error_group", Op: "aggregate", Aggregate: &plan.Aggregate{Input: "source", Groups: []plan.Projection{{Name: "key", Expr: bad}}, Measures: []plan.Measure{{Name: "count", Op: "count_rows"}}}}
	cases = append(cases, relationExample{name: "group expression evaluated before aggregation", nodes: []plan.Node{source, badGroup}, root: "error_group", key: []string{"key"}, failure: "error_group"})
	for _, emptyRight := range []bool{false, true} {
		r := right
		if emptyRight {
			r = rows("right", right.ConstantRows.Fields)
		}
		badOn := op("eq", bad, op("integer", col("left", "lid")))
		join := plan.Node{ID: "error_join", Op: "join", Join: &plan.Join{Left: "left", Right: "right", Kind: "left", On: &badOn}}
		e := relationExample{name: fmt.Sprintf("join condition empty right=%t", emptyRight), nodes: []plan.Node{left, r, join}, root: "error_join", key: []string{"lid"}, failure: "error_join"}
		if emptyRight {
			e.failure = ""
			e.want = []map[string]any{{"lid": int64(1), "rid": nil, "fk": nil}, {"lid": int64(2), "rid": nil, "fk": nil}}
		}
		cases = append(cases, e)
	}
	// Assertion roots remain reachable even when the business root is empty.
	cases = append(cases, relationExample{name: "assertion independent of empty root", nodes: []plan.Node{source, unknown}, root: "unknown", key: []string{"id"}, assertions: []plan.Assertion{{Violation: "source", Code: "source_must_be_empty"}}, assertionFailure: "source_must_be_empty"})

	distinctOutput := project("distinct_output", "unique", plan.Projection{Name: "key", Expr: op("coalesce", col("unique", "label"), lit(datatype.FieldTypeString, "~NULL"))}, plan.Projection{Name: "value", Expr: col("unique", "label")})
	cases = append(cases, relationExample{name: "distinct restores original text and NULL", nodes: []plan.Node{labels, distinct, distinctOutput}, root: "distinct_output", key: []string{"key"}, want: []map[string]any{{"key": "A", "value": "A"}, {"key": "a", "value": "a"}, {"key": "a ", "value": "a "}, {"key": "~NULL", "value": nil}}})
	errorProjection := project("error_projection", "source", plan.Projection{Name: "id", Expr: col("source", "id")}, plan.Projection{Name: "amount", Expr: op("case", op("eq", col("source", "id"), lit(datatype.FieldTypeInt, "4")), bad, op("integer", col("source", "id")))})
	errorSorted := plan.Node{ID: "error_sorted", Op: "sort", Sort: &plan.Sort{Input: "error_projection", Keys: []plan.SortKey{{Name: "id", Direction: "asc", Nulls: "last"}}}}
	errorLimited := plan.Node{ID: "error_limited", Op: "limit", Limit: &plan.Limit{Input: "error_sorted", Count: 1}}
	cases = append(cases, relationExample{name: "limit cannot skip later projection error", nodes: []plan.Node{source, errorProjection, errorSorted, errorLimited}, root: "error_limited", key: []string{"id"}, failure: "error_projection"})
	return cases
}

func PreparedRelations(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo) {
	t.Helper()
	engineID := uint(time.Now().UnixNano())
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
	for _, tc := range relationCases() {
		t.Run(tc.name, func(t *testing.T) {
			r := integerRequest(engineID)
			r.Plan.Nodes = tc.nodes
			r.Plan.Root = tc.root
			r.Plan.Parameters = nil
			r.Plan.Assertions = tc.assertions
			r.Plan.Output = relationOutput(t, tc)
			c := provider.AnalyticalCompiler()
			compiled, err := c.Compile(r)
			if err != nil {
				t.Fatal(err)
			}
			reversed := r
			reversed.Plan.Nodes = append([]plan.Node(nil), r.Plan.Nodes...)
			for i, j := 0, len(reversed.Plan.Nodes)-1; i < j; i, j = i+1, j-1 {
				reversed.Plan.Nodes[i], reversed.Plan.Nodes[j] = reversed.Plan.Nodes[j], reversed.Plan.Nodes[i]
			}
			again, err := c.Compile(reversed)
			if err != nil || compiled.Template() != again.Template() || compiled.Fingerprint() != again.Fingerprint() {
				t.Fatalf("node order changed compilation: %v", err)
			}
			req, err := compiled.QueryRequest(nil, 10*time.Second)
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
			if tc.failure != "" {
				var failure *plugin.AnalyticalEvaluationError
				if !errors.As(err, &failure) || failure.Check.Node != tc.failure || failure.Check.Code != sqlcompile.EvaluationFailureCode || out != nil {
					t.Fatalf("wrong evaluation failure: %#v %v", out, err)
				}
				return
			}
			if tc.assertionFailure != "" {
				var failure *plugin.AnalyticalAssertionError
				if !errors.As(err, &failure) || failure.Code != tc.assertionFailure || out != nil {
					t.Fatalf("wrong assertion failure: %#v %v", out, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Rows) != len(tc.want) {
				t.Fatalf("rows=%#v want=%#v", out.Rows, tc.want)
			}
			for i, row := range out.Rows {
				if !reflect.DeepEqual(row, tc.want[i]) {
					t.Fatalf("row=%#v want=%#v", row, tc.want[i])
				}
			}
		})
	}
}

// Fixture output contracts are explicit expectations, not another production
// type inference implementation. The real compiler validates them with Analyze.
func relationOutput(t *testing.T, tc relationExample) plan.OutputContract {
	t.Helper()
	f := func(name string, typ datatype.FieldType, nullable bool) datatype.FieldInfo {
		return datatype.FieldInfo{Name: name, Type: typ, Nullable: nullable}
	}
	var fields []datatype.FieldInfo
	switch tc.root {
	case "distinct_output":
		fields = []datatype.FieldInfo{f("key", datatype.FieldTypeString, false), f("value", datatype.FieldTypeString, true)}
	case "error_limited":
		fields = []datatype.FieldInfo{f("id", datatype.FieldTypeInt, false), f("amount", datatype.FieldTypeBigInt, false)}
	case "limited":
		fields = []datatype.FieldInfo{f("key", datatype.FieldTypeInt, false), f("value", datatype.FieldTypeString, true)}
	case "joined", "error_join":
		nullable := tc.name == "left join" || tc.root == "error_join"
		fields = []datatype.FieldInfo{f("lid", datatype.FieldTypeInt, false), f("rid", datatype.FieldTypeInt, nullable), f("fk", datatype.FieldTypeInt, nullable)}
	case "counted", "error_count":
		fields = []datatype.FieldInfo{f("count", datatype.FieldTypeBigInt, false)}
	case "grouped":
		fields = []datatype.FieldInfo{f("key", datatype.FieldTypeString, false), f("rows", datatype.FieldTypeBigInt, false), f("values", datatype.FieldTypeBigInt, false)}
	case "unknown", "error_filter":
		fields = []datatype.FieldInfo{f("id", datatype.FieldTypeInt, false), f("label", datatype.FieldTypeString, true)}
	case "error_group":
		fields = []datatype.FieldInfo{f("key", datatype.FieldTypeBigInt, false), f("count", datatype.FieldTypeBigInt, false)}
	default:
		t.Fatalf("missing expected output for %s", tc.name)
	}
	return plan.OutputContract{Fields: fields, StableKey: tc.key}
}
