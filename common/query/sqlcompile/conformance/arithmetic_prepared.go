package conformance

import (
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

func PreparedArithmetic(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo) {
	t.Helper()
	engineID := uint(time.Now().UnixNano())
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
	for _, tc := range ArithmeticCases() {
		t.Run(tc.Name, func(t *testing.T) {
			r := integerRequest(engineID)
			r.Plan.Parameters = []plan.Parameter{{Name: "left_value", Type: tc.Left.Type}, {Name: "right_value", Type: tc.Right.Type}, {Name: "keep", Type: datatype.FieldTypeBool, Required: true}}
			r.Plan.Nodes[1].Project.Columns[1].Expr = plan.Expr{Op: tc.Op, Args: []plan.Expr{{Op: "parameter", Parameter: "left_value"}, {Op: "parameter", Parameter: "right_value"}}}
			field := datatype.FieldInfo{Name: "value", Type: tc.OutputType(), Nullable: true}
			if field.Type == datatype.FieldTypeDecimal {
				field.Precision, field.Scale = 38, 18
			}
			r.Plan.Output.Fields[1] = field
			compiled, err := provider.AnalyticalCompiler().Compile(r)
			if err != nil {
				t.Fatal(err)
			}
			for _, keep := range []bool{true, false} {
				t.Run("keep="+strconv.FormatBool(keep), func(t *testing.T) {
					params := map[string]plan.Literal{"left_value": tc.Left, "right_value": tc.Right, "keep": {Type: datatype.FieldTypeBool, Text: strconv.FormatBool(keep)}}
					value, invalid, err := executeValueFixture(t, provider, conn, compiled, params, keep, "expression_evaluation_failed")
					tc.Check(t, value, invalid, keep, err)
				})
			}
		})
	}
	preparedArithmeticBranches(t, provider, conn, engineID)
}

func preparedArithmeticBranches(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo, engineID uint) {
	param := func(name string) plan.Expr { return plan.Expr{Op: "parameter", Parameter: name} }
	operation := func(op string, args ...plan.Expr) plan.Expr { return plan.Expr{Op: op, Args: args} }
	division := operation("divide", param("left_value"), param("right_value"))
	fallback := param("fallback")
	for _, tc := range []struct {
		name     string
		expr     plan.Expr
		fallback plan.Literal
		invalid  bool
	}{
		{"case masks division error", operation("case", param("branch"), division, fallback), plan.Literal{Type: datatype.FieldTypeDecimal, Text: "3"}, false},
		{"coalesce masks unreached division", operation("coalesce", fallback, division), plan.Literal{Type: datatype.FieldTypeDecimal, Text: "3"}, false},
		{"coalesce cannot swallow division error", operation("coalesce", division, fallback), plan.Literal{Type: datatype.FieldTypeDecimal, Text: "3"}, true},
		{"null operand cannot swallow child error", operation("add", division, fallback), plan.Literal{Type: datatype.FieldTypeDecimal, Null: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := integerRequest(engineID)
			r.Plan.Parameters = []plan.Parameter{{Name: "left_value", Type: datatype.FieldTypeDecimal}, {Name: "right_value", Type: datatype.FieldTypeDecimal}, {Name: "fallback", Type: datatype.FieldTypeDecimal}, {Name: "keep", Type: datatype.FieldTypeBool, Required: true}}
			params := map[string]plan.Literal{"left_value": {Type: datatype.FieldTypeDecimal, Text: "1"}, "right_value": {Type: datatype.FieldTypeDecimal, Text: "0"}, "fallback": tc.fallback}
			if tc.expr.Op == "case" {
				r.Plan.Parameters = append(r.Plan.Parameters, plan.Parameter{Name: "branch", Type: datatype.FieldTypeBool, Required: true})
				params["branch"] = plan.Literal{Type: datatype.FieldTypeBool, Text: "false"}
			}
			r.Plan.Nodes[1].Project.Columns[1].Expr = tc.expr
			r.Plan.Output.Fields[1] = datatype.FieldInfo{Name: "value", Type: datatype.FieldTypeDecimal, Nullable: true, Precision: 38, Scale: 18}
			compiled, err := provider.AnalyticalCompiler().Compile(r)
			if err != nil {
				t.Fatal(err)
			}
			for _, keep := range []bool{true, false} {
				params["keep"] = plan.Literal{Type: datatype.FieldTypeBool, Text: strconv.FormatBool(keep)}
				value, invalid, err := executeValueFixture(t, provider, conn, compiled, params, keep, "expression_evaluation_failed")
				if err != nil || invalid != tc.invalid {
					t.Fatalf("invalid=%t value=%v err=%v", invalid, value, err)
				}
				if keep && !invalid && (value == nil || *value != "3.000000000000000000") {
					t.Fatalf("fallback lost: %v", value)
				}
			}
		})
	}
}
