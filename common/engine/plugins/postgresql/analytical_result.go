package postgresql

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// Native result syntax only; this does not advertise a complete analytical
// compiler or certify the instance's source types and encoding.
type analyticalResultDialect struct{}

func (analyticalResultDialect) QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (analyticalResultDialect) TypedNull(f datatype.FieldInfo) (string, error) {
	var typ string
	switch f.Type {
	case datatype.FieldTypeString:
		typ = "text"
	case datatype.FieldTypeBool:
		typ = "boolean"
	case datatype.FieldTypeInt:
		typ = "integer"
	case datatype.FieldTypeBigInt:
		typ = "bigint"
	case datatype.FieldTypeDecimal:
		typ = "numeric(38,18)"
	case datatype.FieldTypeDate:
		typ = "date"
	default:
		return "", plugin.ErrAnalyticalUnsupported
	}
	return "CAST(NULL AS " + typ + ")", nil
}

func (analyticalResultDialect) OrderTerms(column string, f datatype.FieldInfo, key plan.SortKey) ([]string, error) {
	if f.Type == datatype.FieldTypeString {
		column += ` COLLATE "C"`
	}
	return []string{column + " " + strings.ToUpper(key.Direction) + " NULLS " + strings.ToUpper(key.Nulls)}, nil
}
