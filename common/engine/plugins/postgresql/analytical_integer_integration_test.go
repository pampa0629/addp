package postgresql

import (
	"database/sql"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/query/sqlcompile"
	"github.com/addp/common/query/sqlcompile/conformance"
)

func TestIntegrationPostgresLosslessAnalyticalInteger(t *testing.T) {
	db, pg, conn := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	expr, err := sqlcompile.LosslessInteger(sqlcompile.CheckedExpression{SQL: "v", Type: datatype.FieldTypeDecimal}, analyticalIntegerDialect{})
	if err != nil {
		t.Fatal(err)
	}
	query := "WITH input AS (SELECT CAST($1 AS numeric(38,18)) AS v) SELECT " + expr.SQL + ", " + expr.Invalid + " FROM input"
	conformance.LosslessInteger(t, func(input *string) (*int64, bool, error) {
		var value sql.NullInt64
		var invalid bool
		err := db.QueryRowContext(t.Context(), query, input).Scan(&value, &invalid)
		if !value.Valid {
			return nil, invalid, err
		}
		return &value.Int64, invalid, err
	})
	conformance.PreparedLosslessInteger(t, pg, conn)
	conformance.PreparedConditionals(t, pg, conn)
}
