package sqlcompile

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

type integerTestDialect struct{}

func (integerTestDialect) TruncateDecimal(x string) string { return "CUT(" + x + ")" }
func (integerTestDialect) CastInteger(x string) string     { return "TO_INTEGER(" + x + ")" }

func TestLosslessIntegerRetainsChecks(t *testing.T) {
	input := CheckedExpression{SQL: "source.amount", Invalid: "previous_error", Type: datatype.FieldTypeDecimal}
	out, err := LosslessInteger(input, integerTestDialect{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != datatype.FieldTypeBigInt || !strings.Contains(out.Invalid, "(previous_error) OR") || !strings.Contains(out.Invalid, "IS NOT NULL AND NOT") {
		t.Fatalf("check lost: %#v", out)
	}
	// Truncation participates only in the guard, never in the returned value.
	if !strings.Contains(out.SQL, "THEN TO_INTEGER((source.amount)) ELSE TO_INTEGER(NULL)") || strings.Contains(out.SQL, "THEN TO_INTEGER(CUT") {
		t.Fatalf("lossy cast: %s", out.SQL)
	}
	for _, typ := range []datatype.FieldType{datatype.FieldTypeInt, datatype.FieldTypeBigInt} {
		input.Type = typ
		out, err := LosslessInteger(input, integerTestDialect{})
		if err != nil || out.Invalid != input.Invalid || out.Type != datatype.FieldTypeBigInt {
			t.Fatalf("integer widening lost check: %#v %v", out, err)
		}
	}
}

func TestLosslessIntegerRejectsUnsupportedTypesAndBudgets(t *testing.T) {
	if _, err := LosslessInteger(CheckedExpression{SQL: "value", Type: datatype.FieldTypeString}, integerTestDialect{}); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
	for _, value := range []string{"", strings.Repeat("x", plan.MaxBytes/2)} {
		if _, err := LosslessInteger(CheckedExpression{SQL: value, Type: datatype.FieldTypeDecimal}, integerTestDialect{}); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := LosslessInteger(CheckedExpression{SQL: "value", Type: datatype.FieldTypeDecimal}, nil); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
}
