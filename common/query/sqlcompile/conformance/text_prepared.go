package conformance

import (
	"database/sql"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/query/sqlcompile"
)

func textCases() []expressionExample {
	lit := func(typ datatype.FieldType, s string) plan.Expr {
		return plan.Expr{Op: "literal", Literal: &plan.Literal{Type: typ, Text: s}}
	}
	op := func(name string, args ...plan.Expr) plan.Expr { return plan.Expr{Op: name, Args: args} }
	want := func(s string) *string { return &s }
	var cases []expressionExample
	for _, group := range []struct {
		typ    datatype.FieldType
		values []string
	}{
		{datatype.FieldTypeString, []string{"", "A", "a ", "  中文😀\n'\\:p --", "0002.700", "-0"}},
		{datatype.FieldTypeInt, []string{"0", "+0002", "-2147483648", "2147483647"}},
		{datatype.FieldTypeBigInt, []string{"-9223372036854775808", "9223372036854775807", "9007199254740993"}},
		{datatype.FieldTypeDecimal, []string{"-0.000", "0.000000000000000001", "-0.000000000000000001", "100", "100.000", "2.700", "-2.700", "99999999999999999999.999999999999999999", "-99999999999999999999.999999999999999999"}},
		{datatype.FieldTypeBool, []string{"true", "false"}},
		{datatype.FieldTypeDate, []string{"0001-01-01", "0999-12-31", "2000-02-29", "9999-12-31"}},
	} {
		for _, value := range group.values {
			v := plan.Literal{Type: group.typ, Text: value}
			canonical, err := v.Canonical()
			if err != nil {
				panic(err)
			} // fixed fixture values must be valid
			cases = append(cases,
				expressionExample{name: string(group.typ) + " literal " + value, expr: op("text", lit(group.typ, value)), want: want(canonical.Text)},
				expressionExample{name: string(group.typ) + " parameter " + value, expr: op("text", plan.Expr{Op: "parameter", Parameter: "value_parameter"}), parameter: &v, want: want(canonical.Text)},
			)
		}
		null := plan.Literal{Type: group.typ, Null: true}
		cases = append(cases,
			expressionExample{name: string(group.typ) + " NULL literal", expr: op("text", plan.Expr{Op: "literal", Literal: &null})},
			expressionExample{name: string(group.typ) + " NULL parameter", expr: op("text", plan.Expr{Op: "parameter", Parameter: "value_parameter"}), parameter: &null},
		)
	}
	bad := op("integer", lit(datatype.FieldTypeDecimal, "2.7"))
	badText := op("text", bad)
	validText := lit(datatype.FieldTypeString, "fallback")
	cases = append(cases,
		expressionExample{name: "integer column", expr: op("text", plan.Expr{Op: "column", Column: &plan.ColumnRef{Input: "seed", Name: "id"}}), want: want("1")},
		expressionExample{name: "rounded decimal division", expr: op("text", op("divide", lit(datatype.FieldTypeInt, "2"), lit(datatype.FieldTypeInt, "3"))), want: want("0.666666666666666667")},
		expressionExample{name: "decimal trailing integer zeros", expr: op("text", op("multiply", lit(datatype.FieldTypeDecimal, "10"), lit(datatype.FieldTypeDecimal, "10"))), want: want("100")},
		expressionExample{name: "month calculation", expr: op("text", op("add_months", lit(datatype.FieldTypeDate, "2024-01-31"), lit(datatype.FieldTypeInt, "1"))), want: want("2024-02-29")},
		expressionExample{name: "integer failure", expr: badText, invalid: true},
		expressionExample{name: "date failure", expr: op("text", op("date", lit(datatype.FieldTypeString, "2026-02-30"))), invalid: true},
		expressionExample{name: "boolean failure", expr: op("text", op("is_null", bad)), invalid: true},
		expressionExample{name: "coalesce before text preserves failure", expr: op("text", op("coalesce", bad, lit(datatype.FieldTypeBigInt, "2"))), invalid: true},
		expressionExample{name: "coalesce after text preserves failure", expr: op("coalesce", badText, validText), invalid: true},
		expressionExample{name: "CASE skips text failure", expr: op("case", lit(datatype.FieldTypeBool, "false"), badText, validText), want: want("fallback")},
		expressionExample{name: "CASE retains text failure", expr: op("case", lit(datatype.FieldTypeBool, "true"), badText, validText), invalid: true},
		expressionExample{name: "coalesce skips text failure", expr: op("coalesce", validText, badText), want: want("fallback")},
		expressionExample{name: "nested text", expr: op("text", op("text", lit(datatype.FieldTypeDecimal, "2.700"))), want: want("2.7")},
	)

	for _, tc := range []struct{ name, haystack, needle, want string }{
		{"Chinese", "户外苏玮伦😀", "苏玮", "true"},
		{"case", "Outdoor", "outdoor", "false"},
		{"accent", "café", "cafe", "false"},
		{"no normalization", "é", "é", "false"},
		{"wildcard literal", "a%b_c", "%b_", "true"},
		{"percent not wildcard", "abc", "%", "false"},
		{"underscore not wildcard", "abc", "_", "false"},
		{"space significant", "abc", "c ", "false"},
		{"empty needle", "abc", "", "true"},
		{"empty both", "", "", "true"},
		{"empty haystack", "", "a", "false"},
		{"backslash quote", "a\\'b", "\\'", "true"},
	} {
		v := plan.Literal{Type: datatype.FieldTypeString, Text: tc.needle}
		cases = append(cases,
			expressionExample{name: "contains/" + tc.name, expr: op("text", op("contains", lit(datatype.FieldTypeString, tc.haystack), lit(datatype.FieldTypeString, tc.needle))), want: want(tc.want)},
			expressionExample{name: "contains parameter/" + tc.name, expr: op("text", op("contains", lit(datatype.FieldTypeString, tc.haystack), plan.Expr{Op: "parameter", Parameter: "value_parameter"})), parameter: &v, want: want(tc.want)},
		)
	}
	nullText := plan.Expr{Op: "literal", Literal: &plan.Literal{Type: datatype.FieldTypeString, Null: true}}
	cases = append(cases,
		expressionExample{name: "contains NULL input", expr: op("text", op("contains", nullText, lit(datatype.FieldTypeString, "")))},
		expressionExample{name: "contains NULL needle", expr: op("text", op("contains", lit(datatype.FieldTypeString, "a"), nullText))},
	)
	return cases
}

