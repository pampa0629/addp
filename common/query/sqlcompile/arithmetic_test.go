package sqlcompile

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// A third rendering strategy proves that the shared operation has no central
// engine-name dispatch. Real numeric results are covered by engine conformance.
type arithmeticTestDialect struct{ integerTestDialect }

func (arithmeticTestDialect) CastDecimal(v string, p, s int) string {
	return fmt.Sprintf("FIXED(%s,%d,%d)", v, p, s)
}
func (arithmeticTestDialect) QuotientEstimate(a, b string) string {
	return "ESTIMATE(" + a + "," + b + ")"
}

func TestArithmeticContractsAndBudgets(t *testing.T) {
	d := arithmeticTestDialect{}
	integer := CheckedExpression{SQL: "a", Type: datatype.FieldTypeBigInt}
	decimal := CheckedExpression{SQL: "b", Type: datatype.FieldTypeDecimal}
	for _, op := range []string{"add", "subtract", "multiply", "divide"} {
		for _, right := range []CheckedExpression{integer, decimal} {
			out, err := Arithmetic(op, integer, right, d)
			if err != nil {
				t.Fatal(err)
			}
			want := datatype.FieldTypeBigInt
			if op == "divide" || right.Type == datatype.FieldTypeDecimal {
				want = datatype.FieldTypeDecimal
			}
			if out.Type != want || out.Invalid == "" {
				t.Fatalf("lost type/check: %#v", out)
			}
			left := integer
			left.Invalid = "previous_failure"
			out, err = Arithmetic(op, left, right, d)
			if err != nil || !strings.Contains(out.Invalid, "previous_failure") {
				t.Fatalf("lost child error: %v", err)
			}
		}
	}
	if _, err := Arithmetic("power", integer, integer, d); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
	if _, err := Arithmetic("add", CheckedExpression{SQL: "a", Type: datatype.FieldTypeString}, integer, d); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
	if _, err := Arithmetic("add", integer, integer, nil); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
	if _, err := Arithmetic("add", CheckedExpression{}, integer, d); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
	large := decimal
	large.SQL = strings.Repeat("x", plan.MaxBytes/4)
	if _, err := Arithmetic("divide", large, decimal, d); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
}
