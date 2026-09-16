package postgresql

import (
	"encoding/hex"
	"strings"

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
		native = "text"
	case datatype.FieldTypeBool:
		native = "boolean"
	case datatype.FieldTypeInt:
		native = "integer"
	case datatype.FieldTypeBigInt:
		native = "bigint"
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
		if strings.ContainsRune(v.Text, 0) {
			return "", plugin.ErrAnalyticalUnsupported
		}
		return "pg_catalog.convert_from(pg_catalog.decode('" + hex.EncodeToString([]byte(v.Text)) + "', 'hex'), 'UTF8')", nil
	}
	sql := v.Text
	if v.Type == datatype.FieldTypeDate {
		sql = "'" + v.Text + "'"
	}
	return d.Value(sql, v.Type)
}
func (analyticalExpressionDialect) Comparable(sql string, typ datatype.FieldType) (string, error) {
	if typ == datatype.FieldTypeString {
		return "(" + sql + `) COLLATE "C"`, nil
	}
	return sql, nil
}

func (analyticalExpressionDialect) TrimDecimalText(value string) string {
	return "pg_catalog.rtrim(pg_catalog.rtrim(" + value + ", '0'), '.')"
}

func (analyticalExpressionDialect) ISODateText(value string) string {
	return "pg_catalog.to_char(CAST(" + value + " AS timestamp without time zone), 'YYYY-MM-DD')"
}
