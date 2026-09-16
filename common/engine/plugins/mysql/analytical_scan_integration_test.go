package mysql

import (
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile/conformance"
)

func TestIntegrationMySQLAnalyticalScan(t *testing.T) {
	db, mysql, info, database := openMySQLUpsertIntegration(t)
	defer db.Close()
	defer dropMySQLIntegrationDatabase(db, database)
	info["database"] = database
	table := "scan_`quoted"
	d := analyticalExpressionDialect{}
	q := d.QuoteIdentifier(database) + "." + d.QuoteIdentifier(table)
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE "+q+" (row_id bigint NOT NULL PRIMARY KEY, small_value int NOT NULL, label_value varchar(80), numeric_value decimal(38,18), flag_value tinyint(1), day_value date) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO "+q+" VALUES (1,7,'A',12345678901234567890.123456789012345678,1,'0001-01-01'),(2,-2,'a ',-0.000000000000000001,0,'2024-02-29'),(9007199254740993,0,NULL,NULL,NULL,NULL)"); err != nil {
		t.Fatal(err)
	}
	columns, err := mysqlTableColumns(t.Context(), db, database, table)
	if err != nil {
		t.Fatal(err)
	}
	engineID := uint(time.Now().UnixNano())
	defer plugin.ClosePool(engineID)
	path := plugin.EngineCatalogBranchLeafPath(mysql.EngineCatalogModel(), engineID, plugin.EngineCatalogTermDatabase, database, plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, table)
	binding := conformance.ScanBinding(t, path, mysqlFieldsFromColumns(columns))
	provider := &integerPreparedProvider{MySQLPlugin: mysql, compiler: conformance.RelationalFixtureCompiler{Expression: d, Result: analyticalResultDialect{}, Scan: analyticalScanDialect{}}}
	conformance.PreparedScan(t, provider, info, binding)
	t.Run("non boolean tinyint", func(t *testing.T) {
		if _, err := db.ExecContext(t.Context(), "UPDATE "+q+" SET flag_value=2 WHERE row_id=9007199254740993"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := db.ExecContext(t.Context(), "UPDATE "+q+" SET flag_value=NULL WHERE row_id=9007199254740993"); err != nil {
				t.Error(err)
			}
		}()
		conformance.PreparedInvalidScan(t, provider, info, binding)
	})
	t.Run("zero date", func(t *testing.T) {
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var mode string
		if err := conn.QueryRowContext(t.Context(), "SELECT @@SESSION.sql_mode").Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(t.Context(), "SET SESSION sql_mode = ''"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := conn.ExecContext(t.Context(), "SET SESSION sql_mode = ?", mode); err != nil {
				t.Error(err)
			}
		}()
		if _, err := conn.ExecContext(t.Context(), "UPDATE "+q+" SET day_value='0000-00-00' WHERE row_id=9007199254740993"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := conn.ExecContext(t.Context(), "UPDATE "+q+" SET day_value=NULL WHERE row_id=9007199254740993"); err != nil {
				t.Error(err)
			}
		}()
		conformance.PreparedInvalidScan(t, provider, info, binding)
	})
}
