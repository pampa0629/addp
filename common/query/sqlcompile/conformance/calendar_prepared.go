package conformance

import (
	"database/sql"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/query/sqlcompile"
	"testing"
)

// CalendarCases fixes the shared ISO calendar contract independently of native
// parsers. Native warning checks and PreparedQuery use exactly the same cases.
func CalendarCases() []expressionExample {
	lit := func(typ datatype.FieldType, s string) plan.Expr {
		return plan.Expr{Op: "literal", Literal: &plan.Literal{Type: typ, Text: s}}
	}
	op := func(name string, args ...plan.Expr) plan.Expr { return plan.Expr{Op: name, Args: args} }
	date := func(s string) plan.Expr { return op("date", lit(datatype.FieldTypeString, s)) }
	want := func(s string) *string { return &s }
	var cases []expressionExample
	for _, s := range []string{"0001-01-01", "0999-12-31", "1000-01-01", "9999-12-31", "2000-02-29", "2024-02-29", "1900-02-28", "2100-02-28", "2026-04-30"} {
		cases = append(cases, expressionExample{name: "date " + s, expr: date(s), want: want(s)}, expressionExample{name: "month start " + s, expr: op("month_start", lit(datatype.FieldTypeDate, s)), want: want(s[:8] + "01")})
	}
	for _, s := range []string{"", "invalid", "1900-02-29", "2100-02-29", "2026-02-29", "2026-04-31", "2026-01-00", "2026-00-01", "0000-01-01", "10000-01-01", "2026-13-01", "2026-01-32", "2026-1-01", "2026-01-1", " 2026-01-01", "2026-01-01 ", "2026-01-01\n", "2026/01/01", "2026-01-01T00:00:00", "２０２６-01-01", "2026-０１-01", "2026-01-٠١"} {
		cases = append(cases, expressionExample{name: "reject " + s, expr: date(s), invalid: true})
	}
	null := plan.Expr{Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeString, Null: true}}
	bad := date("2026-02-30")
	cases = append(cases,
		expressionExample{name: "NULL date", expr: op("date", null)},
		expressionExample{name: "NULL month", expr: op("month_start", op("date", null))},
		expressionExample{name: "month of parsed date", expr: op("month_start", date("2024-02-29")), want: want("2024-02-01")},
		expressionExample{name: "month preserves parse error", expr: op("month_start", bad), invalid: true},
		expressionExample{name: "CASE skips parse error", expr: op("case", lit(datatype.FieldTypeBool, "false"), bad, date("2026-01-01")), want: want("2026-01-01")},
		expressionExample{name: "CASE preserves parse error", expr: op("case", lit(datatype.FieldTypeBool, "true"), bad, date("2026-01-01")), invalid: true},
		expressionExample{name: "COALESCE skips parse error", expr: op("coalesce", date("2026-01-01"), bad), want: want("2026-01-01")},
		expressionExample{name: "COALESCE preserves parse error", expr: op("coalesce", bad, date("2026-01-01")), invalid: true},
		expressionExample{name: "date string parameter", expr: op("date", plan.Expr{Op: "parameter", Parameter: "value_parameter"}), parameter: &plan.Literal{Type: datatype.FieldTypeString, Text: "2000-02-29"}, want: want("2000-02-29")},
		expressionExample{name: "invalid date string parameter", expr: op("date", plan.Expr{Op: "parameter", Parameter: "value_parameter"}), parameter: &plan.Literal{Type: datatype.FieldTypeString, Text: "1900-02-29"}, invalid: true},
	)
	return append(cases, monthShiftCases()...)
}

func PreparedCalendar(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo) {
	t.Helper()
	preparedExpressionCases(t, provider, conn, CalendarCases())
}

// NativeCalendar checks that rejected calendar text never reaches a lenient
// native CAST. The caller can additionally inspect warnings on the same session.
func NativeCalendar(t *testing.T, conn *sql.Conn, d sqlcompile.ExpressionDialect, checkWarnings func(*testing.T)) {
	t.Helper()
	for _, tc := range CalendarCases() {
		if tc.parameter != nil {
			continue
		}
		t.Run("native/"+tc.name, func(t *testing.T) {
			e, err := sqlcompile.CompileExpression(tc.expr, sqlcompile.ExpressionScope{}, d)
			if err != nil {
				t.Fatal(err)
			}
			invalidSQL := e.Invalid
			if invalidSQL == "" {
				invalidSQL = "FALSE"
			}
			textSQL, err := d.Value(e.SQL, datatype.FieldTypeString)
			if err != nil {
				t.Fatal(err)
			}
			var value sql.NullString
			var invalid bool
			if err = conn.QueryRowContext(t.Context(), "SELECT "+textSQL+", "+invalidSQL).Scan(&value, &invalid); err != nil {
				t.Fatal(err)
			}
			if invalid != tc.invalid {
				t.Fatalf("invalid=%t want=%t", invalid, tc.invalid)
			}
			if tc.invalid {
				// Only the check is observable for failed expressions. COALESCE may
				// produce a non-NULL placeholder that execution must never expose.
			} else if tc.want == nil {
				if value.Valid {
					t.Fatalf("expected NULL, got %s", value.String)
				}
			} else if !value.Valid || value.String != *tc.want {
				t.Fatalf("got %#v want %s", value, *tc.want)
			}
			if checkWarnings != nil {
				checkWarnings(t)
			}
		})
	}
}

