package plan_test

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/query/plan"
)

func f(name string, t datatype.FieldType, nullable bool) datatype.FieldInfo {
	v := datatype.FieldInfo{Name: name, Type: t, Nullable: nullable}
	if t == datatype.FieldTypeDecimal {
		v.Precision = 38
		v.Scale = 18
	}
	return v
}
func column(input plan.NodeID, name string) plan.Expr {
	return plan.Expr{Op: "column", Column: &plan.ColumnRef{Input: input, Name: name}}
}
func lit(t datatype.FieldType, text string) plan.Expr {
	return plan.Expr{Op: "literal", Literal: &plan.Literal{Type: t, Text: text}}
}
func op(name string, args ...plan.Expr) plan.Expr { return plan.Expr{Op: name, Args: args} }
func fixture() plan.Plan {
	fields := []datatype.FieldInfo{f("id", datatype.FieldTypeString, false), f("value", datatype.FieldTypeDecimal, true)}
	return plan.Plan{SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile, Nodes: []plan.Node{{ID: "source", Op: "scan", Scan: &plan.Scan{Source: "items", Fields: fields}}}, Root: "source", Output: plan.OutputContract{Fields: fields, StableKey: []string{"id"}}}
}
func copyPlan(p plan.Plan) plan.Plan {
	b, _ := json.Marshal(p)
	var q plan.Plan
	_ = json.Unmarshal(b, &q)
	return q
}
func mustValid(t *testing.T, p plan.Plan) {
	t.Helper()
	if err := plan.Validate(p); err != nil {
		t.Fatal(err)
	}
}

func TestFilterNonNullInference(t *testing.T) {
	nonNull := op("not", op("is_null", column("source", "value")))
	for _, tt := range []struct {
		name      string
		predicate plan.Expr
		nullable  bool
	}{
		{"explicit", nonNull, false},
		{"conjunction", op("and", nonNull, op("eq", column("source", "id"), lit(datatype.FieldTypeString, "x"))), false},
		{"disjunction", op("or", nonNull, op("eq", column("source", "id"), lit(datatype.FieldTypeString, "x"))), true},
		{"null check", op("is_null", column("source", "value")), true},
		{"expression", op("not", op("is_null", op("coalesce", column("source", "value"), lit(datatype.FieldTypeDecimal, "0")))), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := copyPlan(fixture())
			p.Nodes = append(p.Nodes, plan.Node{ID: "filtered", Op: "filter", Filter: &plan.Filter{Input: "source", Predicate: tt.predicate}})
			p.Root = "filtered"
			p.Output.Fields[1].Nullable = tt.nullable
			if !tt.nullable {
				p.Output.StableKey = []string{"id", "value"}
			}
			schemas, err := plan.Analyze(p)
			if err != nil {
				t.Fatal(err)
			}
			if !schemas["source"][1].Nullable || !p.Nodes[0].Scan.Fields[1].Nullable {
				t.Fatal("filter changed source nullability")
			}
			if got := schemas["filtered"][1]; got.Nullable != tt.nullable || got.Precision != 38 || got.Scale != 18 {
				t.Fatalf("unexpected filtered field: %+v", got)
			}
		})
	}
}

