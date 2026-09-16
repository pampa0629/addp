package mysql

import (
	"encoding/hex"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// Native scalar rendering only; full compiler capability remains unregistered.
type analyticalExpressionDialect struct {
	analyticalArithmeticDialect
	analyticalResultDialect
	analyticalCalendarDialect
}

func (analyticalExpressionDialect) CastInteger(v string) string {
	return analyticalIntegerDialect{}.CastInteger(v)
}
func (analyticalExpressionDialect) TruncateDecimal(v string) string {
	return analyticalIntegerDialect{}.TruncateDecimal(v)
}

func (analyticalExpressionDialect) Value(sql string, typ datatype.FieldType) (string, error) {
	var native string
	switch typ {
	case datatype.FieldTypeString:
		native = "char CHARACTER SET utf8mb4"
	case datatype.FieldTypeBool:
		return "(CAST(" + sql + " AS signed) <> 0)", nil
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		native = "signed"
	case datatype.FieldTypeDecimal:
		native = "decimal(38,18)"
	case datatype.FieldTypeDate:
		native = "date"
	default:
		return "", plugin.ErrAnalyticalUnsupported
	}
	return "CAST(" + sql + " AS " + native + ")", nil
}
func (d analyticalExpressionDialect) Literal(value plan.Literal) (string, error) {
	v, err := value.Canonical()
	if err != nil {
		return "", plugin.ErrAnalyticalInvalid
	}
	if v.Null {
		return d.Value("NULL", v.Type)
	}
	if v.Type == datatype.FieldTypeString {
		return "CONVERT(X'" + hex.EncodeToString([]byte(v.Text)) + "' USING utf8mb4)", nil
	}
	sql := v.Text
	if v.Type == datatype.FieldTypeDate {
		sql = "'" + v.Text + "'"
	}
	return d.Value(sql, v.Type)
}
func (analyticalExpressionDialect) Comparable(sql string, typ datatype.FieldType) (string, error) {
	if typ == datatype.FieldTypeString {
		return "CAST(" + sql + " AS binary)", nil
	}
	return sql, nil
}
