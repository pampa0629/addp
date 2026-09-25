package postgresql

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile"
)

// This renderer does not register a production analytical capability. UTF-8
// database encoding remains an instance-certification prerequisite for text.
type analyticalScanDialect struct{}

func postgresAnalyticalName(s string) bool {
	return s != "" && len(s) <= 63 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func (analyticalScanDialect) ValidateSchema(fields []datatype.FieldInfo) error {
	seen := map[string]bool{}
	for _, f := range fields {
		if !postgresAnalyticalName(f.Name) || seen[f.Name] {
			return plugin.ErrAnalyticalUnsupported
		}
		seen[f.Name] = true
	}
	return nil
}
func (analyticalScanDialect) Table(s plugin.SourceBinding) (string, error) {
	p := &PostgreSQLPlugin{}
	return sqlcompile.TabularScanTable(s, p.EngineCatalogModel(), analyticalExpressionDialect{}.QuoteIdentifier, postgresAnalyticalName, p.isSystemSchema)
}

var postgresAnalyticalNumeric = regexp.MustCompile(`^numeric\(([1-9][0-9]*),([0-9]+)\)$`)
var postgresAnalyticalVarchar = regexp.MustCompile(`^character varying\([1-9][0-9]*\)$`)

func (analyticalScanDialect) Column(c plugin.ColumnBinding, alias string) (sqlcompile.CheckedExpression, error) {
	f := c.Field
	if len(f.Path) != 1 || f.Path[0] != f.Name || !postgresAnalyticalName(f.Name) || !postgresAnalyticalName(alias) {
		return sqlcompile.CheckedExpression{}, plugin.ErrAnalyticalUnsupported
	}
	d := analyticalExpressionDialect{}
	value := d.QuoteIdentifier(alias) + "." + d.QuoteIdentifier(f.Name)
	native := f.NativeType
	supported := false
	switch f.Type {
	case datatype.FieldTypeInt:
		supported = native == "smallint" || native == "integer"
	case datatype.FieldTypeBigInt:
		supported = native == "bigint"
	case datatype.FieldTypeBool:
		supported = native == "boolean"
	case datatype.FieldTypeString:
		supported = native == "text" || native == "character varying" || postgresAnalyticalVarchar.MatchString(native)
	case datatype.FieldTypeDate:
		if native == "date" {
			return sqlcompile.ScanDate(value, d)
		}
	case datatype.FieldTypeDecimal:
		if m := postgresAnalyticalNumeric.FindStringSubmatch(native); m != nil {
			precision, e1 := strconv.Atoi(m[1])
			scale, e2 := strconv.Atoi(m[2])
			if e1 == nil && e2 == nil && sqlcompile.DecimalSourceFits(f, precision, scale) {
				return sqlcompile.ScanDecimal(value, d)
			}
		}
	}
	if !supported {
		return sqlcompile.CheckedExpression{}, sqlcompile.ErrUnsupportedSourceFieldType
	}
	return sqlcompile.CheckedExpression{SQL: value, Type: f.Type}, nil
}