func TestRejectInvalidPlans(t *testing.T) {
	cases := map[string]func(*plan.Plan){
		"unknown schema":           func(p *plan.Plan) { p.SchemaVersion = "future" },
		"unknown semantic profile": func(p *plan.Plan) { p.SemanticProfile = "native" },
		"unknown op":               func(p *plan.Plan) { p.Nodes[0].Op = "sql" },
		"mixed payload":            func(p *plan.Plan) { p.Nodes[0].Distinct = &plan.Unary{Input: "source"} },
		"missing root":             func(p *plan.Plan) { p.Root = "absent" },
		"duplicate ID":             func(p *plan.Plan) { p.Nodes = append(p.Nodes, p.Nodes[0]) },
		"cycle": func(p *plan.Plan) {
			p.Nodes = []plan.Node{{ID: "source", Op: "distinct", Distinct: &plan.Unary{Input: "source"}}}
		},
		"unreachable": func(p *plan.Plan) {
			p.Nodes = append(p.Nodes, plan.Node{ID: "orphan", Op: "distinct", Distinct: &plan.Unary{Input: "source"}})
		},
		"unknown column": func(p *plan.Plan) {
			p.Nodes = append(p.Nodes, plan.Node{ID: "root", Op: "project", Project: &plan.Project{Input: "source", Columns: []plan.Projection{{Name: "id", Expr: column("source", "absent")}}}})
			p.Root = "root"
		},
		"cross-scope column": func(p *plan.Plan) {
			p.Nodes = append(p.Nodes, plan.Node{ID: "root", Op: "filter", Filter: &plan.Filter{Input: "source", Predicate: op("eq", column("outside", "id"), lit(datatype.FieldTypeString, "x"))}})
			p.Root = "root"
		},
		"non-boolean predicate": func(p *plan.Plan) {
			p.Nodes = append(p.Nodes, plan.Node{ID: "root", Op: "filter", Filter: &plan.Filter{Input: "source", Predicate: column("source", "id")}})
			p.Root = "root"
		},
		"native field metadata":  func(p *plan.Plan) { p.Nodes[0].Scan.Fields[0].NativeType = "VARCHAR" },
		"generated expression":   func(p *plan.Plan) { p.Nodes[0].Scan.Fields[0].DefaultExpression = "native()" },
		"duplicate column":       func(p *plan.Plan) { p.Nodes[0].Scan.Fields[1].Name = "id" },
		"unsupported type":       func(p *plan.Plan) { p.Nodes[0].Scan.Fields[1].Type = datatype.FieldType("float") },
		"invalid decimal scale":  func(p *plan.Plan) { p.Nodes[0].Scan.Fields[1].Scale = 2 },
		"output mismatch":        func(p *plan.Plan) { p.Output.Fields[0].Nullable = true },
		"nullable key":           func(p *plan.Plan) { p.Output.StableKey = []string{"value"} },
		"no key":                 func(p *plan.Plan) { p.Output.StableKey = nil },
		"duplicate key":          func(p *plan.Plan) { p.Output.StableKey = []string{"id", "id"} },
		"missing assertion node": func(p *plan.Plan) { p.Assertions = []plan.Assertion{{Violation: "absent", Code: "invalid_source"}} },
		"unused parameter":       func(p *plan.Plan) { p.Parameters = []plan.Parameter{{Name: "unused", Type: datatype.FieldTypeString}} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := copyPlan(fixture())
			change(&p)
			if err := plan.Validate(p); err == nil {
				t.Fatal("invalid plan accepted")
			}
		})
	}
}

func TestSchemaInferenceAndAssertions(t *testing.T) {
	p := fixture()
	p.Nodes = append(p.Nodes,
		plan.Node{ID: "filtered", Op: "filter", Filter: &plan.Filter{Input: "source", Predicate: op("not", op("is_null", column("source", "value")))}},
		plan.Node{ID: "unique", Op: "distinct", Distinct: &plan.Unary{Input: "filtered"}},
		plan.Node{ID: "counts", Op: "aggregate", Aggregate: &plan.Aggregate{Input: "unique", Groups: []plan.Projection{{Name: "id", Expr: column("unique", "id")}}, Measures: []plan.Measure{{Name: "rows", Op: "count_rows"}, {Name: "values", Op: "count_value", Value: ptr(column("unique", "value"))}}}},
		plan.Node{ID: "sorted", Op: "sort", Sort: &plan.Sort{Input: "counts", Keys: []plan.SortKey{{Name: "id", Direction: "asc", Nulls: "last"}}}},
		plan.Node{ID: "root", Op: "limit", Limit: &plan.Limit{Input: "sorted", Count: 101}},
		plan.Node{ID: "violations", Op: "filter", Filter: &plan.Filter{Input: "source", Predicate: op("is_null", column("source", "value"))}},
	)
	p.Root = "root"
	p.Assertions = []plan.Assertion{{Violation: "violations", Code: "value_required"}}
	p.Output.Fields = []datatype.FieldInfo{f("id", datatype.FieldTypeString, false), f("rows", datatype.FieldTypeBigInt, false), f("values", datatype.FieldTypeBigInt, false)}
	schemas, err := plan.Analyze(p)
	if err != nil {
		t.Fatal(err)
	}
	if !schemas["source"][1].Nullable || schemas["counts"][1].Nullable {
		t.Fatal("count nullability was not derived")
	}
	p.Nodes[3].Aggregate.Measures[0].Value = ptr(column("unique", "value"))
	if plan.Validate(p) == nil {
		t.Fatal("count_rows accepted a value")
	}
}