func PreparedText(t *testing.T, provider plugin.AnalyticalCompilerProvider, conn plugin.ConnectionInfo) {
	t.Helper()
	preparedExpressionCases(t, provider, conn, textCases())
}

// NativeText allows each engine to perturb session display settings and check
// warnings on this same connection. PreparedText separately verifies the sole
// application execution path and errors surviving an empty result root.
func NativeText(t *testing.T, conn *sql.Conn, d sqlcompile.ExpressionDialect, checkWarnings func(*testing.T)) {
	t.Helper()
	for _, tc := range textCases() {
		if tc.parameter != nil || tc.name == "integer column" {
			continue
		}
		t.Run("native/"+tc.name, func(t *testing.T) {
			x, err := sqlcompile.CompileExpression(tc.expr, sqlcompile.ExpressionScope{}, d)
			if err != nil {
				t.Fatal(err)
			}
			invalidSQL := x.Invalid
			if invalidSQL == "" {
				invalidSQL = "FALSE"
			}
			var value sql.NullString
			var invalid bool
			if err = conn.QueryRowContext(t.Context(), "SELECT "+x.SQL+", "+invalidSQL).Scan(&value, &invalid); err != nil {
				t.Fatal(err)
			}
			if invalid != tc.invalid {
				t.Fatalf("invalid=%t want=%t", invalid, tc.invalid)
			}
			if !tc.invalid {
				if tc.want == nil {
					if value.Valid {
						t.Fatalf("expected NULL, got %q", value.String)
					}
				} else if !value.Valid || value.String != *tc.want {
					t.Fatalf("value=%#v want=%q", value, *tc.want)
				}
			}
			if checkWarnings != nil {
				checkWarnings(t)
			}
		})
	}
}
