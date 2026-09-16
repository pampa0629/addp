package conformance

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

func integerRequest(engineID uint) plugin.CompileRequest {
	id := datatype.FieldInfo{Name: "id", Type: datatype.FieldTypeInt}
	return plugin.CompileRequest{Instance: plugin.AnalyticalInstance{EngineID: engineID, Capability: plugin.AnalyticalCapability{Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile}}},
		Plan: plan.Plan{SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile, Root: "filtered",
			Parameters: []plan.Parameter{{Name: "amount", Type: datatype.FieldTypeDecimal}, {Name: "keep", Type: datatype.FieldTypeBool, Required: true}},
			Output:     plan.OutputContract{Fields: []datatype.FieldInfo{id, {Name: "value", Type: datatype.FieldTypeBigInt, Nullable: true}}, StableKey: []string{"id"}},
			Nodes: []plan.Node{
				{ID: "seed", Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: []datatype.FieldInfo{id}, Rows: [][]plan.Literal{{{Type: datatype.FieldTypeInt, Text: "1"}}}}},
				{ID: "converted", Op: "project", Project: &plan.Project{Input: "seed", Columns: []plan.Projection{{Name: "id", Expr: plan.Expr{Op: "column", Column: &plan.ColumnRef{Input: "seed", Name: "id"}}}, {Name: "value", Expr: plan.Expr{Op: "integer", Args: []plan.Expr{{Op: "parameter", Parameter: "amount"}}}}}}},
				{ID: "filtered", Op: "filter", Filter: &plan.Filter{Input: "converted", Predicate: plan.Expr{Op: "parameter", Parameter: "keep"}}},
			},
		},
	}
}

// PreparedLosslessInteger repeats the same matrix through real PreparedQuery,
// also discarding all data downstream. Errors must survive this empty root;
// valid NULL and exact values must retain their semantics.
func PreparedLosslessInteger(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo) {
	t.Helper()
	engineID := uint(time.Now().UnixNano())
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
	compiled, err := provider.AnalyticalCompiler().Compile(integerRequest(engineID))
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []bool{true, false} {
		name := "visible rows"
		if !keep {
			name = "empty root"
		}
		t.Run(name, func(t *testing.T) {
			losslessInteger(t, func(input *string) (*int64, bool, error) {
				v := plan.Literal{Type: datatype.FieldTypeDecimal, Null: input == nil}
				if input != nil {
					v.Text = *input
				}
				flag := "false"
				if keep {
					flag = "true"
				}
				return executeIntegerFixture(t, provider, conn, compiled, map[string]plan.Literal{"amount": v, "keep": {Type: datatype.FieldTypeBool, Text: flag}}, keep)
			}, keep)
		})
	}
}

func executeValueFixture(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo, compiled plugin.CompiledQuery, parameters map[string]plan.Literal, keep bool, code string) (*string, bool, error) {
	t.Helper()
	req, err := compiled.QueryRequest(parameters, 10*time.Second)
	if err != nil {
		return nil, false, err
	}
	prepared, err := provider.PrepareQuery(t.Context(), conn, req)
	if err != nil {
		return nil, false, err
	}
	if _, err := prepared.ReadSet(t.Context()); err != nil {
		return nil, false, err
	}
	if _, err := prepared.OutputLineage(t.Context()); err != nil {
		return nil, false, err
	}
	out, err := prepared.Execute(t.Context())
	var failure *plugin.AnalyticalEvaluationError
	if errors.As(err, &failure) {
		if out != nil || failure.Check.Code != code || failure.Check.Node != "converted" {
			t.Fatalf("invalid error result: %#v %v", out, err)
		}
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !keep {
		if len(out.Rows) != 0 {
			t.Fatal("filter ignored")
		}
		return nil, false, nil
	}
	if len(out.Rows) != 1 || len(out.Columns) != 2 {
		t.Fatalf("invalid data result: %#v", out)
	}
	if out.Rows[0]["value"] == nil {
		return nil, false, nil
	}
	var value string
	switch v := out.Rows[0]["value"].(type) {
	case int64:
		value = strconv.FormatInt(v, 10)
	case string:
		value = v
	case bool:
		value = strconv.FormatBool(v)
	default:
		t.Fatalf("not an exact numeric value: %#v", out)
	}
	return &value, false, nil
}

func executeIntegerFixture(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo, compiled plugin.CompiledQuery, parameters map[string]plan.Literal, keep bool) (*int64, bool, error) {
	text, invalid, err := executeValueFixture(t, provider, conn, compiled, parameters, keep, "expression_evaluation_failed")
	if text == nil || err != nil {
		return nil, invalid, err
	}
	value, err := strconv.ParseInt(*text, 10, 64)
	return &value, invalid, err
}

// CompilerContract runs without a database and exercises the registered compiler.
func CompilerContract(t *testing.T, c plugin.AnalyticalCompiler) {
	t.Helper()
	r := integerRequest(42)
	report, err := c.Check(r)
	if err != nil || !report.Supported {
		t.Fatalf("check=%#v err=%v", report, err)
	}
	first, err := c.Compile(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Plan.Nodes[0], r.Plan.Nodes[2] = r.Plan.Nodes[2], r.Plan.Nodes[0]
	second, err := c.Compile(r)
	if err != nil || first.Fingerprint() != second.Fingerprint() || first.Template() != second.Template() {
		t.Fatalf("non deterministic compilation: %v", err)
	}
	r.Instance.Capability = plugin.AnalyticalCapability{}
	report, err = c.Check(r)
	if err != nil || report.Supported || len(report.Diagnostics) == 0 {
		t.Fatalf("revoked check=%#v err=%v", report, err)
	}
	if _, err = c.Compile(r); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatalf("revoked compile=%v", err)
	}
	r = integerRequest(42)
	r.Plan.Root = "missing"
	if _, err = c.Compile(r); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatalf("invalid compile=%v", err)
	}
}
