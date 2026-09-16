package postgresql

import (
	"database/sql"
	"fmt"
	"github.com/addp/common/query/sqlcompile"
	"github.com/addp/common/query/sqlcompile/conformance"
	"testing"
)

func TestIntegrationPostgresAnalyticalArithmetic(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	defer db.Close()

	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	dialect := analyticalArithmeticDialect{}
	for _, precision := range []int{0} {

		t.Run(fmt.Sprintf("native precision=%d", precision), func(t *testing.T) {
			for _, tc := range conformance.ArithmeticCases() {
				t.Run(tc.Name, func(t *testing.T) {
					expr, err := sqlcompile.Arithmetic(tc.Op, sqlcompile.CheckedExpression{SQL: "a", Type: tc.Left.Type}, sqlcompile.CheckedExpression{SQL: "b", Type: tc.Right.Type}, dialect)
					if err != nil {
						t.Fatal(err)
					}
					query := "WITH input AS (SELECT CAST($1 AS decimal(38,18)) AS a, CAST($2 AS decimal(38,18)) AS b) SELECT " + expr.SQL + ", " + expr.Invalid + " FROM input"
					var left, right any = tc.Left.Text, tc.Right.Text
					if tc.Left.Null {
						left = nil
					}
					if tc.Right.Null {
						right = nil
					}
					var value sql.NullString
					var invalid bool
					err = conn.QueryRowContext(t.Context(), query, left, right).Scan(&value, &invalid)

					var actual *string
					if value.Valid {
						actual = &value.String
					}
					tc.Check(t, actual, invalid, true, err)
				})
			}
		})
	}
	conformance.PreparedArithmetic(t, provider, info)
}
