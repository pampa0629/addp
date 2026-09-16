package conformance

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// PreparedConditionals executes the same logical branch/error scenarios on
// every tested engine through ReadSet, OutputLineage and the real executor.
func PreparedConditionals(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo) {
	t.Helper()
	param := func(name string) plan.Expr { return plan.Expr{Op: "parameter", Parameter: name} }
	op := func(name string, args ...plan.Expr) plan.Expr { return plan.Expr{Op: name, Args: args} }
	amount := op("integer", param("amount"))
	other := op("integer", param("other"))
	third := op("integer", param("third"))
	choice := param("choice")
	conditional := op("case", choice, amount, other)
	coalesce := op("coalesce", amount, other)
	cases := []struct {
		name   string
		expr   plan.Expr
		values map[string]string // "null" is a typed business NULL
		want   string            // integer, "null", or "error"
	}{
		{"true masks invalid else", conditional, map[string]string{"choice": "true", "amount": "2", "other": "2.7"}, "2"},
		{"false masks invalid then", conditional, map[string]string{"choice": "false", "amount": "2.7", "other": "3"}, "3"},
		{"null selects else", conditional, map[string]string{"choice": "null", "amount": "2.7", "other": "3"}, "3"},
		{"true selects error", conditional, map[string]string{"choice": "true", "amount": "2.7", "other": "3"}, "error"},
		{"false selects error", conditional, map[string]string{"choice": "false", "amount": "2", "other": "2.7"}, "error"},
		{"null selects error", conditional, map[string]string{"choice": "null", "amount": "2", "other": "2.7"}, "error"},
		{"selected null is not fallback", conditional, map[string]string{"choice": "true", "amount": "null", "other": "3"}, "null"},
		{"invalid condition fails", op("case", op("is_null", op("integer", param("condition_amount"))), amount, other), map[string]string{"condition_amount": "2.7", "amount": "2", "other": "3"}, "error"},
		{"coalesce stops at value", coalesce, map[string]string{"amount": "2", "other": "2.7"}, "2"},
		{"coalesce continues at null", coalesce, map[string]string{"amount": "null", "other": "3"}, "3"},
		{"coalesce all null", coalesce, map[string]string{"amount": "null", "other": "null"}, "null"},
		{"coalesce cannot swallow first error", coalesce, map[string]string{"amount": "2.7", "other": "3"}, "error"},
		{"coalesce reached error fails", coalesce, map[string]string{"amount": "null", "other": "2.7"}, "error"},
		{"coalesce middle value masks later error", op("coalesce", amount, other, third), map[string]string{"amount": "null", "other": "3", "third": "2.7"}, "3"},
		{"coalesce middle error cannot use later value", op("coalesce", amount, other, third), map[string]string{"amount": "null", "other": "2.7", "third": "3"}, "error"},
		{"coalesce reaches third", op("coalesce", amount, other, third), map[string]string{"amount": "null", "other": "null", "third": "4"}, "4"},
		{"case masks whole failing coalesce", op("case", choice, third, coalesce), map[string]string{"choice": "true", "amount": "2.7", "other": "3", "third": "4"}, "4"},
		{"case reaches failing coalesce", op("case", choice, third, coalesce), map[string]string{"choice": "false", "amount": "2.7", "other": "3", "third": "4"}, "error"},
		{"coalesce masks whole failing case", op("coalesce", third, conditional), map[string]string{"choice": "true", "amount": "2.7", "other": "3", "third": "4"}, "4"},
		{"coalesce cannot hide failing case", op("coalesce", conditional, third), map[string]string{"choice": "true", "amount": "2.7", "other": "3", "third": "4"}, "error"},
		{"coalesce fills selected null", op("coalesce", conditional, third), map[string]string{"choice": "true", "amount": "null", "other": "2.7", "third": "4"}, "4"},
		{"nested case masks inner errors", op("case", choice, third, op("case", op("is_null", amount), amount, other)), map[string]string{"choice": "true", "amount": "2.7", "other": "2.7", "third": "4"}, "4"},
	}
	engineID := uint(time.Now().UnixNano())
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := integerRequest(engineID)
			r.Plan.Nodes[1].Project.Columns[1].Expr = tc.expr
			r.Plan.Parameters = []plan.Parameter{{Name: "keep", Type: datatype.FieldTypeBool, Required: true}}
			parameters := map[string]plan.Literal{}
			var collect func(plan.Expr)
			collect = func(e plan.Expr) {
				if e.Op == "parameter" {
					if _, found := parameters[e.Parameter]; found {
						return
					}
					typ := datatype.FieldTypeDecimal
					if e.Parameter == "choice" {
						typ = datatype.FieldTypeBool
					}
					text, ok := tc.values[e.Parameter]
					if !ok {
						t.Fatalf("missing fixture parameter %s", e.Parameter)
					}
					value := plan.Literal{Type: typ, Text: text}
					if text == "null" {
						value.Text, value.Null = "", true
					}
					parameters[e.Parameter] = value
					r.Plan.Parameters = append(r.Plan.Parameters, plan.Parameter{Name: e.Parameter, Type: typ})
				}
				for _, arg := range e.Args {
					collect(arg)
				}
			}
			collect(tc.expr)
			compiled, err := provider.AnalyticalCompiler().Compile(r)
			if err != nil {
				t.Fatal(err)
			}
			for _, keep := range []bool{true, false} {
				t.Run(fmt.Sprintf("keep=%t", keep), func(t *testing.T) {
					parameters["keep"] = plan.Literal{Type: datatype.FieldTypeBool, Text: strconv.FormatBool(keep)}
					value, invalid, err := executeIntegerFixture(t, provider, conn, compiled, parameters, keep)
					if err != nil || invalid != (tc.want == "error") {
						t.Fatalf("value=%v invalid=%t err=%v want=%s", value, invalid, err, tc.want)
					}
					if !keep || invalid {
						return
					}
					if tc.want == "null" {
						if value != nil {
							t.Fatalf("expected NULL, got %d", *value)
						}
					} else if value == nil || strconv.FormatInt(*value, 10) != tc.want {
						t.Fatalf("value=%v want=%s", value, tc.want)
					}
				})
			}
		})
	}
}