func TestDecimalSumInference(t *testing.T) {
	p := fixture()
	p.Nodes = append(p.Nodes, plan.Node{ID: "totals", Op: "aggregate", Aggregate: &plan.Aggregate{
		Input: "source", Groups: []plan.Projection{{Name: "id", Expr: column("source", "id")}},
		Measures: []plan.Measure{{Name: "total", Op: "sum_decimal", Value: ptr(column("source", "value"))}},
	}})
	p.Root = "totals"
	p.Output.Fields = []datatype.FieldInfo{f("id", datatype.FieldTypeString, false), f("total", datatype.FieldTypeDecimal, true)}
	mustValid(t, p)
	p.Nodes[1].Aggregate.Measures[0].Value = nil
	if plan.Validate(p) == nil {
		t.Fatal("sum_decimal accepted a missing value")
	}
	p.Nodes[1].Aggregate.Measures[0].Value = ptr(column("source", "id"))
	if plan.Validate(p) == nil {
		t.Fatal("sum_decimal accepted a string")
	}
}
func ptr(e plan.Expr) *plan.Expr { return &e }

func TestJoinAndUnionNullability(t *testing.T) {
	p := fixture()
	rightFields := []datatype.FieldInfo{f("other_id", datatype.FieldTypeString, false)}
	p.Nodes = append(p.Nodes, plan.Node{ID: "other", Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: rightFields, Rows: [][]plan.Literal{{{Type: datatype.FieldTypeString, Text: "a"}}}}}, plan.Node{ID: "root", Op: "join", Join: &plan.Join{Left: "source", Right: "other", Kind: "left", On: ptr(op("eq", column("source", "id"), column("other", "other_id")))}})
	p.Root = "root"
	p.Output.Fields = append(p.Output.Fields, f("other_id", datatype.FieldTypeString, true))
	mustValid(t, p)
	p.Nodes[2].Join.Kind = "inner"
	p.Output.Fields[2].Nullable = false
	mustValid(t, p)
	p.Nodes[2].Join.Kind = "cross"
	p.Nodes[2].Join.On = nil
	mustValid(t, p)
	p.Nodes[2].Join.Kind = "right"
	if plan.Validate(p) == nil {
		t.Fatal("unsupported join accepted")
	}
	p = fixture()
	fields := append([]datatype.FieldInfo(nil), p.Output.Fields...)
	fields[1].Nullable = false
	p.Nodes = append(p.Nodes, plan.Node{ID: "other", Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: fields}}, plan.Node{ID: "root", Op: "union_all", UnionAll: &plan.UnionAll{Inputs: []plan.NodeID{"source", "other"}}})
	p.Root = "root"
	mustValid(t, p)
	p.Nodes[1].ConstantRows.Fields[1].Type = datatype.FieldTypeInt
	p.Nodes[1].ConstantRows.Fields[1].Precision = 0
	p.Nodes[1].ConstantRows.Fields[1].Scale = 0
	if plan.Validate(p) == nil {
		t.Fatal("union accepted incompatible types")
	}
}

