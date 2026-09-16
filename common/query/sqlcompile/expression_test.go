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

type expressionTestDialect struct{ arithmeticTestDialect }

func (expressionTestDialect) ISODateShape(s string) string { return "ISO(" + s + ")" }
func (expressionTestDialect) TextSlice(s string, start, length int) string {
	return fmt.Sprintf("SLICE(%s,%d,%d)", s, start, length)
}
func (expressionTestDialect) DateParts(s string) CalendarParts {
	return CalendarParts{"YEAR(" + s + ")", "MONTH(" + s + ")", "DAY(" + s + ")"}
}
func (expressionTestDialect) CastDate(s string) string   { return "DATE(" + s + ")" }
func (expressionTestDialect) MonthStart(s string) string { return "MONTH_START(" + s + ")" }

func (expressionTestDialect) QuoteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
func (expressionTestDialect) Value(s string, typ datatype.FieldType) (string, error) {
	return "TYPED(" + s + "," + string(typ) + ")", nil
}
func (expressionTestDialect) Literal(v plan.Literal) (string, error) {
	return "LITERAL(" + v.Text + ")", nil
}
func (expressionTestDialect) Comparable(s string, _ datatype.FieldType) (string, error) {
	return "EXACT(" + s + ")", nil
}

func (expressionTestDialect) TrimDecimalText(s string) string { return "TRIM_DECIMAL(" + s + ")" }
func (expressionTestDialect) ISODateText(s string) string     { return "ISO_DATE_TEXT(" + s + ")" }

func TestCompileExpressionBindingAndCanonicalValue(t *testing.T) {
	ref := plan.ColumnRef{Input: "source", Name: "id"}
	scope := ExpressionScope{Fields: map[plan.NodeID][]datatype.FieldInfo{"source": {{Name: "id", Type: datatype.FieldTypeInt}}}, Columns: map[plan.ColumnRef]ExpressionColumn{ref: {Relation: `a"b`, Name: `x";DROP TABLE t;--`}}}
	out, err := CompileExpression(plan.Expr{Op: "column", Column: &ref}, scope, expressionTestDialect{})
	if err != nil || out.SQL != `"a""b"."x"";DROP TABLE t;--"` {
		t.Fatalf("unquoted binding: %s %v", out.SQL, err)
	}
	literal := plan.Literal{Type: datatype.FieldTypeInt, Text: "+0002"}
	out, err = CompileExpression(plan.Expr{Op: "literal", Literal: &literal}, ExpressionScope{}, expressionTestDialect{})
	if err != nil || out.SQL != "LITERAL(2)" || literal.Text != "+0002" {
		t.Fatalf("literal not canonical/private: %#v %v", out, err)
	}
	delete(scope.Columns, ref)
	if _, err := CompileExpression(plan.Expr{Op: "column", Column: &ref}, scope, expressionTestDialect{}); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
}

func TestCompileExpressionRejectsInvalidAndUnsupported(t *testing.T) {
	p := plan.Expr{Op: "parameter", Parameter: "p"}
	scope := ExpressionScope{Parameters: []plan.Parameter{{Name: "p", Type: datatype.FieldTypeInt}}}
	for _, e := range []plan.Expr{
		{Op: "add", Args: []plan.Expr{p}},
		{Op: "coalesce", Args: []plan.Expr{p, {Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeBool, Text: "true"}}}},
		{Op: "native_sql", Parameter: "p"},
		{Op: "parameter", Parameter: "missing"},
	} {
		if _, err := CompileExpression(e, scope, expressionTestDialect{}); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
			t.Fatal(err)
		}
	}
	if out, err := CompileExpression(plan.Expr{Op: "text", Args: []plan.Expr{p}}, scope, expressionTestDialect{}); err != nil || out.Type != datatype.FieldTypeString {
		t.Fatal(err)
	}
	if _, err := CompileExpression(p, scope, nil); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
	scope.Parameters[0].Name = "p;SELECT"
	if _, err := CompileExpression(p, scope, expressionTestDialect{}); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
}

func (expressionTestDialect) ShiftMonths(date, months string) string {
	return "SHIFT_MONTHS(" + date + "," + months + ")"
}
