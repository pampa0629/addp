package plugin

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/query/plan"
)

func resultCompileRequest() CompileRequest {
	fields := []datatype.FieldInfo{{Name: "value", Type: datatype.FieldTypeBigInt}}
	return CompileRequest{Instance: AnalyticalInstance{EngineID: 7, Capability: AnalyticalCapability{
		Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile},
	}}, Plan: plan.Plan{SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile,
		Nodes: []plan.Node{{ID: "rows", Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: fields}},
			{ID: "violations", Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: fields}}},
		Root: "rows", Output: plan.OutputContract{Fields: fields, StableKey: []string{"value"}}, Assertions: []plan.Assertion{{Violation: "violations", Code: "invalid_source"}},
	}}
}

type resultTestCompiler struct{ version string }

func (c resultTestCompiler) Identity() CompilerIdentity {
	return CompilerIdentity{ID: "result_test", Version: c.version}
}
func (c resultTestCompiler) Check(r CompileRequest) (SupportReport, error) {
	return SupportReport{Supported: true}, r.Validate()
}
func (c resultTestCompiler) Compile(r CompileRequest) (CompiledQuery, error) {
	return NewCompiledQuery(r, c.Identity(), "sql", "SELECT :value AS value, 'data' AS __addp_record", nil)
}

type analyticalRuntimeTestProvider struct {
	fakeSQLRuntimeProvider
	result   *QueryResult
	calls    int
	compiler resultTestCompiler
	deadline bool
}

func (p *analyticalRuntimeTestProvider) AnalyticalCompiler() AnalyticalCompiler { return p.compiler }
func (p *analyticalRuntimeTestProvider) ExecuteSQL(ctx context.Context, _ ConnectionInfo, sql string, opts QueryOptions) (*QueryResult, error) {
	p.calls++
	_, p.deadline = ctx.Deadline()
	p.lastSQL, p.lastOptions = sql, opts
	return p.result, nil
}

func TestAnalyticalResultChecksIndependentOfData(t *testing.T) {
	r := resultCompileRequest()
	q, err := NewCompiledQuery(r, resultTestCompiler{"1"}.Identity(), "sql", "SELECT 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	row := func(marker string, value interface{}) map[string]interface{} {
		return map[string]interface{}{"value": value, q.layout.ControlColumn: marker}
	}
	tests := []struct {
		name  string
		rows  []map[string]interface{}
		want  error
		count int
	}{
		{"empty success", []map[string]interface{}{row("ok:1", nil), row("complete", nil)}, nil, 0},
		{"empty assertion fails", []map[string]interface{}{row("fail:1", nil), row("complete", nil)}, ErrAnalyticalAssertion, 0},
		{"data before failed assertion", []map[string]interface{}{row("data", int64(4)), row("complete", nil), row("fail:1", nil)}, ErrAnalyticalAssertion, 0},
		{"successful data", []map[string]interface{}{row("complete", nil), row("data", int64(4)), row("ok:1", nil)}, nil, 1},
		{"no records", nil, ErrAnalyticalResultInvalid, 0},
		{"truncated checks", []map[string]interface{}{row("data", int64(4)), row("complete", nil)}, ErrAnalyticalResultInvalid, 0},
		{"missing completion", []map[string]interface{}{row("ok:1", nil)}, ErrAnalyticalResultInvalid, 0},
		{"duplicate check", []map[string]interface{}{row("ok:1", nil), row("ok:1", nil), row("complete", nil)}, ErrAnalyticalResultInvalid, 0},
		{"unknown check", []map[string]interface{}{row("ok:2", nil), row("complete", nil)}, ErrAnalyticalResultInvalid, 0},
		{"invalid marker", []map[string]interface{}{row("something", nil)}, ErrAnalyticalResultInvalid, 0},
		{"control carries data", []map[string]interface{}{row("ok:1", int64(5)), row("complete", nil)}, ErrAnalyticalResultInvalid, 0},
		{"null nonnullable", []map[string]interface{}{row("data", nil), row("ok:1", nil), row("complete", nil)}, ErrAnalyticalResultInvalid, 0},
		{"float loses precision", []map[string]interface{}{row("data", float64(4)), row("ok:1", nil), row("complete", nil)}, ErrAnalyticalResultInvalid, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := q.normalizeResult(&QueryResult{Columns: []string{"value", q.layout.ControlColumn}, Rows: tt.rows})
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v want=%v", err, tt.want)
			}
			if err != nil {
				if out != nil {
					t.Fatal("partial result escaped")
				}
				return
			}
			if len(out.Rows) != tt.count || !reflect.DeepEqual(out.Columns, []string{"value"}) {
				t.Fatalf("unexpected output: %#v", out)
			}
		})
	}
}

