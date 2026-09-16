package postgresql

import (
	"context"
	"database/sql"
	"errors"
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
	provider := pg
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

	verifyPostgresAnalyticalPreflight(t, db, provider, info, binding, q)
}

func verifyPostgresAnalyticalPreflight(t *testing.T, db *sql.DB, provider *PostgreSQLPlugin, info plugin.ConnectionInfo, binding plugin.SourceBinding, table string) {
	t.Helper()
	for _, tc := range []struct{ name, change, restore string }{
		{"precision", "ALTER COLUMN numeric_value TYPE numeric(39,18)", "ALTER COLUMN numeric_value TYPE numeric(38,18)"},
		{"varchar", "ALTER COLUMN label_value TYPE varchar(90)", "ALTER COLUMN label_value TYPE varchar(80)"},
		{"nullable", "ALTER COLUMN small_value DROP NOT NULL", "ALTER COLUMN small_value SET NOT NULL"},
		{"rename", "RENAME COLUMN label_value TO renamed_label", "RENAME COLUMN renamed_label TO label_value"},
		{"RLS", "ENABLE ROW LEVEL SECURITY", "DISABLE ROW LEVEL SECURITY"},
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
		ddlCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		_, blockedErr := db.ExecContext(ddlCtx, "ALTER TABLE "+table+" ADD COLUMN unrelated int")
		cancel()
		if blockedErr == nil {
			t.Fatal("DDL bypassed source lock")
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
