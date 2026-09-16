package sqlcompile

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// IntegerDialect provides native primitives without an engine-name dispatch.
// TruncateDecimal is used solely to test integrality, never to choose a result.
type IntegerDialect interface {
	TruncateDecimal(string) string
	CastInteger(string) string
}

// LosslessInteger compiles the integer operation's full value/error pair. The
// guarded CAST cannot round a fraction, clamp overflow or create native warnings.
func LosslessInteger(input CheckedExpression, dialect IntegerDialect) (CheckedExpression, error) {
	if dialect == nil {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	if err := validateExpressions(input); err != nil {
		return CheckedExpression{}, err
	}
	result := CheckedExpression{Type: datatype.FieldTypeBigInt, Invalid: input.Invalid}
	switch input.Type {
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		result.SQL = dialect.CastInteger(input.SQL)
	case datatype.FieldTypeDecimal:
		x := "(" + input.SQL + ")"
		valid := x + " >= -9223372036854775808 AND " + x + " <= 9223372036854775807 AND " + x + " = " + dialect.TruncateDecimal(x)
		result.SQL = "CASE WHEN " + valid + " THEN " + dialect.CastInteger(x) + " ELSE " + dialect.CastInteger("NULL") + " END"
		invalid := "(" + x + " IS NOT NULL AND NOT (" + valid + "))"
		if input.Invalid != "" {
			invalid = "(" + input.Invalid + ") OR " + invalid
		}
		result.Invalid = invalid
	default:
		return CheckedExpression{}, plugin.ErrAnalyticalUnsupported
	}
	if len(result.SQL)+len(result.Invalid) > plan.MaxBytes {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	return result, nil
}
