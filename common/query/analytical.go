package query

import (
	"fmt"
	"strconv"
	"strings"
)

// AnalyticalDialect renders engine-native expressions, never business formulas.
// Inputs are compiler-owned SQL fragments; user values remain named parameters.
type AnalyticalDialect struct{ Dialect }

func NewAnalyticalDialect(name string) (AnalyticalDialect, error) {
	switch name {
	case DialectPostgreSQL, DialectMySQL:
		return AnalyticalDialect{ForDialect(name)}, nil
	default:
		return AnalyticalDialect{}, fmt.Errorf("analytical SQL expressions are unavailable for dialect %q", name)
	}
}

func (d AnalyticalDialect) Date(expr string) string    { return "CAST(" + expr + " AS date)" }
func (d AnalyticalDialect) Decimal(expr string) string { return "CAST(" + expr + " AS decimal(38,18))" }
func (d AnalyticalDialect) Integer(expr string) string {
	if d.Name() == DialectMySQL {
		return "CAST(" + expr + " AS signed)"
	}
	return "CAST(" + expr + " AS bigint)"
}
func (d AnalyticalDialect) Text(expr string) string {
	if d.Name() == DialectMySQL {
		return "CAST(" + expr + " AS char)"
	}
	return "CAST(" + expr + " AS text)"
}

// ExactText preserves identity equality independently of database collation.
func (d AnalyticalDialect) ExactText(expr string) string {
	if d.Name() == DialectMySQL {
		return "CAST(" + expr + " AS binary)"
	}
	return "(" + expr + ") COLLATE \"C\""
}
func (d AnalyticalDialect) MonthStart(expr string) string {
	if d.Name() == DialectMySQL {
		return d.Date("DATE_FORMAT(" + expr + ", '%Y-%m-01')")
	}
	return d.Date("date_trunc('month', " + expr + ")")
}
func (d AnalyticalDialect) AddMonths(expr, count string) string {
	if d.Name() == DialectMySQL {
		return d.Date("DATE_ADD(" + expr + ", INTERVAL " + count + " MONTH)")
	}
	return d.Date("(" + expr + ") + (" + count + ") * INTERVAL '1 month'")
}

// IntegerRows is a bounded constant relation, not an external/table-function source.
func (d AnalyticalDialect) IntegerRows(column string, count int) string {
	rows := make([]string, count)
	for i := range rows {
		rows[i] = "SELECT " + strconv.Itoa(i)
		if i == 0 {
			rows[i] += " AS " + d.QuoteIdentifier(column)
		}
	}
	return "(" + strings.Join(rows, " UNION ALL ") + ")"
}
