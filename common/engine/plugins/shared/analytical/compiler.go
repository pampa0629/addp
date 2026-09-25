package analytical

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/query/sqlcompile"
)

// MySQLCompatibleCompilerOptions describes the physical facts that remain
// engine-owned while sharing MySQL-compatible relational SQL semantics.
type MySQLCompatibleCompilerOptions struct {
	CompilerID     plugin.CompilerIdentity
	CatalogModel   plugin.EngineCatalogModelSpec
	IsSystemSchema func(string) bool
}

func NewMySQLCompatibleCompiler(opts MySQLCompatibleCompilerOptions) plugin.AnalyticalCompiler {
	return sqlcompile.RelationalCompiler{
		CompilerID: opts.CompilerID,
		Expression: MySQLCompatibleExpressionDialect{},
		Result:     mySQLCompatibleResultDialect{},
		Scan:       MySQLCompatibleScanDialect{CatalogModel: opts.CatalogModel, IsSystemSchema: opts.IsSystemSchema},
	}
}

func NewMySQLCompatibleExpressionDialect() sqlcompile.ExpressionDialect {
	return MySQLCompatibleExpressionDialect{}
}

type mySQLCompatibleIntegerDialect struct{}

func (mySQLCompatibleIntegerDialect) TruncateDecimal(value string) string {
	return "TRUNCATE(" + value + ", 0)"
}
func (mySQLCompatibleIntegerDialect) CastInteger(value string) string {
	return "CAST(" + value + " AS signed)"
}

type mySQLCompatibleArithmeticDialect struct{ mySQLCompatibleIntegerDialect }

func (mySQLCompatibleArithmeticDialect) CastDecimal(value string, precision, scale int) string {
	return fmt.Sprintf("CAST(%s AS decimal(%d,%d))", value, precision, scale)
}
func (d mySQLCompatibleArithmeticDialect) QuotientEstimate(numerator, denominator string) string {
	return "(" + d.CastDecimal(numerator, 50, 30) + " / " + d.CastDecimal(denominator, 38, 18) + ")"
}

type mySQLCompatibleCalendarDialect struct{ mySQLCompatibleIntegerDialect }

func (mySQLCompatibleCalendarDialect) ISODateShape(value string) string {
	return "(char_length(" + value + ") = 10 AND (" + value + ") REGEXP '^[0-9]{4}-[0-9]{2}-[0-9]{2}$')"
}
func (mySQLCompatibleCalendarDialect) TextSlice(value string, start, length int) string {
	return fmt.Sprintf("SUBSTRING(%s, %d, %d)", value, start, length)
}
func (d mySQLCompatibleCalendarDialect) DateParts(value string) sqlcompile.CalendarParts {
	return sqlcompile.CalendarParts{Year: d.CastInteger("EXTRACT(YEAR FROM " + value + ")"), Month: d.CastInteger("EXTRACT(MONTH FROM " + value + ")"), Day: d.CastInteger("EXTRACT(DAY FROM " + value + ")")}
}
func (mySQLCompatibleCalendarDialect) CastDate(value string) string {
	return "CAST(" + value + " AS date)"
}
func (mySQLCompatibleCalendarDialect) MonthStart(value string) string {
	return "CAST(DATE_FORMAT(" + value + ", '%Y-%m-01') AS date)"
}
func (mySQLCompatibleCalendarDialect) ShiftMonths(date, months string) string {
	return "CAST(DATE_ADD(" + date + ", INTERVAL " + months + " MONTH) AS date)"
}

type MySQLCompatibleExpressionDialect struct {
	mySQLCompatibleArithmeticDialect
	mySQLCompatibleCalendarDialect
	mySQLCompatibleResultDialect
}

