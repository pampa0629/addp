package mysql

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile"
)

type analyticalScanDialect struct{}

func mysqlAnalyticalName(s string) bool {
	return s != "" && utf8.RuneCountInString(s) <= 64 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func (analyticalScanDialect) ValidateSchema(fields []datatype.FieldInfo) error {
	seen := map[string]bool{}
	for _, f := range fields {
		name := strings.ToLower(f.Name)
		if !mysqlAnalyticalName(f.Name) || seen[name] {
			return plugin.ErrAnalyticalUnsupported
		}
		seen[name] = true
	}
	return nil
}
func (analyticalScanDialect) Table(s plugin.SourceBinding) (string, error) {
	p := &MySQLPlugin{}
	return sqlcompile.TabularScanTable(s, p.EngineCatalogModel(), analyticalExpressionDialect{}.QuoteIdentifier, mysqlAnalyticalName, p.isSystemSchema)
}

var mysqlAnalyticalDecimal = regexp.MustCompile(`^decimal\(([1-9][0-9]*),([0-9]+)\)$`)
var mysqlAnalyticalVarchar = regexp.MustCompile(`^varchar\([1-9][0-9]*\)$`)

func (analyticalScanDialect) Column(c plugin.ColumnBinding, alias string) (sqlcompile.CheckedExpression, error) {
	f := c.Field
	if len(f.Path) != 1 || f.Path[0] != f.Name || !mysqlAnalyticalName(f.Name) || !mysqlAnalyticalName(alias) {
		return sqlcompile.CheckedExpression{}, plugin.ErrAnalyticalUnsupported
	}
	d := analyticalExpressionDialect{}
	value := d.QuoteIdentifier(alias) + "." + d.QuoteIdentifier(f.Name)
	native := f.NativeType
	switch f.Type {
	case datatype.FieldTypeInt:
		if native == "tinyint" || native == "smallint" || native == "mediumint" || native == "int" {
			return sqlcompile.CheckedExpression{SQL: value, Type: f.Type}, nil
		}
	case datatype.FieldTypeBigInt:
		if native == "bigint" {
			return sqlcompile.CheckedExpression{SQL: value, Type: f.Type}, nil
		}
	case datatype.FieldTypeBool:
		if native == "tinyint(1)" {
			valid := "(" + value + ") IN (0,1)"
			sql, err := d.Value("CASE WHEN "+valid+" THEN "+value+" ELSE NULL END", f.Type)
			return sqlcompile.CheckedExpression{SQL: sql, Invalid: "(" + value + ") IS NOT NULL AND NOT (" + valid + ")", Type: f.Type}, err
		}
	case datatype.FieldTypeString:
		if native == "text" || native == "tinytext" || native == "mediumtext" || native == "longtext" || mysqlAnalyticalVarchar.MatchString(native) {
			sql, err := d.Value(value, f.Type)
			return sqlcompile.CheckedExpression{SQL: sql, Type: f.Type}, err
		}
	case datatype.FieldTypeDate:
		if native == "date" {
			return sqlcompile.ScanDate(value, d)
		}
	case datatype.FieldTypeDecimal:
		if m := mysqlAnalyticalDecimal.FindStringSubmatch(native); m != nil {
			precision, e1 := strconv.Atoi(m[1])
			scale, e2 := strconv.Atoi(m[2])
			if e1 == nil && e2 == nil && sqlcompile.DecimalSourceFits(f, precision, scale) {
				return sqlcompile.ScanDecimal(value, d)
			}
		}
	}
	return sqlcompile.CheckedExpression{}, plugin.ErrAnalyticalUnsupported
}
