package sqlcompile

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// CanonicalText uses Literal's value representation, not a native display
// format. Its input is already typed and total; inherited failures remain
// attached even if a failed child has a non-NULL internal placeholder.
func CanonicalText(input CheckedExpression, d ExpressionDialect) (CheckedExpression, error) {
	if d == nil {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	if err := validateExpressions(input); err != nil {
		return CheckedExpression{}, err
	}
	out := CheckedExpression{Type: datatype.FieldTypeString, Invalid: input.Invalid}
	var err error
	switch input.Type {
	case datatype.FieldTypeString:
		out.SQL = input.SQL
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		out.SQL, err = d.Value(d.CastInteger(input.SQL), datatype.FieldTypeString)
	case datatype.FieldTypeDecimal:
		// Always normalize to fixed scale before trimming. Trimming the raw
		// text "100" would otherwise destroy integer zeros.
		out.SQL, err = d.Value(d.CastDecimal(input.SQL, 38, 18), datatype.FieldTypeString)
		if err == nil {
			out.SQL = d.TrimDecimalText(out.SQL)
		}
	case datatype.FieldTypeDate:
		out.SQL = d.ISODateText(input.SQL)
	case datatype.FieldTypeBool:
		trueText, trueErr := d.Literal(plan.Literal{Type: datatype.FieldTypeString, Text: "true"})
		falseText, falseErr := d.Literal(plan.Literal{Type: datatype.FieldTypeString, Text: "false"})
		if trueErr != nil {
			return CheckedExpression{}, trueErr
		}
		if falseErr != nil {
			return CheckedExpression{}, falseErr
		}
		out.SQL = "CASE WHEN (" + input.SQL + ") IS NULL THEN NULL WHEN " + input.SQL + " THEN " + trueText + " ELSE " + falseText + " END"
	default:
		return CheckedExpression{}, plugin.ErrAnalyticalUnsupported
	}
	if err != nil {
		return CheckedExpression{}, err
	}
	if err := validateExpressions(out); err != nil {
		return CheckedExpression{}, err
	}
	return out, nil
}