func (MySQLCompatibleExpressionDialect) CastInteger(value string) string {
	return mySQLCompatibleIntegerDialect{}.CastInteger(value)
}
func (MySQLCompatibleExpressionDialect) TruncateDecimal(value string) string {
	return mySQLCompatibleIntegerDialect{}.TruncateDecimal(value)
}
func (MySQLCompatibleExpressionDialect) Contains(value, substring string) string {
	return "(LOCATE(CAST(" + substring + " AS binary), CAST(" + value + " AS binary)) > 0)"
}
func (MySQLCompatibleExpressionDialect) Value(value string, typ datatype.FieldType) (string, error) {
	var native string
	switch typ {
	case datatype.FieldTypeString:
		native = "char CHARACTER SET utf8mb4"
	case datatype.FieldTypeBool:
		return "(CAST(" + value + " AS signed) <> 0)", nil
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		native = "signed"
	case datatype.FieldTypeDecimal:
		native = "decimal(38,18)"
	case datatype.FieldTypeDate:
		native = "date"
	default:
		return "", plugin.ErrAnalyticalUnsupported
	}
	return "CAST(" + value + " AS " + native + ")", nil
}
func (d MySQLCompatibleExpressionDialect) Literal(value plan.Literal) (string, error) {
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
func (MySQLCompatibleExpressionDialect) Comparable(value string, typ datatype.FieldType) (string, error) {
	if typ == datatype.FieldTypeString {
		return "CAST(" + value + " AS binary)", nil
	}
	return value, nil
}
func (MySQLCompatibleExpressionDialect) TrimDecimalText(value string) string {
	return "TRIM(TRAILING '.' FROM TRIM(TRAILING '0' FROM " + value + "))"
}
func (MySQLCompatibleExpressionDialect) ISODateText(value string) string {
	return "DATE_FORMAT(" + value + ", '%Y-%m-%d')"
}

type mySQLCompatibleResultDialect struct{}

func (mySQLCompatibleResultDialect) QuoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
func (mySQLCompatibleResultDialect) TypedNull(f datatype.FieldInfo) (string, error) {
	var native string
	switch f.Type {
	case datatype.FieldTypeString:
		native = "char CHARACTER SET utf8mb4"
	case datatype.FieldTypeBool, datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		native = "signed"
	case datatype.FieldTypeDecimal:
		native = "decimal(38,18)"
	case datatype.FieldTypeDate:
		native = "date"
	default:
		return "", plugin.ErrAnalyticalUnsupported
	}
	return "CAST(NULL AS " + native + ")", nil
}
func (mySQLCompatibleResultDialect) OrderTerms(column string, f datatype.FieldInfo, key plan.SortKey) ([]string, error) {
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

type MySQLCompatibleScanDialect struct {
	CatalogModel   plugin.EngineCatalogModelSpec
	IsSystemSchema func(string) bool
}

var (
	mySQLCompatibleDecimal = regexp.MustCompile(`^decimal\(([1-9][0-9]*),([0-9]+)\)$`)
	mySQLCompatibleVarchar = regexp.MustCompile(`^varchar\([1-9][0-9]*\)$`)
)

func mySQLCompatibleName(s string) bool {
	return s != "" && utf8.RuneCountInString(s) <= 64 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func (MySQLCompatibleScanDialect) ValidateSchema(fields []datatype.FieldInfo) error {
	seen := map[string]bool{}
	for _, f := range fields {
		name := strings.ToLower(f.Name)
		if !mySQLCompatibleName(f.Name) || seen[name] {
			return plugin.ErrAnalyticalUnsupported
		}
		seen[name] = true
	}
	return nil
}
func (d MySQLCompatibleScanDialect) Table(source plugin.SourceBinding) (string, error) {
	if d.IsSystemSchema == nil {
		return "", plugin.ErrAnalyticalInvalid
	}
	return sqlcompile.TabularScanTable(source, d.CatalogModel, mySQLCompatibleResultDialect{}.QuoteIdentifier, mySQLCompatibleName, d.IsSystemSchema)
}
func (MySQLCompatibleScanDialect) Column(c plugin.ColumnBinding, alias string) (sqlcompile.CheckedExpression, error) {
	f := c.Field
	if len(f.Path) != 1 || f.Path[0] != f.Name || !mySQLCompatibleName(f.Name) || !mySQLCompatibleName(alias) {
		return sqlcompile.CheckedExpression{}, plugin.ErrAnalyticalUnsupported
	}
	d := MySQLCompatibleExpressionDialect{}
	value := d.QuoteIdentifier(alias) + "." + d.QuoteIdentifier(f.Name)
	switch f.Type {
	case datatype.FieldTypeInt:
		if f.NativeType == "tinyint" || f.NativeType == "smallint" || f.NativeType == "mediumint" || f.NativeType == "int" {
			return sqlcompile.CheckedExpression{SQL: value, Type: f.Type}, nil
		}
	case datatype.FieldTypeBigInt:
		if f.NativeType == "bigint" {
			return sqlcompile.CheckedExpression{SQL: value, Type: f.Type}, nil
		}
	case datatype.FieldTypeBool:
		if f.NativeType == "tinyint(1)" {
			valid := "(" + value + ") IN (0,1)"
			sql, err := d.Value("CASE WHEN "+valid+" THEN "+value+" ELSE NULL END", f.Type)
			return sqlcompile.CheckedExpression{SQL: sql, Invalid: "(" + value + ") IS NOT NULL AND NOT (" + valid + ")", Type: f.Type}, err
		}
	case datatype.FieldTypeString:
		if f.NativeType == "text" || f.NativeType == "tinytext" || f.NativeType == "mediumtext" || f.NativeType == "longtext" || mySQLCompatibleVarchar.MatchString(f.NativeType) {
			sql, err := d.Value(value, f.Type)
			return sqlcompile.CheckedExpression{SQL: sql, Type: f.Type}, err
		}
	case datatype.FieldTypeDate:
		if f.NativeType == "date" {
			return sqlcompile.ScanDate(value, d)
		}
	case datatype.FieldTypeDecimal:
		if m := mySQLCompatibleDecimal.FindStringSubmatch(f.NativeType); m != nil {
			precision, e1 := strconv.Atoi(m[1])
			scale, e2 := strconv.Atoi(m[2])
			if e1 == nil && e2 == nil && sqlcompile.DecimalSourceFits(f, precision, scale) {
				return sqlcompile.ScanDecimal(value, d)
			}
		}
	}
	return sqlcompile.CheckedExpression{}, sqlcompile.ErrUnsupportedSourceFieldType
}
