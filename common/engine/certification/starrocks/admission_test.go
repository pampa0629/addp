// Package starrocks certifies native protocol facts before any Engine is registered.
package starrocks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if _, err := db.ExecContext(ctx, "INSERT INTO source VALUES ('ungranted',3,1,NULL)"); err == nil {
		t.Fatal("SELECT-only reader wrote to source")
	}
}

func TestAdmissionNativeReadOnlyTransactionRejectsWrites(t *testing.T) {
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
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("native read-only transaction unavailable: %v", err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM source").Scan(&count); err != nil || count != 2 {
		t.Fatalf("read-only transaction cannot query source: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO write_probe VALUES (2)"); err == nil {
		t.Fatal("native read-only transaction allowed a write; Engine admission is blocked")
	}
}
