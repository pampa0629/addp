package postgresql

import (
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile/conformance"
)

func TestIntegrationPostgresAnalyticalScan(t *testing.T) {
	db, pg, info := openPostgresPrepareIntegration(t, false)
	defer db.Close()
	var encoding string
	if err := db.QueryRowContext(t.Context(), "SHOW server_encoding").Scan(&encoding); err != nil || encoding != "UTF8" {
		t.Fatalf("UTF8 certification prerequisite: %s %v", encoding, err)
	}
	table := fmt.Sprintf("scan_\"quoted_%d", time.Now().UnixNano())
	q := `"common_pg_it".` + analyticalExpressionDialect{}.QuoteIdentifier(table)
	if _, err := db.ExecContext(t.Context(), `CREATE SCHEMA IF NOT EXISTS "common_pg_it"`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE "+q+` (row_id bigint NOT NULL PRIMARY KEY, small_value integer NOT NULL, label_value varchar(80), numeric_value numeric(38,18), flag_value boolean, day_value date)`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.ExecContext(t.Context(), "DROP TABLE IF EXISTS "+q); err != nil {
			t.Error(err)
		}
	}()

	if _, err := db.ExecContext(t.Context(), "INSERT INTO "+q+" VALUES (1,7,'A',12345678901234567890.123456789012345678,true,'0001-01-01'),(2,-2,'a ',-0.000000000000000001,false,'2024-02-29'),(9007199254740993,0,NULL,NULL,NULL,NULL)"); err != nil {
		t.Fatal(err)
	}
	columns, err := postgresTableColumns(t.Context(), db, "common_pg_it", table)
	if err != nil {
		t.Fatal(err)
	}
	fields := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		fields = append(fields, postgresFieldInfoFromColumn(column))
	}
	engineID := uint(time.Now().UnixNano())
	defer plugin.ClosePool(engineID)
	path := plugin.EngineCatalogBranchLeafPath(pg.EngineCatalogModel(), engineID, plugin.EngineCatalogTermSchema, "common_pg_it", plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, table)
	binding := conformance.ScanBinding(t, path, fields)
	provider := &integerPreparedProvider{PostgreSQLPlugin: pg, compiler: conformance.RelationalFixtureCompiler{Expression: analyticalExpressionDialect{}, Result: analyticalResultDialect{}, Scan: analyticalScanDialect{}}}
	conformance.PreparedScan(t, provider, info, binding)
	for _, bad := range []struct{ name, assignment, reset string }{{"numeric NaN", "numeric_value = 'NaN'", "numeric_value = NULL"}, {"infinite date", "day_value = 'infinity'", "day_value = NULL"}, {"BC date", "day_value = '0001-01-01 BC'", "day_value = NULL"}, {"date beyond neutral range", "day_value = '10000-01-01'", "day_value = NULL"}} {
		t.Run(bad.name, func(t *testing.T) {
			if _, err := db.ExecContext(t.Context(), "UPDATE "+q+" SET "+bad.assignment+" WHERE row_id=9007199254740993"); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := db.ExecContext(t.Context(), "UPDATE "+q+" SET "+bad.reset+" WHERE row_id=9007199254740993"); err != nil {
					t.Error(err)
				}
			}()
			conformance.PreparedInvalidScan(t, provider, info, binding)
		})
	}
}
