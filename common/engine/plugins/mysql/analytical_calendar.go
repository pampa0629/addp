package mysql

import (
	"fmt"
	"github.com/addp/common/query/sqlcompile"
)

type analyticalCalendarDialect struct{ analyticalIntegerDialect }

func (analyticalCalendarDialect) ISODateShape(value string) string {
	return "(char_length(" + value + ") = 10 AND " + "(" + value + ") REGEXP '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'" + ")"
}
func (analyticalCalendarDialect) TextSlice(value string, start, length int) string {
	return fmt.Sprintf("SUBSTRING(%s, %d, %d)", value, start, length)
}
func (d analyticalCalendarDialect) DateParts(value string) sqlcompile.CalendarParts {
	return sqlcompile.CalendarParts{Year: d.CastInteger("EXTRACT(YEAR FROM " + value + ")"), Month: d.CastInteger("EXTRACT(MONTH FROM " + value + ")"), Day: d.CastInteger("EXTRACT(DAY FROM " + value + ")")}
}
func (analyticalCalendarDialect) CastDate(value string) string { return "CAST(" + value + " AS date)" }
func (analyticalCalendarDialect) MonthStart(value string) string {
	return "CAST(DATE_FORMAT(" + value + ", '%Y-%m-01') AS date)"
}

func (analyticalCalendarDialect) ShiftMonths(date, months string) string {
	return "CAST(DATE_ADD(" + date + ", INTERVAL " + months + " MONTH) AS date)"
}
