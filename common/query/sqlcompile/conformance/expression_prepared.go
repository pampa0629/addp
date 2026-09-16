package conformance

import (
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// PreparedExpressions verifies native leaf encoding, binding and exact
// comparisons through the same shared compiler used by the arithmetic suites.
func PreparedExpressions(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo) {
	t.Helper()
	lit := func(typ datatype.FieldType, text string) plan.Expr {
		return plan.Expr{Op: "literal", Literal: &plan.Literal{Type: typ, Text: text}}
	}
	null := func(typ datatype.FieldType) plan.Expr {
		return plan.Expr{Op: "literal", Literal: &plan.Literal{Type: typ, Null: true}}
	}
	op := func(name string, args ...plan.Expr) plan.Expr { return plan.Expr{Op: name, Args: args} }
	text := func(s string) plan.Expr { return lit(datatype.FieldTypeString, s) }
	boolean := func(s string) plan.Expr { return lit(datatype.FieldTypeBool, s) }
	param := plan.Expr{Op: "parameter", Parameter: "value_parameter"}
	want := func(s string) *string { return &s }
	encoded := "引号'反斜杠\\\n:p $1 ; SELECT pg_sleep(99); -- 😀 "
	cases := []expressionExample{
		{"quoted UTF8 literal", text(encoded), nil, want(encoded), false},
		{"empty text", text(""), nil, want(""), false},
		{"integer canonicalization", lit(datatype.FieldTypeInt, "+0002"), nil, want("2"), false},
		{"logical column", plan.Expr{Op: "column", Column: &plan.ColumnRef{Input: "seed", Name: "id"}}, nil, want("1"), false},
		{"decimal conversion", op("decimal", lit(datatype.FieldTypeBigInt, "9007199254740993")), nil, want("9007199254740993"), false},
		{"calendar literal", lit(datatype.FieldTypeDate, "2000-02-29"), nil, want("2000-02-29"), false},
		{"null literal", null(datatype.FieldTypeString), nil, nil, false},
		{"text identity", op("eq", text(encoded), text(encoded)), nil, want("true"), false},
		{"case sensitive identity", op("eq", text("A"), text("a")), nil, want("false"), false},
		{"trailing space identity", op("eq", text("a"), text("a ")), nil, want("false"), false},
		{"binary order", op("lt", text("Z"), text("a")), nil, want("true"), false},
		{"UTF8 binary order", op("lt", text("中"), text("😀")), nil, want("true"), false},
		{"unequal", op("ne", text("a"), text("a ")), nil, want("true"), false},
		{"less or equal", op("le", lit(datatype.FieldTypeInt, "2"), lit(datatype.FieldTypeInt, "2")), nil, want("true"), false},
		{"greater", op("gt", lit(datatype.FieldTypeInt, "3"), lit(datatype.FieldTypeInt, "2")), nil, want("true"), false},
		{"greater or equal", op("ge", boolean("true"), boolean("false")), nil, want("true"), false},
		{"null comparison", op("eq", null(datatype.FieldTypeInt), lit(datatype.FieldTypeInt, "2")), nil, nil, false},
		{"not null", op("not", null(datatype.FieldTypeBool)), nil, nil, false},
		{"not true", op("not", boolean("true")), nil, want("false"), false},
		{"and false null", op("and", boolean("false"), null(datatype.FieldTypeBool)), nil, want("false"), false},
		{"and true null", op("and", boolean("true"), null(datatype.FieldTypeBool)), nil, nil, false},
		{"or true null", op("or", boolean("true"), null(datatype.FieldTypeBool)), nil, want("true"), false},
		{"or false null", op("or", boolean("false"), null(datatype.FieldTypeBool)), nil, nil, false},
		{"integer null parameter", op("is_null", param), &plan.Literal{Type: datatype.FieldTypeInt, Null: true}, want("true"), false},
		{"boolean null parameter", op("is_null", param), &plan.Literal{Type: datatype.FieldTypeBool, Null: true}, want("true"), false},
		{"UTF8 parameter", param, &plan.Literal{Type: datatype.FieldTypeString, Text: encoded}, want(encoded), false},
		{"decimal parameter", param, &plan.Literal{Type: datatype.FieldTypeDecimal, Text: "2.750"}, want("2.75"), false},
	}
	division := op("divide", lit(datatype.FieldTypeInt, "1"), lit(datatype.FieldTypeInt, "0"))
	cases = append(cases,
		expressionExample{"AND preserves operand error", op("and", boolean("false"), op("is_null", division)), nil, nil, true},
		expressionExample{"OR preserves operand error", op("or", boolean("true"), op("is_null", division)), nil, nil, true},
		expressionExample{"NOT preserves operand error", op("not", op("is_null", division)), nil, nil, true},
		expressionExample{"comparison preserves operand error", op("eq", division, lit(datatype.FieldTypeDecimal, "0")), nil, nil, true},
	)
	preparedExpressionCases(t, provider, conn, cases)
}

type expressionExample struct {
	name      string
	expr      plan.Expr
	parameter *plan.Literal
	want      *string
	invalid   bool
}

func preparedExpressionCases(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo, cases []expressionExample) {
	t.Helper()
	engineID := uint(time.Now().UnixNano())
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := integerRequest(engineID)
			r.Plan.Parameters = []plan.Parameter{{Name: "keep", Type: datatype.FieldTypeBool, Required: true}}
			params := map[string]plan.Literal{}
			if tc.parameter != nil {
				r.Plan.Parameters = append(r.Plan.Parameters, plan.Parameter{Name: "value_parameter", Type: tc.parameter.Type})
				params["value_parameter"] = *tc.parameter
			}
			r.Plan.Nodes[1].Project.Columns[1].Expr = tc.expr
			f, err := plan.AnalyzeExpression(tc.expr, map[plan.NodeID][]datatype.FieldInfo{"seed": r.Plan.Nodes[0].ConstantRows.Fields}, r.Plan.Parameters)
			if err != nil {
				t.Fatal(err)
			}
			f.Name = "value"
			r.Plan.Output.Fields[1] = f
			compiled, err := provider.AnalyticalCompiler().Compile(r)
			if err != nil {
				t.Fatal(err)
			}
			for _, keep := range []bool{true, false} {
				t.Run("keep="+strconv.FormatBool(keep), func(t *testing.T) {
					params["keep"] = plan.Literal{Type: datatype.FieldTypeBool, Text: strconv.FormatBool(keep)}
					value, invalid, err := executeValueFixture(t, provider, conn, compiled, params, keep, "expression_evaluation_failed")
					if err != nil || invalid != tc.invalid {
						t.Fatalf("invalid=%t err=%v", invalid, err)
					}
					if !keep || tc.want == nil {
						if value != nil {
							t.Fatalf("expected no value, got %s", *value)
						}
						return
					}
					if value == nil {
						t.Fatal("unexpected NULL")
					}
					canonical, err := (plan.Literal{Type: f.Type, Text: *value}).Canonical()
					if err != nil || canonical.Text != *tc.want {
						t.Fatalf("got %q want %q err=%v", *value, *tc.want, err)
					}
				})
			}
		})
	}
}
