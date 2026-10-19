// Package starrocks certifies native protocol facts before any Engine is registered.
package starrocks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	mysql "github.com/go-sql-driver/mysql"
)

func connect(t *testing.T, reader bool) *sql.DB {
	t.Helper()
	if os.Getenv("ADDP_STARROCKS_INTEGRATION") != "1" {
		t.Skip("requires standard disposable StarRocks admission entry")
	}
	encoded, err := os.ReadFile(filepath.Join(os.Getenv("ADDP_STARROCKS_ADMISSION_DIR"), "connection.json"))
	if err != nil {
		t.Fatal("private admission connection unavailable")
	}
	var value struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Password string `json:"reader_password"`
	}
	if json.Unmarshal(encoded, &value) != nil || value.Host != "127.0.0.1" || value.Port <= 0 || value.Password == "" {
		t.Fatal("invalid admission connection")
	}
	config := mysql.NewConfig()
	config.Net, config.Addr, config.DBName = "tcp", fmt.Sprintf("127.0.0.1:%d", value.Port), "addp_acceptance"
	config.User = "root"
	if reader {
		config.User, config.Passwd = "fixture_reader", value.Password
	}
	config.Timeout, config.ReadTimeout, config.WriteTimeout = 10*time.Second, 15*time.Second, 15*time.Second
	connector, err := mysql.NewConnector(config)
	if err != nil {
		t.Fatal("cannot create pinned MySQL connector")
	}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestAdmissionPreparedReadPrecisionAndLeastPrivilege(t *testing.T) {
	db := connect(t, true)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT name,id,amount,optional_text FROM source WHERE id>=? ORDER BY id", int64(2))
	if err != nil {
		t.Fatalf("native parameterized SELECT rejected: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var name, amount string
		var id int64
		var optional sql.NullString
		if err := rows.Scan(&name, &id, &amount, &optional); err != nil {
			t.Fatal(err)
		}
		wantAmount := "12345678901234567890.123456789012345678"
		if id == 2 {
			wantAmount = "-0.000000000000000001"
			if name != "保留尾空格  " || !optional.Valid || optional.String != "" {
				t.Fatal("native empty string or trailing spaces changed")
			}
		} else if id != 9007199254740993 || name != "正式接入中文" || optional.Valid {
			t.Fatal("native bigint/Chinese/null changed")
		}
		actual, a := new(big.Rat).SetString(amount)
		want, b := new(big.Rat).SetString(wantAmount)
		if !a || !b || actual.Cmp(want) != 0 {
			t.Fatal("native Decimal(38,18) changed")
		}
		count++
	}
	if rows.Err() != nil || count != 2 {
		t.Fatal("native result incomplete")
	}
	_, err = db.ExecContext(ctx, "INSERT INTO source VALUES ('ungranted',3,1,NULL)")
	var denied *mysql.MySQLError
	// StarRocks 3.5.21 ErrorCode.ERR_ACCESS_DENIED is 5203/42000,
	// rather than MySQL's table-access error 1142.
	if !errors.As(err, &denied) || denied.Number != 5203 || string(denied.SQLState[:]) != "42000" {
		t.Fatalf("reader must be denied by native table privileges, not a syntax error: %v", err)
	}
}

func TestAdmissionGuardRejectsSideEffects(t *testing.T) {
	for _, statement := range []string{
		"INSERT INTO write_probe VALUES (2)", "UPDATE source SET id=3", "DELETE FROM source", "DROP TABLE source",
		"CREATE TABLE surprise(id INT)", "SELECT * FROM source INTO OUTFILE '/tmp/addp-unexpected-export'",
		"SELECT * FROM source FOR UPDATE", "SELECT * FROM source LOCK IN SHARE MODE", "SET sql_mode=''",
		"CALL unexpected_procedure()", "SELECT * FROM source; INSERT INTO write_probe VALUES (2)", "EXPLAIN INSERT INTO write_probe VALUES (2)",
	} {
		if commonquery.RequireReadOnly(statement) == nil {
			t.Fatalf("shared guard accepted side-effect SQL: %s", statement)
		}
	}
}

func TestAdmissionValidatedStatementAndNativeTransactionBoundary(t *testing.T) {
	db := connect(t, false)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	control, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("positive control transaction unavailable: %v", err)
	}
	defer control.Rollback()
	if _, err := control.ExecContext(ctx, "INSERT INTO write_probe VALUES (1)"); err != nil {
		t.Fatalf("positive control transaction is not writable: %v", err)
	}
	if err := control.Commit(); err != nil {
		t.Fatalf("positive control write commit failed: %v", err)
	}
	unsupported, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if unsupported != nil {
		_ = unsupported.Rollback()
	}
	var syntax *mysql.MySQLError
	if !errors.As(err, &syntax) || syntax.Number != 1064 || !strings.Contains(syntax.Message, "READ") {
		t.Fatalf("fixed 3.5.21 native read-only transaction boundary changed; re-audit before admission: %v", err)
	}
	const statement = "SELECT count(*) FROM source WHERE id>=?"
	if err := commonquery.RequireReadOnly(statement); err != nil {
		t.Fatal(err)
	}
	tx, err := plugin.BeginControlledReadOnlySQLTransaction(ctx, db, commonquery.DialectMySQL, plugin.ControlledReadOnlySQLBoundaryValidatedStatement, sql.LevelDefault)
	if err != nil {
		t.Fatalf("shared validated-statement transaction unavailable: %v", err)
	}
	defer tx.Rollback()
	var count int
	// Distinguish native SELECT support from COM_STMT_PREPARE support in an
	// explicit transaction. The fixed server validates the PrepareStmt wrapper
	// before it unwraps the underlying QueryStatement.
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM source WHERE id>=2").Scan(&count); err != nil || count != 2 {
		t.Fatalf("plain SELECT inside ordinary transaction failed: %v", err)
	}
	err = tx.QueryRowContext(ctx, statement, int64(2)).Scan(&count)
	var prepared *mysql.MySQLError
	if !errors.As(err, &prepared) || prepared.Number != 5305 || string(prepared.SQLState[:]) != "25P01" {
		t.Fatalf("fixed explicit-transaction prepared-query limitation changed; re-audit execution route: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("ordinary diagnostic transaction rollback failed: %v", err)
	}
	// The admitted route executes one guarded statement with server-side
	// parameters using the SELECT-only account. It never retries a transaction
	// failure with interpolation or promises a cross-statement snapshot.
	reader := connect(t, true)
	if err := reader.QueryRowContext(ctx, statement, int64(2)).Scan(&count); err != nil || count != 2 {
		t.Fatalf("validated single-statement parameter query failed: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM write_probe").Scan(&count); err != nil || count != 1 {
		t.Fatalf("unexpected write beyond the positive control: %v", err)
	}
}

func TestAdmissionNativeVisibleCatalogAndFieldFacts(t *testing.T) {
	db := connect(t, true)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var catalog string
	if err := db.QueryRowContext(ctx, "SELECT catalog()").Scan(&catalog); err != nil || catalog != "default_catalog" {
		t.Fatalf("native internal catalog required: %q %v", catalog, err)
	}
	rows, err := db.QueryContext(ctx, "SHOW FULL TABLES FROM addp_acceptance")
	if err != nil {
		t.Fatal(err)
	}
	visible := map[string]string{}
	for rows.Next() {
		var name, kind string
		if err := rows.Scan(&name, &kind); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		visible[name] = kind
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(visible) != 2 || visible["source"] != "BASE TABLE" || visible["visible_view"] != "VIEW" {
		t.Fatalf("native visible catalog must distinguish tables/views and hide ungranted write_probe: %+v %v", visible, err)
	}
	var table, ddl string
	if err := db.QueryRowContext(ctx, "SHOW CREATE TABLE addp_acceptance.source").Scan(&table, &ddl); err != nil {
		t.Fatal(err)
	}
	compact := strings.Join(strings.Fields(ddl), "")
	if table != "source" || !strings.Contains(compact, "PRIMARYKEY(`name`,`id`)") {
		t.Fatalf("native ordered primary key missing: %s", ddl)
	}
	rows, err = db.QueryContext(ctx, "DESC addp_acceptance.source")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for rows.Next() {
		var name, nativeType, nullable, sortKey string
		var defaultValue, extra sql.NullString
		if err := rows.Scan(&name, &nativeType, &nullable, &sortKey, &defaultValue, &extra); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		names = append(names, name)
		if name == "amount" && strings.ToLower(strings.ReplaceAll(nativeType, " ", "")) != "decimal(38,18)" {
			t.Fatalf("DESC lost native Decimal(38,18): %s", nativeType)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || strings.Join(names, ",") != "name,id,amount,optional_text" {
		t.Fatalf("DESC field order changed: %v %v", names, err)
	}
	// DESC.Key is a sort-key flag, not a primary-key declaration. The writable
	// control uses DUPLICATE KEY and must never be reported as a primary key.
	admin := connect(t, false)
	if err := admin.QueryRowContext(ctx, "SHOW CREATE TABLE addp_acceptance.write_probe").Scan(&table, &ddl); err != nil {
		t.Fatal(err)
	}
	compact = strings.Join(strings.Fields(ddl), "")
	if !strings.Contains(compact, "DUPLICATEKEY(`id`)") || strings.Contains(compact, "PRIMARYKEY(") {
		t.Fatal("duplicate/sort key was confused with a primary key")
	}
	rows, err = db.QueryContext(ctx, "SELECT name,id,amount,optional_text FROM source WHERE id<0")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.ColumnTypes()
	if err != nil || len(columns) != 4 {
		t.Fatalf("empty native result lost schema: %v", err)
	}
	precision, scale, ok := columns[2].DecimalSize()
	// Fixed Type.getMysqlResultSetFieldLength adds sign + leading zero +
	// overflow space, plus a decimal point when scale > 0. The MySQL driver
	// subtracts only sign/point, so the unadapted ColumnType reports P+2.
	// This certifies the raw protocol quirk, not a production field mapper.
	if columns[1].DatabaseTypeName() != "BIGINT" || !ok || precision != 40 || scale != 18 {
		t.Fatalf("raw BIGINT/Decimal protocol fact changed; re-audit mapper: bigint=%s precision=%d scale=%d known=%t", columns[1].DatabaseTypeName(), precision, scale, ok)
	}
	if rows.Next() || rows.Err() != nil {
		t.Fatal("expected an empty result with native schema")
	}
}

func TestAdmissionNativeDecimalProtocolLengths(t *testing.T) {
	db := connect(t, true)
	for _, target := range []struct{ precision, scale int64 }{
		{1, 0}, {9, 0}, {12, 2}, {18, 18}, {38, 0}, {38, 18}, {38, 38},
	} {
		t.Run(fmt.Sprintf("decimal_%d_%d", target.precision, target.scale), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			// Both an expression and an empty result must retain protocol type
			// facts; source-column lookup and sample-value inference cannot help.
			statement := fmt.Sprintf("SELECT CAST(NULL AS DECIMAL(%d,%d)) AS computed_amount FROM source WHERE id<0", target.precision, target.scale)
			rows, err := db.QueryContext(ctx, statement)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			columns, err := rows.ColumnTypes()
			if err != nil || len(columns) != 1 {
				t.Fatalf("native expression schema missing: %v", err)
			}
			precision, scale, ok := columns[0].DecimalSize()
			if columns[0].DatabaseTypeName() != "DECIMAL" || !ok || precision != target.precision+2 || scale != target.scale {
				t.Fatalf("raw Decimal protocol length changed: type=%s precision=%d scale=%d known=%t", columns[0].DatabaseTypeName(), precision, scale, ok)
			}
			if rows.Next() || rows.Err() != nil {
				t.Fatal("expected empty expression result with native schema")
			}
		})
	}
}
