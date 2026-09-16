package mysql

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// Result syntax does not by itself certify a complete analytical compiler.
type analyticalResultDialect struct{}

func (analyticalResultDialect) QuoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
func (analyticalResultDialect) TypedNull(f datatype.FieldInfo) (string, error) {
	var typ string
	switch f.Type {
	case datatype.FieldTypeString:
		typ = "char CHARACTER SET utf8mb4"
	case datatype.FieldTypeBool, datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		typ = "signed"
	case datatype.FieldTypeDecimal:
		typ = "decimal(38,18)"
	case datatype.FieldTypeDate:
		typ = "date"
	default:
		return "", plugin.ErrAnalyticalUnsupported
	}
	return "CAST(NULL AS " + typ + ")", nil
}
func (analyticalResultDialect) OrderTerms(column string, f datatype.FieldInfo, key plan.SortKey) ([]string, error) {
	nullDirection := "ASC"
	if key.Nulls == "first" {
		nullDirection = "DESC"
	}
	value := column
	if f.Type == datatype.FieldTypeString {
		value = "CAST(" + column + " AS binary)"
	}
	return []string{"(" + column + " IS NULL) " + nullDirection, value + " " + strings.ToUpper(key.Direction)}, nil
}