func TestAnalyticalPrepareFreezesAndConsumesOnce(t *testing.T) {
	r := resultCompileRequest()
	r.Plan.Parameters = []plan.Parameter{{Name: "value", Type: datatype.FieldTypeBigInt, Required: true}}
	r.Plan.Nodes = append(r.Plan.Nodes, plan.Node{ID: "parameter_result", Op: "project", Project: &plan.Project{Input: "rows", Columns: []plan.Projection{{Name: "value", Expr: plan.Expr{Op: "parameter", Parameter: "value"}}}}})
	r.Plan.Root = "parameter_result"
	c := resultTestCompiler{"1"}
	q, err := c.Compile(r)
	if err != nil {
		t.Fatal(err)
	}
	req, err := q.QueryRequest(map[string]plan.Literal{"value": {Type: datatype.FieldTypeBigInt, Text: "9007199254740993"}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	p := &analyticalRuntimeTestProvider{compiler: c, result: &QueryResult{Columns: []string{"value", q.layout.ControlColumn}, Rows: []map[string]interface{}{
		{"value": int64(9007199254740993), q.layout.ControlColumn: "data"},
		{"value": nil, q.layout.ControlColumn: "ok:1"},
		{"value": nil, q.layout.ControlColumn: "complete"},
	}}}
	prepared, err := PrepareSQLRuntimeQuery(p, nil, req, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Streaming is rejected without consuming the existing prepared value.
	if _, _, err := ConsumeSQLPreparedQuery(prepared, p); !errors.Is(err, ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
	if _, err := prepared.ReadSet(t.Context()); !errors.Is(err, ErrQueryReadSetUnresolved) {
		t.Fatalf("authorization facts must fail closed: %v", err)
	}
	req.Options.Parameters["value"] = int64(1)
	req.Query = "SELECT 0"
	result, err := prepared.Execute(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["value"] != int64(9007199254740993) {
		t.Fatalf("result=%#v", result)
	}
	if p.lastSQL != "SELECT $1 AS value, 'data' AS __addp_record" || p.lastOptions.Parameters != nil || p.lastOptions.Args[0] != int64(9007199254740993) || p.lastOptions.Limit != 0 {
		t.Fatalf("bound request changed: %s %#v", p.lastSQL, p.lastOptions)
	}
	if !p.deadline {
		t.Fatal("analytical timeout did not reach the query provider")
	}
	if _, err := prepared.Execute(t.Context()); !errors.Is(err, ErrPreparedQueryConsumed) || p.calls != 1 {
		t.Fatal("not one-shot")
	}
}

func TestAnalyticalPrepareRejectsRequestMutation(t *testing.T) {
	r := resultCompileRequest()
	r.Plan.Parameters = []plan.Parameter{{Name: "value", Type: datatype.FieldTypeBigInt, Required: true}}
	r.Plan.Nodes = append(r.Plan.Nodes, plan.Node{ID: "parameter_result", Op: "project", Project: &plan.Project{Input: "rows", Columns: []plan.Projection{{Name: "value", Expr: plan.Expr{Op: "parameter", Parameter: "value"}}}}})
	r.Plan.Root = "parameter_result"
	c := resultTestCompiler{"1"}
	q, err := c.Compile(r)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*QueryRequest){
		"template":              func(r *QueryRequest) { r.Query += " LIMIT 1" },
		"language":              func(r *QueryRequest) { r.Language = "SQL" },
		"engine":                func(r *QueryRequest) { r.EngineID++ },
		"pool engine":           func(r *QueryRequest) { r.Options.EngineID = 8 },
		"pool type":             func(r *QueryRequest) { r.Options.EngineType = "other" },
		"limit":                 func(r *QueryRequest) { r.Options.Limit = 1 },
		"offset":                func(r *QueryRequest) { r.Options.Offset = 1 },
		"write":                 func(r *QueryRequest) { r.Options.ReadOnly = false },
		"describe":              func(r *QueryRequest) { r.Options.Describe = true },
		"spatial":               func(r *QueryRequest) { r.Options.Spatial = true },
		"target":                func(r *QueryRequest) { r.TargetPath = &EngineCatalogPath{} },
		"args":                  func(r *QueryRequest) { r.Options.Args = []interface{}{1} },
		"extra parameter":       func(r *QueryRequest) { r.Options.Parameters["extra"] = 1 },
		"negative timeout":      func(r *QueryRequest) { r.Options.Timeout = -1 },
		"parameter native type": func(r *QueryRequest) { r.Options.Parameters["value"] = []byte("1") },
		"float parameter":       func(r *QueryRequest) { r.Options.Parameters["value"] = float64(1) },
		"missing parameter":     func(r *QueryRequest) { delete(r.Options.Parameters, "value") },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			req, err := q.QueryRequest(map[string]plan.Literal{"value": {Type: datatype.FieldTypeBigInt, Text: "1"}}, 0)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&req)
			p := &analyticalRuntimeTestProvider{compiler: c}
			if _, err := PrepareSQLRuntimeQuery(p, nil, req, nil, nil); !errors.Is(err, ErrAnalyticalInvalid) {
				t.Fatalf("mutation accepted: %v", err)
			}
			if p.calls != 0 {
				t.Fatal("executed during prepare")
			}
		})
	}
}

func TestAnalyticalScalarNormalization(t *testing.T) {
	tests := []struct {
		typ     datatype.FieldType
		raw     interface{}
		want    interface{}
		invalid bool
	}{
		{datatype.FieldTypeDecimal, "-12.5", "-12.500000000000000000", false},
		{datatype.FieldTypeDecimal, "99999999999999999999.999999999999999999", "99999999999999999999.999999999999999999", false},
		{datatype.FieldTypeDecimal, "100000000000000000000", nil, true},
		{datatype.FieldTypeDecimal, "0.0000000000000000001", nil, true},
		{datatype.FieldTypeDecimal, 0.1, nil, true},
		{datatype.FieldTypeBigInt, "9223372036854775807", int64(9223372036854775807), false},
		{datatype.FieldTypeInt, "2147483648", nil, true},
		{datatype.FieldTypeBool, int64(1), true, false},
		{datatype.FieldTypeBool, int64(2), nil, true},
		{datatype.FieldTypeString, " Mixed ", " Mixed ", false},
		{datatype.FieldTypeString, int64(3), nil, true},
		{datatype.FieldTypeDate, time.Date(2024, 2, 29, 0, 0, 0, 0, time.FixedZone("local", 8*3600)), "2024-02-29", false},
		{datatype.FieldTypeDate, time.Date(2024, 2, 29, 1, 0, 0, 0, time.UTC), nil, true},
		{datatype.FieldTypeDate, "2023-02-29", nil, true},
	}
	for _, tt := range tests {
		v, err := analyticalValueLiteral(tt.typ, tt.raw)
		if tt.invalid {
			if err == nil {
				t.Fatalf("accepted %#v", tt.raw)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		out, err := analyticalLiteralValue(v)
		if err != nil || out != tt.want {
			t.Fatalf("raw=%#v out=%#v err=%v", tt.raw, out, err)
		}
	}
}

func TestAnalyticalLineageRetainsAssertionDependency(t *testing.T) {
	q := CompiledQuery{layout: AnalyticalResultLayout{ControlColumn: "__addp_record"}}
	original := &QueryOutputLineage{Sources: []QueryOutputSource{{Bindings: []QueryOutputBinding{{SourcePath: []string{"secret"}, OutputPath: []string{"__addp_record"}, Transformation: QueryOutputTransformationDirect}}}}}
	out := q.normalizeLineage(original)
	if len(out.Sources) != 1 || len(out.Sources[0].Bindings) != 1 || out.Sources[0].Bindings[0].Transformation != QueryOutputTransformationDerived || out.Sources[0].Bindings[0].OutputPath != nil {
		t.Fatalf("dependency dropped: %#v", out)
	}
	if original.Sources[0].Bindings[0].OutputPath[0] != "__addp_record" {
		t.Fatal("input mutated")
	}
}

func TestAnalyticalChecksWithSharedErrorCodeRemainDistinct(t *testing.T) {
	r := resultCompileRequest()
	r.Plan.Assertions = append(r.Plan.Assertions, plan.Assertion{Violation: "rows", Code: "invalid_source"})
	q, err := NewCompiledQuery(r, resultTestCompiler{"1"}.Identity(), "sql", "SELECT 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	layout := q.ResultLayout()
	layout.Assertions[0].Code = "mutated"
	if q.ResultLayout().Assertions[0].Code != "invalid_source" {
		t.Fatal("layout mutated")
	}
	raw := &QueryResult{Columns: []string{"value", layout.ControlColumn}, Rows: []map[string]interface{}{
		{"value": nil, layout.ControlColumn: "ok:1"},
		{"value": nil, layout.ControlColumn: "fail:2"},
		{"value": nil, layout.ControlColumn: "complete"},
	}}
	if out, err := q.normalizeResult(raw); out != nil || !errors.Is(err, ErrAnalyticalAssertion) {
		t.Fatalf("shared code check skipped: %#v %v", out, err)
	}
	raw.Rows = raw.Rows[1:]
	if _, err := q.normalizeResult(raw); !errors.Is(err, ErrAnalyticalResultInvalid) {
		t.Fatalf("missing check accepted: %v", err)
	}
}

func TestAnalyticalEvaluationChecksPrecedeValueNormalization(t *testing.T) {
	r := resultCompileRequest()
	checks := []EvaluationCheck{{Node: "rows", Code: "integer_not_exact"}}
	q, err := NewCompiledQuery(r, resultTestCompiler{"1"}.Identity(), "sql", "SELECT 1", checks)
	if err != nil {
		t.Fatal(err)
	}
	checks[0].Code = "changed"
	layout := q.ResultLayout()
	layout.Evaluations[0].Code = "changed"
	if q.ResultLayout().Evaluations[0].Code != "integer_not_exact" {
		t.Fatal("mutable evaluations")
	}
	row := func(marker string) map[string]interface{} {
		return map[string]interface{}{"value": nil, layout.ControlColumn: marker}
	}
	for _, data := range []bool{false, true} {
		raw := &QueryResult{Columns: []string{"value", layout.ControlColumn}, Rows: []map[string]interface{}{row("ok:1"), row("eval_fail:1"), row("complete")}}
		if data {
			raw.Rows = append(raw.Rows, row("data"))
		}
		out, err := q.normalizeResult(raw)
		var fault *AnalyticalEvaluationError
		if out != nil || !errors.As(err, &fault) || fault.Check.Code != "integer_not_exact" {
			t.Fatalf("evaluation error lost: %#v %v", out, err)
		}
	}
	for _, markers := range [][]string{{"ok:1", "complete"}, {"ok:1", "eval_ok:1", "eval_ok:1", "complete"}, {"ok:1", "eval_ok:2", "complete"}} {
		raw := &QueryResult{Columns: []string{"value", layout.ControlColumn}}
		for _, marker := range markers {
			raw.Rows = append(raw.Rows, row(marker))
		}
		if _, err := q.normalizeResult(raw); !errors.Is(err, ErrAnalyticalResultInvalid) {
			t.Fatal(err)
		}
	}
	other, err := NewCompiledQuery(r, resultTestCompiler{"1"}.Identity(), "sql", "SELECT 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if q.Fingerprint() == other.Fingerprint() {
		t.Fatal("evaluation contract omitted from fingerprint")
	}
	for _, checks := range [][]EvaluationCheck{{{Node: "missing", Code: "bad"}}, {{Node: "rows", Code: "bad"}, {Node: "rows", Code: "bad"}}} {
		if _, err := NewCompiledQuery(r, resultTestCompiler{"1"}.Identity(), "sql", "SELECT 1", checks); !errors.Is(err, ErrAnalyticalInvalid) {
			t.Fatal(err)
		}
	}
}
