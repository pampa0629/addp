package plan

import (
	"strings"
	"testing"

	"github.com/addp/common/datatype"
)

func TestAnalyzeExpressionUsesPlanSemantics(t *testing.T) {
	scope := map[NodeID][]datatype.FieldInfo{"source": {field("amount", datatype.FieldTypeDecimal, true)}}
	e := Expr{Op: "integer", Args: []Expr{{Op: "column", Column: &ColumnRef{Input: "source", Name: "amount"}}}}
	out, err := AnalyzeExpression(e, scope, []Parameter{{Name: "unused_by_this_expression", Type: datatype.FieldTypeBool}})
	if err != nil || out.Type != datatype.FieldTypeBigInt || !out.Nullable || out.Name != "" {
		t.Fatalf("%#v %v", out, err)
	}
	bad := e
	bad.Args = []Expr{{Op: "parameter", Parameter: "missing"}}
	if _, err := AnalyzeExpression(bad, scope, nil); err == nil {
		t.Fatal("unresolved parameter accepted")
	}
	scope["source"][0].NativeType = "numeric"
	if _, err := AnalyzeExpression(e, scope, nil); err == nil {
		t.Fatal("native metadata accepted")
	}
}

func TestAnalyzeExpressionRejectsMalformedAndUnboundedInput(t *testing.T) {
	for _, params := range [][]Parameter{
		{{Name: "x", Type: datatype.FieldTypeBool}, {Name: "x", Type: datatype.FieldTypeBool}},
		{{Name: "x", Type: datatype.FieldTypeBool, Allowed: []Literal{{Type: datatype.FieldTypeBool, Text: "true"}, {Type: datatype.FieldTypeBool, Text: "true"}}}},
		{{Name: "x", Type: datatype.FieldTypeString, Allowed: []Literal{{Type: datatype.FieldTypeString, Text: strings.Repeat("x", MaxBytes)}}}},
	} {
		if _, err := AnalyzeExpression(Expr{Op: "parameter", Parameter: "x"}, nil, params); err == nil {
			t.Fatal("invalid parameter declarations accepted")
		}
	}
	e := Expr{Op: "not", Args: make([]Expr, 1)}
	e.Args[0] = e
	if _, err := AnalyzeExpression(e, nil, nil); err == nil {
		t.Fatal("cyclic Go expression accepted")
	}
	for _, e := range []Expr{
		{Op: "add", Args: []Expr{{Op: "literal", Literal: &Literal{Type: datatype.FieldTypeInt, Text: "1"}}}},
		{Op: "literal", Literal: &Literal{Type: datatype.FieldTypeString, Text: strings.Repeat("x", MaxBytes)}},
		{Op: "not", Args: make([]Expr, MaxExpressions+1)},
	} {
		if _, err := AnalyzeExpression(e, nil, nil); err == nil {
			t.Fatal("invalid expression accepted")
		}
	}
}
