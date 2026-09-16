package sqlcompile

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

func TestConditionalRejectsInvalidContracts(t *testing.T) {
	boolean := CheckedExpression{SQL: "flag", Type: datatype.FieldTypeBool}
	integer := CheckedExpression{SQL: "value", Type: datatype.FieldTypeBigInt}
	invalid := []CheckedExpression{
		{SQL: " ", Type: datatype.FieldTypeBigInt},
		{SQL: "value", Invalid: " ", Type: datatype.FieldTypeBigInt},
		{SQL: "value", Type: "unknown"},
		{SQL: strings.Repeat("x", plan.MaxBytes), Type: datatype.FieldTypeBigInt},
	}
	for _, bad := range invalid {
		if _, err := Case(boolean, bad, integer); err == nil {
			t.Fatal("invalid case accepted")
		}
		if _, err := Coalesce(bad, integer); err == nil {
			t.Fatal("invalid coalesce accepted")
		}
	}
	for _, args := range [][]CheckedExpression{nil, {integer}, {integer, boolean}, make([]CheckedExpression, plan.MaxExpressions+1)} {
		if _, err := Coalesce(args...); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
			t.Fatalf("invalid coalesce: %v", err)
		}
	}
	if _, err := Case(integer, integer, integer); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
	if _, err := Case(boolean, boolean, integer); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
}

func TestConditionalExpansionBudget(t *testing.T) {
	// Inputs fit; repeating the condition for its selected error must not
	// produce a compiled expression beyond the common resource budget.
	condition := CheckedExpression{SQL: strings.Repeat("x", plan.MaxBytes/2), Type: datatype.FieldTypeBool}
	value := CheckedExpression{SQL: "v", Invalid: "bad", Type: datatype.FieldTypeBigInt}
	if _, err := Case(condition, value, value); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
	large := CheckedExpression{SQL: condition.SQL, Type: datatype.FieldTypeBigInt}
	if _, err := Coalesce(large, value); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
	// A long argument list must expand linearly, not repeat all prefix guards.
	args := make([]CheckedExpression, 1000)
	for i := range args {
		args[i] = value
	}
	if out, err := Coalesce(args...); err != nil || len(out.SQL)+len(out.Invalid) > 100*len(args) {
		t.Fatalf("unbounded expansion: %v", err)
	}
}

func TestConditionalWithoutErrorsNeedsNoCheck(t *testing.T) {
	value := CheckedExpression{SQL: "v", Type: datatype.FieldTypeBigInt}
	condition := CheckedExpression{SQL: "flag", Type: datatype.FieldTypeBool}
	for _, compile := range []func() (CheckedExpression, error){
		func() (CheckedExpression, error) { return Case(condition, value, value) },
		func() (CheckedExpression, error) { return Coalesce(value, value) },
	} {
		out, err := compile()
		if err != nil || out.Invalid != "" || out.Type != value.Type {
			t.Fatalf("unexpected check: %#v %v", out, err)
		}
	}
}
