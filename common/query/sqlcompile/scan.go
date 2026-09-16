package sqlcompile

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
)

// ScanDialect is an engine-owned compiler strategy, not an owner SQL API.
// Table validates and quotes the entire catalog leaf. Column validates native
// facts and reads through the compiler alias; it must return a total value.
type ScanDialect interface {
	ValidateSchema([]datatype.FieldInfo) error
	Table(plugin.SourceBinding) (string, error)
	Column(plugin.ColumnBinding, string) (CheckedExpression, error)
}

// DecimalSourceFits rejects declarations that could require rounding or lose
// integer digits when converted to the neutral DECIMAL(38,18) domain.
func DecimalSourceFits(f datatype.FieldInfo, precision, scale int) bool {
	return precision > 0 && scale >= 0 && scale <= 18 && precision-scale <= 20 && scale <= precision && f.Precision == precision && f.Scale == scale
}

func ScanDecimal(value string, d ExpressionDialect) (CheckedExpression, error) {
	valid := "(" + value + ") >= -99999999999999999999.999999999999999999 AND (" + value + ") <= 99999999999999999999.999999999999999999"
	sql, err := d.Value("CASE WHEN "+valid+" THEN "+value+" ELSE NULL END", datatype.FieldTypeDecimal)
	if err != nil {
		return CheckedExpression{}, err
	}
	return CheckedExpression{SQL: sql, Invalid: "(" + value + ") IS NOT NULL AND NOT (" + valid + ")", Type: datatype.FieldTypeDecimal}, nil
}

func ScanDate(value string, d ExpressionDialect) (CheckedExpression, error) {
	// Guard the native range before extracting date parts. PG infinity is a
	// legal native DATE but cannot be extracted then cast to a finite integer.
	lo, err := d.Literal(plan.Literal{Type: datatype.FieldTypeDate, Text: "0001-01-01"})
	if err != nil {
		return CheckedExpression{}, err
	}
	hi, err := d.Literal(plan.Literal{Type: datatype.FieldTypeDate, Text: "9999-12-31"})
	if err != nil {
		return CheckedExpression{}, err
	}
	valid := "(" + value + ") >= " + lo + " AND (" + value + ") <= " + hi
	return CalendarDate(CheckedExpression{SQL: d.CastDate("CASE WHEN " + valid + " THEN " + value + " ELSE NULL END"), Invalid: "(" + value + ") IS NOT NULL AND NOT (" + valid + ")", Type: datatype.FieldTypeDate}, d)
}

// TabularScanTable is selected by flat SQL catalog implementations only. Other
// catalog layouts implement ScanDialect.Table without this helper.
func TabularScanTable(s plugin.SourceBinding, model plugin.EngineCatalogModelSpec, quote func(string) string, validName func(string) bool, hidden func(string) bool) (string, error) {
	p := s.Path
	if p.Version != model.PathVersion || p.EngineID == 0 || len(p.Segments) != 3 || len(model.Levels) != 2 {
		return "", plugin.ErrAnalyticalUnsupported
	}
	root, branch, leaf := p.Segments[0], p.Segments[1], p.Segments[2]
	if root.Term != model.RootTerm || root.Kind != model.RootTerm || root.Name != "" || branch.Term != model.Levels[0].Term || branch.Kind != plugin.EngineCatalogKindNamespace || leaf.Term != model.Levels[1].Term || leaf.Kind != plugin.EngineCatalogKindTable {
		return "", plugin.ErrAnalyticalUnsupported
	}
	if !validName(branch.Name) || !validName(leaf.Name) || hidden(branch.Name) {
		return "", plugin.ErrAnalyticalUnsupported
	}
	return quote(branch.Name) + "." + quote(leaf.Name), nil
}
