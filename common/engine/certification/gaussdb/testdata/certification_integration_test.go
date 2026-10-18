package gaussdb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/HuaweiCloudDeveloper/gaussdb-go/stdlib"
	_ "github.com/lib/pq"
)

// The release owner copies this source into its temporary standalone module.
// testdata keeps this standalone probe outside ordinary Common package discovery.
// The admitted plugin uses the same pinned driver; probe builds do not rewrite Common.
func TestGaussDBDriverCoexistence(t *testing.T) {
	if _, present := os.LookupEnv("GOENV_REGIST_ALL"); present {
		t.Fatal("driver coexistence must not rely on GOENV_REGIST_ALL")
	}
	for name, wanted := range map[string]string{
		"gaussdb":  "github.com/HuaweiCloudDeveloper/gaussdb-go/stdlib",
		"postgres": "github.com/lib/pq",
	} {
		db, err := sql.Open(name, "host=127.0.0.1 port=1 connect_timeout=1")
		if err != nil {
			t.Fatal("driver not registered: " + name)
		}
		if got := reflect.TypeOf(db.Driver()).Elem().PkgPath(); got != wanted {
			t.Fatalf("driver %s registered by %s instead of %s", name, got, wanted)
		}
		db.Close()
	}
}

func TestGaussDBOfficialMediaCertification(t *testing.T) {
	if os.Getenv("ADDP_GAUSSDB_CERTIFICATION") != "1" {
		t.Fatal("native GaussDB certification runs only through the T5 owner")
	}
	dsn := os.Getenv("ADDP_TEST_GAUSSDB_DSN")
	if dsn == "" {
		t.Fatal("release owner must supply the disposable database")
	}
	db, err := sql.Open("gaussdb", dsn)
	if err != nil {
		t.Fatal("cannot create official driver connection")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("candidate driver SHA256 authentication failed: %v", err)
	}
	var version string
	if err := db.QueryRowContext(ctx, "SELECT version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(version, "GaussDB") || !strings.Contains(version, "507.0") {
		t.Fatalf("unexpected official kernel: %s", version)
	}
	t.Logf("official kernel: %s", version)
	const exactAmount = "12345678901234567890.123456789012345678"
	const largeInteger int64 = 9007199254740993
	var boundText, boundAmount string
	var boundInteger int64
	if err := db.QueryRowContext(ctx, "SELECT $1::text, $2::numeric(38,18)::text, $3::bigint", "高斯参数中文", exactAmount, largeInteger).Scan(&boundText, &boundAmount, &boundInteger); err != nil {
		t.Fatalf("typed Chinese/numeric/bigint parameter binding failed: %v", err)
	}
	if boundText != "高斯参数中文" || boundAmount != exactAmount || boundInteger != largeInteger {
		t.Fatal("typed parameter binding changed exact values")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var readOnly string
	if err := tx.QueryRowContext(ctx, "SHOW transaction_read_only").Scan(&readOnly); err != nil || readOnly != "on" {
		t.Fatalf("native read-only transaction: mode=%s err=%v", readOnly, err)
	}
	var id int64
	var name, amount string
	if err := tx.QueryRowContext(ctx, "SELECT id, name, amount::text FROM addp_cert.source WHERE id=$1", 1).Scan(&id, &name, &amount); err != nil {
		t.Fatal(err)
	}
	if id != 1 || name != "高斯认证中文" || amount != "12345678901234567890.123456789012345678" {
		t.Fatal("parameter binding or exact numeric/text roundtrip differs")
	}
	var columns, keys int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2", "addp_cert", "source").Scan(&columns); err != nil || columns != 3 {
		t.Fatalf("column catalog: columns=%d err=%v", columns, err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM pg_catalog.pg_constraint c JOIN pg_catalog.pg_class r ON r.oid=c.conrelid JOIN pg_catalog.pg_namespace n ON n.oid=r.relnamespace WHERE n.nspname=$1 AND r.relname=$2 AND c.contype='p'", "addp_cert", "source").Scan(&keys); err != nil || keys != 1 {
		t.Fatalf("primary key catalog: keys=%d err=%v", keys, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO addp_cert.source VALUES (2,'must reject',0)"); err == nil {
		t.Fatal("read-only transaction accepted a write")
	}
	_ = tx.Rollback()
	short, stop := context.WithTimeout(ctx, 150*time.Millisecond)
	defer stop()
	started := time.Now()
	if _, err := db.ExecContext(short, "SELECT pg_sleep(10)"); err == nil || !errors.Is(short.Err(), context.DeadlineExceeded) || time.Since(started) > 3*time.Second {
		t.Fatalf("timely query cancellation was not enforced: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("connection pool unusable after cancellation: %v", err)
	}
}
