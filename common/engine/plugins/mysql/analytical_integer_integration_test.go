package mysql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/query/sqlcompile"
	"github.com/addp/common/query/sqlcompile/conformance"
)

func TestIntegrationMySQLLosslessAnalyticalInteger(t *testing.T) {
	db, mysql, connInfo, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	connInfo["database"] = database
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	expr, err := sqlcompile.LosslessInteger(sqlcompile.CheckedExpression{SQL: "v", Type: datatype.FieldTypeDecimal}, analyticalIntegerDialect{})
	if err != nil {
		t.Fatal(err)
	}
	query := "WITH input AS (SELECT CAST(? AS decimal(38,18)) AS v) SELECT " + expr.SQL + ", " + expr.Invalid + " FROM input"
	conformance.LosslessInteger(t, func(input *string) (*int64, bool, error) {
		var value sql.NullInt64
		var invalid bool
		err := conn.QueryRowContext(t.Context(), query, input).Scan(&value, &invalid)
		if err != nil {
			return nil, false, err
		}
		// Same session: successful SQL with a warning must not mask a lossy CAST.
		var warnings int
		if err := conn.QueryRowContext(t.Context(), "SHOW COUNT(*) WARNINGS").Scan(&warnings); err != nil {
			return nil, false, err
		}
		if warnings != 0 {
			return nil, false, fmt.Errorf("integer conversion produced %d native warnings", warnings)
		}
		if !value.Valid {
			return nil, invalid, nil
		}
		return &value.Int64, invalid, nil
	})
	conformance.PreparedLosslessInteger(t, mysql, connInfo)
	conformance.PreparedConditionals(t, mysql, connInfo)
}