// Expected dates are explicit calendar examples, not native database results.
func monthShiftCases() []expressionExample {
	lit := func(typ datatype.FieldType, s string) plan.Expr {
		return plan.Expr{Op: "literal", Literal: &plan.Literal{Type: typ, Text: s}}
	}
	op := func(name string, args ...plan.Expr) plan.Expr { return plan.Expr{Op: name, Args: args} }
	date := func(s string) plan.Expr { return lit(datatype.FieldTypeDate, s) }
	months := func(s string) plan.Expr { return lit(datatype.FieldTypeBigInt, s) }
	shift := func(d plan.Expr, n string) plan.Expr { return op("add_months", d, months(n)) }
	want := func(s string) *string { return &s }
	var cases []expressionExample
	for _, tc := range []struct{ date, months, want string }{
		{"2026-01-15", "1", "2026-02-15"},
		{"2026-01-31", "1", "2026-02-28"},
		{"2026-02-28", "1", "2026-03-28"},
		{"2024-01-31", "1", "2024-02-29"},
		{"2024-02-29", "1", "2024-03-29"},
		{"2024-02-29", "12", "2025-02-28"},
		{"2026-04-30", "1", "2026-05-30"},
		{"2026-12-31", "1", "2027-01-31"},
		{"2026-01-31", "-1", "2025-12-31"},
		{"2026-03-31", "-1", "2026-02-28"},
		{"2024-03-31", "-1", "2024-02-29"},
		{"2026-01-31", "2", "2026-03-31"},
		{"2000-02-29", "-1200", "1900-02-28"},
		{"2000-02-29", "1200", "2100-02-28"},
		{"0001-01-01", "0", "0001-01-01"},
		{"9999-12-31", "0", "9999-12-31"},
		{"0001-01-31", "119987", "9999-12-31"},
		{"9999-12-31", "-119987", "0001-01-31"},
		{"2026-01-31", "0", "2026-01-31"},
		{"0001-01-01", "-1", ""},
		{"9999-12-31", "1", ""},
		{"0001-01-01", "119988", ""},
		{"9999-12-31", "-119988", ""},
		{"2026-01-01", "9223372036854775807", ""},
		{"2026-01-01", "-9223372036854775808", ""},
	} {
		e := expressionExample{name: "shift " + tc.date + " by " + tc.months, expr: shift(date(tc.date), tc.months), invalid: tc.want == ""}
		if !e.invalid {
			e.want = want(tc.want)
		}
		cases = append(cases, e)
	}
	nullDate := plan.Expr{Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeDate, Null: true}}
	nullMonths := plan.Expr{Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeBigInt, Null: true}}
	badDate := op("date", lit(datatype.FieldTypeString, "2026-02-30"))
	badMonths := op("integer", lit(datatype.FieldTypeDecimal, "1.5"))
	badShift := shift(date("9999-12-31"), "1")
	cases = append(cases,
		expressionExample{name: "shift twice clips at each step", expr: shift(shift(date("2026-01-31"), "1"), "1"), want: want("2026-03-28")},
		expressionExample{name: "shift back cannot recover lost day", expr: shift(shift(date("2026-01-31"), "1"), "-1"), want: want("2026-01-28")},
		expressionExample{name: "shift parsed date", expr: shift(op("date", lit(datatype.FieldTypeString, "2026-01-31")), "1"), want: want("2026-02-28")},
		expressionExample{name: "shift NULL date", expr: shift(nullDate, "1")},
		expressionExample{name: "shift NULL date extreme months", expr: shift(nullDate, "9223372036854775807")},
		expressionExample{name: "shift NULL months", expr: op("add_months", date("2026-01-31"), nullMonths)},
		expressionExample{name: "shift both NULL", expr: op("add_months", nullDate, nullMonths)},
		expressionExample{name: "shift preserves invalid date with NULL months", expr: op("add_months", badDate, nullMonths), invalid: true},
		expressionExample{name: "shift preserves invalid months with NULL date", expr: op("add_months", nullDate, badMonths), invalid: true},
		expressionExample{name: "shift preserves invalid date", expr: shift(badDate, "1"), invalid: true},
		expressionExample{name: "CASE skips shift overflow", expr: op("case", lit(datatype.FieldTypeBool, "false"), badShift, date("2026-01-01")), want: want("2026-01-01")},
		expressionExample{name: "COALESCE cannot swallow shift overflow", expr: op("coalesce", badShift, date("2026-01-01")), invalid: true},
	)
	for _, offset := range []string{"1", "-1", "9223372036854775807", "-9223372036854775808"} {
		e := expressionExample{name: "shift parameter " + offset, expr: op("add_months", date("2026-01-31"), plan.Expr{Op: "parameter", Parameter: "value_parameter"}), parameter: &plan.Literal{Type: datatype.FieldTypeBigInt, Text: offset}}
		switch offset {
		case "1":
			e.want = want("2026-02-28")
		case "-1":
			e.want = want("2025-12-31")
		default:
			e.invalid = true
		}
		cases = append(cases, e)
	}
	return cases
}
