package conformance

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/query/sqlcompile"
)

// ScanBinding strips presentation facts; names/types come from the real native
// column catalog supplied by the integration fixture, never an invented CAST.
func ScanBinding(t *testing.T, path plugin.EngineCatalogPath, physical []datatype.FieldInfo) plugin.SourceBinding {
	t.Helper()
	names := map[string]string{"row_id": "key", "small_value": "small", "label_value": "label", "numeric_value": "amount", "flag_value": "flag", "day_value": "day"}
	binding := plugin.SourceBinding{Source: "business", Path: path}
	for _, f := range physical {
		logical, ok := names[f.Name]
		if !ok {
			continue
		}
		binding.Columns = append(binding.Columns, plugin.ColumnBinding{Column: logical, Field: datatype.FieldInfo{Name: f.Name, Path: []string{f.Name}, Type: f.Type, NativeType: f.NativeType, Nullable: f.Nullable, Size: f.Size, Precision: f.Precision, Scale: f.Scale}})
	}
	if len(binding.Columns) != len(names) {
		t.Fatalf("incomplete native fields: %#v", physical)
	}
	return binding
}
func scanRequest(s plugin.SourceBinding) plugin.CompileRequest {
	r := integerRequest(s.Path.EngineID)
	fields := make([]datatype.FieldInfo, 0, len(s.Columns))
	for _, c := range s.Columns {
		f := datatype.FieldInfo{Name: c.Column, Type: c.Field.Type, Nullable: c.Field.Nullable}
		if f.Type == datatype.FieldTypeDecimal {
			f.Precision = 38
			f.Scale = 18
		}
		fields = append(fields, f)
	}
	r.Sources = []plugin.SourceBinding{s}
	r.Plan.Parameters = []plan.Parameter{{Name: "keep", Type: datatype.FieldTypeBool, Required: true}}
	r.Plan.Nodes = []plan.Node{{ID: "scan", Op: "scan", Scan: &plan.Scan{Source: s.Source, Fields: fields}}, {ID: "filtered", Op: "filter", Filter: &plan.Filter{Input: "scan", Predicate: plan.Expr{Op: "parameter", Parameter: "keep"}}}}
	r.Plan.Root = "filtered"
	r.Plan.Output = plan.OutputContract{Fields: fields, StableKey: []string{"key"}}
	return r
}
func executeScan(t *testing.T, p plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo, r plugin.CompileRequest, keep bool) (*plugin.QueryResult, error) {
	t.Helper()
	c, err := p.AnalyticalCompiler().Compile(r)
	if err != nil {
		t.Fatal(err)
	}
	value := "false"
	if keep {
		value = "true"
	}
	req, err := c.QueryRequest(map[string]plan.Literal{"keep": {Type: datatype.FieldTypeBool, Text: value}}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := p.PrepareQuery(t.Context(), conn, req)
	if err != nil {
		t.Fatal(err)
	}
	read, err := prepared.ReadSet(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	expected, err := plugin.NewQueryReadSet(r.Sources[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read, expected) {
		t.Fatalf("read set=%#v want=%#v", read, expected)
	}
	lineage, err := prepared.OutputLineage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(lineage.Sources) != 1 || !reflect.DeepEqual(lineage.Sources[0].Path, r.Sources[0].Path) {
		t.Fatalf("source missing from lineage: %#v", lineage)
	}
	return prepared.Execute(t.Context())
}
func PreparedScan(t *testing.T, p plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo, s plugin.SourceBinding) {
	t.Helper()
	r := scanRequest(s)
	// Binding list order has no meaning; scan/output field order does.
	compiled, err := p.AnalyticalCompiler().Compile(r)
	if err != nil {
		t.Fatal(err)
	}
	reversed := r
	reversed.Sources = append([]plugin.SourceBinding(nil), r.Sources...)
	reversed.Sources[0].Columns = append([]plugin.ColumnBinding(nil), s.Columns...)
	cols := reversed.Sources[0].Columns
	for i, j := 0, len(cols)-1; i < j; i, j = i+1, j-1 {
		cols[i], cols[j] = cols[j], cols[i]
	}
	again, err := p.AnalyticalCompiler().Compile(reversed)
	if err != nil || compiled.Template() != again.Template() || compiled.Fingerprint() != again.Fingerprint() {
		t.Fatalf("binding order changed compilation: %v", err)
	}
	want := []map[string]any{
		{"key": int64(1), "small": int64(7), "label": "A", "amount": "12345678901234567890.123456789012345678", "flag": true, "day": "0001-01-01"},
		{"key": int64(2), "small": int64(-2), "label": "a ", "amount": "-0.000000000000000001", "flag": false, "day": "2024-02-29"},
		{"key": int64(9007199254740993), "small": int64(0), "label": nil, "amount": nil, "flag": nil, "day": nil},
	}
	out, err := executeScan(t, p, conn, r, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Rows, want) {
		t.Fatalf("rows=%#v want=%#v", out.Rows, want)
	}
	out, err = executeScan(t, p, conn, r, false)
	if err != nil || len(out.Rows) != 0 {
		t.Fatalf("empty scan output=%#v err=%v", out, err)
	}
	// Exact text comparison must ignore the source's default case/padding rules.
	predicate := plan.Expr{Op: "and", Args: []plan.Expr{{Op: "parameter", Parameter: "keep"}, {Op: "eq", Args: []plan.Expr{{Op: "column", Column: &plan.ColumnRef{Input: "scan", Name: "label"}}, {Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeString, Text: "a"}}}}}}
	r.Plan.Nodes[1].Filter = &plan.Filter{Input: "scan", Predicate: predicate}
	out, err = executeScan(t, p, conn, r, true)
	if err != nil || len(out.Rows) != 0 {
		t.Fatalf("source text identity lost: %#v %v", out, err)
	}
}
func PreparedInvalidScan(t *testing.T, p plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo, s plugin.SourceBinding) {
	t.Helper()
	for _, keep := range []bool{true, false} {
		out, err := executeScan(t, p, conn, scanRequest(s), keep)
		var failure *plugin.AnalyticalEvaluationError
		if !errors.As(err, &failure) || out != nil || failure.Check.Node != "scan" || failure.Check.Code != sqlcompile.EvaluationFailureCode {
			t.Fatalf("scan error lost: keep=%t output=%#v err=%v", keep, out, err)
		}
	}
}
