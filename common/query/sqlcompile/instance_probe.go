package sqlcompile

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// InstanceProbeSession is a caller-owned pinned connection or transaction.
// Do not pass a pool: instance facts and semantic probes must use one session.
// This metadata probe neither reads business sources nor executes owner plans.
type InstanceProbeSession interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ProbeInstanceSemantics verifies a small set of deployment prerequisites with
// the real scalar compiler. It does not replace the full CI conformance suite
// and never enables a production capability by itself.
func ProbeInstanceSemantics(ctx context.Context, session InstanceProbeSession, d ExpressionDialect) (plugin.SupportReport, error) {
	if session == nil || d == nil {
		return plugin.SupportReport{}, plugin.ErrAnalyticalInvalid
	}
	lit := func(typ datatype.FieldType, value string) plan.Expr {
		return plan.Expr{Op: "literal", Literal: &plan.Literal{Type: typ, Text: value}}
	}
	op := func(name string, args ...plan.Expr) plan.Expr { return plan.Expr{Op: name, Args: args} }
	eq := func(a, b plan.Expr) plan.Expr { return op("eq", a, b) }
	checks := []struct {
		code string
		expr plan.Expr
	}{
		{"analytical_integer_semantics", eq(op("add", lit(datatype.FieldTypeBigInt, "9007199254740993"), lit(datatype.FieldTypeBigInt, "1")), lit(datatype.FieldTypeBigInt, "9007199254740994"))},
		{"analytical_decimal_semantics", eq(op("divide", lit(datatype.FieldTypeInt, "2"), lit(datatype.FieldTypeInt, "3")), lit(datatype.FieldTypeDecimal, "0.666666666666666667"))},
		{"analytical_calendar_semantics", eq(op("add_months", lit(datatype.FieldTypeDate, "2024-01-31"), lit(datatype.FieldTypeInt, "1")), lit(datatype.FieldTypeDate, "2024-02-29"))},
		{"analytical_date_format", eq(op("text", lit(datatype.FieldTypeDate, "0001-01-01")), lit(datatype.FieldTypeString, "0001-01-01"))},
		{"analytical_date_upper_bound", eq(op("text", lit(datatype.FieldTypeDate, "9999-12-31")), lit(datatype.FieldTypeString, "9999-12-31"))},
		{"analytical_text_case", op("ne", lit(datatype.FieldTypeString, "A"), lit(datatype.FieldTypeString, "a"))},
		{"analytical_text_trailing_space", op("ne", lit(datatype.FieldTypeString, "a"), lit(datatype.FieldTypeString, "a "))},
		{"analytical_text_encoding", op("ne", lit(datatype.FieldTypeString, "中文😀"), lit(datatype.FieldTypeString, "中文😁"))},
		{"analytical_null_semantics", op("is_null", op("text", plan.Expr{Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeBool, Null: true}}))},
	}
	// Individual fixed SELECTs keep SQL expansion and server work bounded, and
	// identify the failing semantic without leaking query text in diagnostics.
	for _, check := range checks {
		if err := ctx.Err(); err != nil {
			return plugin.SupportReport{}, err
		}
		x, err := CompileExpression(check.expr, ExpressionScope{}, d)
		if err != nil {
			return plugin.SupportReport{}, err
		}
		invalid := "FALSE"
		if x.Invalid != "" {
			invalid = x.Invalid
		}
		var ok, bad bool
		if err = session.QueryRowContext(ctx, "SELECT COALESCE(("+x.SQL+"), FALSE), "+invalid).Scan(&ok, &bad); err != nil {
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			return plugin.SupportReport{}, fmt.Errorf("analytical instance semantic probe: %w", err)
		}
		if !ok || bad {
			return plugin.SupportReport{Diagnostics: []plugin.SupportDiagnostic{{Code: check.code}}}, nil
		}
	}
	return plugin.SupportReport{Supported: true}, nil
}