func TestExpressionTyping(t *testing.T) {
	cases := []struct {
		name     string
		expr     plan.Expr
		typ      datatype.FieldType
		nullable bool
	}{
		{"decimal division", op("divide", lit(datatype.FieldTypeInt, "1"), lit(datatype.FieldTypeInt, "3")), datatype.FieldTypeDecimal, false},
		{"nullable arithmetic", op("add", column("source", "value"), lit(datatype.FieldTypeInt, "1")), datatype.FieldTypeDecimal, true},
		{"conditional", op("case", op("is_null", column("source", "value")), lit(datatype.FieldTypeDecimal, "0"), column("source", "value")), datatype.FieldTypeDecimal, true},
		{"coalesce", op("coalesce", column("source", "value"), lit(datatype.FieldTypeDecimal, "0")), datatype.FieldTypeDecimal, false},
		{"text", op("text", column("source", "value")), datatype.FieldTypeString, true},
		{"integer", op("integer", column("source", "value")), datatype.FieldTypeBigInt, true},
		{"decimal", op("decimal", lit(datatype.FieldTypeInt, "2")), datatype.FieldTypeDecimal, false},
		{"date", op("date", lit(datatype.FieldTypeString, "2024-02-29")), datatype.FieldTypeDate, false},
		{"month", op("add_months", op("month_start", lit(datatype.FieldTypeDate, "2024-02-29")), lit(datatype.FieldTypeInt, "1")), datatype.FieldTypeDate, false},
		{"boolean", op("and", op("eq", column("source", "id"), lit(datatype.FieldTypeString, "a")), op("gt", column("source", "value"), lit(datatype.FieldTypeDecimal, "0"))), datatype.FieldTypeBool, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := fixture()
			p.Nodes = append(p.Nodes, plan.Node{ID: "root", Op: "project", Project: &plan.Project{Input: "source", Columns: []plan.Projection{{Name: "id", Expr: column("source", "id")}, {Name: "result", Expr: c.expr}}}})
			p.Root = "root"
			p.Output.Fields = []datatype.FieldInfo{p.Output.Fields[0], f("result", c.typ, c.nullable)}
			mustValid(t, p)
			p.Nodes[1].Project.Columns[1].Expr.Op = "native_function"
			if plan.Validate(p) == nil {
				t.Fatal("native function accepted")
			}
		})
	}
}

func TestDateParametersAndAllowedValues(t *testing.T) {
	p := plan.Plan{SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile, Parameters: []plan.Parameter{{Name: "start", Type: datatype.FieldTypeDate, Required: true}, {Name: "end", Type: datatype.FieldTypeDate, Required: true}}, Nodes: []plan.Node{{ID: "months", Op: "date_buckets", DateBuckets: &plan.DateBuckets{Start: plan.Expr{Op: "parameter", Parameter: "start"}, End: plan.Expr{Op: "parameter", Parameter: "end"}, Name: "month", MaxMonths: 120}}}, Root: "months", Output: plan.OutputContract{Fields: []datatype.FieldInfo{f("month", datatype.FieldTypeDate, false)}, StableKey: []string{"month"}}}
	mustValid(t, p)
	p.Parameters[0].Required = false
	if plan.Validate(p) == nil {
		t.Fatal("nullable date accepted")
	}
	p.Parameters[0].Required = true
	p.Parameters[0].Allowed = []plan.Literal{{Type: datatype.FieldTypeDate, Text: "2024-02-29"}, {Type: datatype.FieldTypeDate, Text: "2024-02-29"}}
	if plan.Validate(p) == nil {
		t.Fatal("duplicate options accepted")
	}
	p.Parameters[0].Allowed = nil
	p.Nodes[0].DateBuckets.MaxMonths = 121
	if plan.Validate(p) == nil {
		t.Fatal("excessive date buckets accepted")
	}
}

func TestLiteralCanonicalPrecision(t *testing.T) {
	cases := []struct {
		typ         datatype.FieldType
		input, want string
	}{
		{datatype.FieldTypeDecimal, "-0.000", "0"}, {datatype.FieldTypeDecimal, "99999999999999999999.123456789012345678", "99999999999999999999.123456789012345678"}, {datatype.FieldTypeDecimal, "1.2000", "1.2"}, {datatype.FieldTypeBigInt, "+009007199254740993", "9007199254740993"}, {datatype.FieldTypeString, "X ", "X "},
	}
	for _, c := range cases {
		v, err := (plan.Literal{Type: c.typ, Text: c.input}).Canonical()
		if err != nil || v.Text != c.want {
			t.Fatalf("%q: %v %v", c.input, v, err)
		}
	}
	for _, v := range []plan.Literal{{Type: datatype.FieldTypeDecimal, Text: "1e3"}, {Type: datatype.FieldTypeDecimal, Text: "100000000000000000000"}, {Type: datatype.FieldTypeDecimal, Text: "0.1234567890123456789"}, {Type: datatype.FieldTypeBigInt, Text: "9223372036854775808"}, {Type: datatype.FieldTypeInt, Text: "2147483648"}, {Type: datatype.FieldTypeDate, Text: "2025-02-29"}, {Type: datatype.FieldTypeDate, Text: "0000-01-01"}, {Type: datatype.FieldTypeBool, Text: "1"}, {Type: datatype.FieldTypeString, Text: "x", Null: true}} {
		if _, err := v.Canonical(); err == nil {
			t.Fatalf("accepted %v", v)
		}
	}
}

