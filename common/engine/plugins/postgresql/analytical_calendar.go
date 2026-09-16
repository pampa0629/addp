package postgresql

import (
	"fmt"
	"github.com/addp/common/query/sqlcompile"
)

type analyticalCalendarDialect struct{ analyticalIntegerDialect }

func (analyticalCalendarDialect) ISODateShape(value string) string {
	return "(pg_catalog.char_length(" + value + ") = 10 AND " + "(" + value + ") ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'" + ")"
}
func (analyticalCalendarDialect) TextSlice(value string, start, length int) string {
	return fmt.Sprintf("pg_catalog.substr(%s, %d, %d)", value, start, length)
}
func (d analyticalCalendarDialect) DateParts(value string) sqlcompile.CalendarParts {
	return sqlcompile.CalendarParts{Year: d.CastInteger("EXTRACT(YEAR FROM " + value + ")"), Month: d.CastInteger("EXTRACT(MONTH FROM " + value + ")"), Day: d.CastInteger("EXTRACT(DAY FROM " + value + ")")}
}
func (analyticalCalendarDialect) CastDate(value string) string { return "CAST(" + value + " AS date)" }
func (analyticalCalendarDialect) MonthStart(value string) string {
	return "CAST(pg_catalog.date_trunc('month', CAST(" + value + " AS timestamp without time zone)) AS date)"
}

func (analyticalCalendarDialect) ShiftMonths(date, months string) string {
	// Timestamp without time zone keeps calendar arithmetic independent of the
	// session zone. Guarded offsets fit integer and interval's month component.
	return "CAST((CAST(" + date + " AS timestamp without time zone) + CAST(" + months + " AS integer) * INTERVAL '1 month') AS date)"
}
