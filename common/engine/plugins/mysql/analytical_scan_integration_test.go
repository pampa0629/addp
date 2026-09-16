package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile/conformance"
	mysqlDriver "github.com/go-sql-driver/mysql"
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
	provider := mysql
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

	verifyMySQLAnalyticalPreflight(t, db, provider, info, binding, q)
}

func verifyMySQLAnalyticalPreflight(t *testing.T, db *sql.DB, provider *MySQLPlugin, info plugin.ConnectionInfo, binding plugin.SourceBinding, table string) {
	t.Helper()
	for _, tc := range []struct{ name, change, restore string }{
		{"precision", "MODIFY numeric_value decimal(39,18)", "MODIFY numeric_value decimal(38,18)"},
		{"varchar", "MODIFY label_value varchar(90)", "MODIFY label_value varchar(80)"},
		{"nullable", "MODIFY small_value int NULL", "MODIFY small_value int NOT NULL"},
		{"rename", "RENAME COLUMN label_value TO renamed_label", "RENAME COLUMN renamed_label TO label_value"},
		{"MyISAM", "ENGINE=MyISAM", "ENGINE=InnoDB"},
	} {
		t.Run("preflight/"+tc.name, func(t *testing.T) {
			prepared := conformance.PrepareScanForPreflight(t, provider, info, binding)
			if _, err := db.ExecContext(t.Context(), "ALTER TABLE "+table+" "+tc.change); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := db.ExecContext(t.Context(), "ALTER TABLE "+table+" "+tc.restore); err != nil {
					t.Error(err)
				}
			}()
			out, err := prepared.Execute(t.Context())
			want := plugin.ErrAnalyticalPlanChanged
			if tc.name == "RLS" || tc.name == "MyISAM" {
				want = plugin.ErrAnalyticalUnsupported
			}
			if !errors.Is(err, want) || out != nil {
				t.Fatalf("drift result=%#v err=%v want=%v", out, err, want)
			}
		})
	}
	t.Run("preflight/schema lock", func(t *testing.T) {
		tx, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err = provider.ValidateAnalyticalExecution(t.Context(), tx, []plugin.SourceBinding{binding}); err != nil {
			t.Fatal(err)
		}
		ddlConn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer ddlConn.Close()
		if _, err = ddlConn.ExecContext(t.Context(), "SET SESSION lock_wait_timeout = 1"); err != nil {
			t.Fatal(err)
		}
		// A client cancellation alone does not establish that MySQL stopped
		// the waiting DDL. Await a server-side lock timeout before releasing it.
		ddlCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_, blockedErr := ddlConn.ExecContext(ddlCtx, "ALTER TABLE "+table+" ADD COLUMN unrelated int")
		cancel()
		var lockErr *mysqlDriver.MySQLError
		if !errors.As(blockedErr, &lockErr) || lockErr.Number != 1205 {
			t.Fatalf("expected server lock timeout: %v", blockedErr)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		prepared := conformance.PrepareScanForPreflight(t, provider, info, binding)
		if _, err = db.ExecContext(t.Context(), "ALTER TABLE "+table+" ADD COLUMN unrelated int"); err != nil {
			t.Fatal(err)
		}
		out, err := prepared.Execute(t.Context())
		if err != nil || len(out.Rows) != 3 {
			t.Fatalf("unrelated column should not invalidate result: %#v %v", out, err)
		}
	})

	t.Run("preflight/missing source", func(t *testing.T) {
		prepared := conformance.PrepareScanForPreflight(t, provider, info, binding)
		if _, err := db.ExecContext(t.Context(), "DROP TABLE "+table); err != nil {
			t.Fatal(err)
		}
		out, err := prepared.Execute(t.Context())
		if !errors.Is(err, plugin.ErrAnalyticalPlanChanged) || out != nil {
			t.Fatalf("missing source result=%#v err=%v", out, err)
		}
	})
}