func TestCanonicalFingerprintAndStrictJSON(t *testing.T) {
	p := fixture()
	p.Nodes = append(p.Nodes, plan.Node{ID: "root", Op: "filter", Filter: &plan.Filter{Input: "source", Predicate: op("gt", column("source", "value"), lit(datatype.FieldTypeDecimal, "1.200"))}})
	p.Root = "root"
	original, _ := json.Marshal(p)
	a, err := plan.Fingerprint(p)
	if err != nil {
		t.Fatal(err)
	}
	q := copyPlan(p)
	q.Nodes[0], q.Nodes[1] = q.Nodes[1], q.Nodes[0]
	q.Nodes[0].Filter.Predicate.Args[1].Literal.Text = "1.2"
	q.Parameters = []plan.Parameter{}
	q.Assertions = []plan.Assertion{}
	b, err := plan.Fingerprint(q)
	if err != nil || a != b {
		t.Fatalf("equivalent plans differ: %v", err)
	}
	after, _ := json.Marshal(p)
	if string(after) != string(original) {
		t.Fatal("fingerprinting mutated input")
	}
	canonical, err := plan.CanonicalJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := plan.Decode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := plan.Fingerprint(decoded)
	if c != a {
		t.Fatal("roundtrip changed fingerprint")
	}
	q.Nodes[0].Filter.Predicate.Args[1].Literal.Text = "1.3"
	d, _ := plan.Fingerprint(q)
	if a == d {
		t.Fatal("semantic change missing from hash")
	}
	for _, raw := range []string{string(canonical) + ` {}`, strings.Replace(string(canonical), `"root":`, `"sql":"select 1","root":`, 1), strings.Replace(string(canonical), `"root":`, `"root":"ignored","root":`, 1), strings.Replace(string(canonical), `"root":`, `"Root":`, 1), strings.Replace(string(canonical), `"op":"filter"`, `"op":"filter","op":"filter"`, 1)} {
		if _, err := plan.Decode([]byte(raw)); err == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
}

func TestBudgetsAndProgrammaticCycles(t *testing.T) {
	p := fixture()
	e := op("eq", column("source", "id"), lit(datatype.FieldTypeString, strings.Repeat("a", plan.MaxBytes+1)))
	p.Nodes = append(p.Nodes, plan.Node{ID: "root", Op: "filter", Filter: &plan.Filter{Input: "source", Predicate: e}})
	p.Root = "root"
	if plan.Validate(p) == nil {
		t.Fatal("oversized plan accepted")
	}
	cycle := plan.Expr{Op: "not", Args: make([]plan.Expr, 1)}
	cycle.Args[0] = cycle
	p.Nodes[1].Filter.Predicate = cycle
	if plan.Validate(p) == nil {
		t.Fatal("recursive Go expression accepted")
	}
	p = fixture()
	previous := p.Root
	for i := 0; i <= plan.MaxDepth; i++ {
		id := plan.NodeID(fmt.Sprintf("n%d", i))
		p.Nodes = append(p.Nodes, plan.Node{ID: id, Op: "distinct", Distinct: &plan.Unary{Input: previous}})
		previous = id
	}
	p.Root = previous
	if plan.Validate(p) == nil {
		t.Fatal("too deep DAG accepted")
	}
	if _, err := plan.Decode([]byte(strings.Repeat(" ", plan.MaxBytes+1))); err == nil {
		t.Fatal("JSON byte budget not applied")
	}
}

func TestLogicalPackageImportBoundary(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if strings.Contains(strings.Split(path, "/")[0], ".") && path != "github.com/addp/common/datatype" {
				t.Fatalf("logical plan acquired non-value dependency: %s", path)
			}
		}
	}
}
