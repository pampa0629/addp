package sqlcompile

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

type CalendarParts struct{ Year, Month, Day string }

// CalendarDialect supplies native primitives, not the parsing rules. Shape
// must require exactly ten ASCII YYYY-MM-DD characters. CastDate only receives
// already guarded input; native lenient parsing must never choose the result.
type CalendarDialect interface {
	IntegerDialect
	ISODateShape(string) string
	TextSlice(value string, start, length int) string
	DateParts(string) CalendarParts
	CastDate(string) string
	MonthStart(string) string
	// ShiftMonths preserves the day number, clipping only when the target month
	// is shorter. Its date and month offset have already been range guarded.
	ShiftMonths(date, months string) string
}

// CalendarDate shares Literal's strict calendar domain. A NULL placeholder
// remains paired with its failure predicate until the enclosing node checks it.
func CalendarDate(input CheckedExpression, d CalendarDialect) (CheckedExpression, error) {
	if d == nil {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	if err := validateExpressions(input); err != nil {
		return CheckedExpression{}, err
	}
	if input.Type != datatype.FieldTypeString && input.Type != datatype.FieldTypeDate {
		return CheckedExpression{}, plugin.ErrAnalyticalUnsupported
	}
	b := &scalarBuilder{}
	var parts CalendarParts
	shape := "TRUE"
	if input.Type == datatype.FieldTypeString {
		shape = d.ISODateShape(input.SQL)
		digits := func(start, length int) string {
			// Guard inside the CAST as well as its caller. A database must
			// never try to parse an invalid constant while folding expressions.
			return d.CastInteger(b.choose(shape, d.TextSlice(input.SQL, start, length), "NULL"))
		}
		parts = CalendarParts{Year: digits(1, 4), Month: digits(6, 2), Day: digits(9, 2)}
	} else {
		parts = d.DateParts(input.SQL)
	}
	valid := b.binary(shape, "AND", calendarValid(parts, b))
	value := d.CastDate(b.choose(valid, input.SQL, "NULL"))
	invalid := b.binary("("+input.SQL+") IS NOT NULL", "AND", "NOT ("+valid+")")
	if input.Invalid != "" {
		invalid = b.binary(input.Invalid, "OR", invalid)
	}
	if b.failed {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	result := CheckedExpression{SQL: value, Invalid: invalid, Type: datatype.FieldTypeDate}
	if err := validateExpressions(result); err != nil {
		return CheckedExpression{}, err
	}
	return result, nil
}

func calendarValid(p CalendarParts, b *scalarBuilder) string {
	leap := b.binary(b.binary(p.Year, "%", "4"), "=", "0") + " AND (" + b.binary(b.binary(p.Year, "%", "100"), "<>", "0") + " OR " + b.binary(b.binary(p.Year, "%", "400"), "=", "0") + ")"
	days := b.choose(b.binary(p.Month, "=", "2"), b.choose(leap, "29", "28"), b.choose("("+p.Month+") IN (4,6,9,11)", "30", "31"))
	return "(" + p.Year + ") BETWEEN 1 AND 9999 AND (" + p.Month + ") BETWEEN 1 AND 12 AND (" + p.Day + ") BETWEEN 1 AND (" + days + ")"
}

func CalendarMonthStart(input CheckedExpression, d CalendarDialect) (CheckedExpression, error) {
	if input.Type != datatype.FieldTypeDate {
		return CheckedExpression{}, plugin.ErrAnalyticalUnsupported
	}
	date, err := CalendarDate(input, d)
	if err != nil {
		return CheckedExpression{}, err
	}
	date.SQL = d.MonthStart(date.SQL)
	if err := validateExpressions(date); err != nil {
		return CheckedExpression{}, err
	}
	return date, nil
}

// CalendarAddMonths checks the offset BEFORE native arithmetic. Comparing
// against the two safe differences avoids adding an arbitrary int64 offset to
// the source month index, which itself could overflow before validation.
func CalendarAddMonths(input, months CheckedExpression, d CalendarDialect) (CheckedExpression, error) {
	if err := validateExpressions(input, months); err != nil {
		return CheckedExpression{}, err
	}
	if input.Type != datatype.FieldTypeDate || (months.Type != datatype.FieldTypeInt && months.Type != datatype.FieldTypeBigInt) {
		return CheckedExpression{}, plugin.ErrAnalyticalUnsupported
	}
	date, err := CalendarDate(input, d)
	if err != nil {
		return CheckedExpression{}, err
	}
	b := &scalarBuilder{}
	// Extract from the total input expression, not the expanded guarded CAST.
	// Validation remains in date.SQL/date.Invalid; repeating it in each month
	// bound would multiply nested expression size and exhaust AST budgets.
	p := d.DateParts(input.SQL)
	index := b.binary(b.binary(p.Year, "*", "12"), "+", b.binary(p.Month, "-", "1"))
	// year*12+(month-1): January 0001 is 12; December 9999 is 119999.
	valid := b.binary(b.binary(months.SQL, ">=", b.binary("12", "-", index)), "AND", b.binary(months.SQL, "<=", b.binary("119999", "-", index)))
	safeMonths := d.CastInteger(b.choose(valid, months.SQL, "NULL"))
	value := d.ShiftMonths(date.SQL, safeMonths)
	present := b.binary("("+input.SQL+") IS NOT NULL", "AND", "("+months.SQL+") IS NOT NULL")
	invalid := b.binary(present, "AND", "NOT ("+valid+")")
	for _, inherited := range []string{date.Invalid, months.Invalid} {
		if inherited != "" {
			invalid = b.binary(inherited, "OR", invalid)
		}
	}
	if b.failed {
		return CheckedExpression{}, plugin.ErrAnalyticalInvalid
	}
	result := CheckedExpression{SQL: value, Invalid: invalid, Type: datatype.FieldTypeDate}
	if err := validateExpressions(result); err != nil {
		return CheckedExpression{}, err
	}
	return result, nil
}
