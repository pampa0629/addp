package doris

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestIntegrationDorisDecimalTableWriteDefinition(t *testing.T) {
	if os.Getenv("ADDP_DORIS_INTEGRATION") != "1" {
		t.Skip("set ADDP_DORIS_INTEGRATION=1 to run Doris integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connInfo := dorisIntegrationConnInfo(t)
	p := &DorisPlugin{}
	database := plugin.GetString(connInfo, "database")
	table := "addp_decimal_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	path := plugin.TabularItemPath(93002, "database", database, table)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = p.DeleteResource(cleanupCtx, connInfo, path)
	}()

	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: false},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 20, Scale: 6, Nullable: false},
	}
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatalf("PrepareTableWrite() error = %v", err)
	}

	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var precision, scale int
	if err := db.QueryRowContext(ctx, `
		SELECT numeric_precision, numeric_scale
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ? AND column_name = 'amount'
	`, database, table).Scan(&precision, &scale); err != nil {
		t.Fatalf("query Doris decimal definition: %v", err)
	}
	if precision != 20 || scale != 6 {
		t.Fatalf("Doris amount definition = DECIMAL(%d,%d), want DECIMAL(20,6)", precision, scale)
	}
}

func dorisIntegrationConnInfo(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	portText := strings.TrimSpace(os.Getenv("ADDP_TEST_DORIS_PORT"))
	if portText == "" {
		portText = "9030"
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("invalid Doris integration port %q: %v", portText, err)
	}
	valueOr := func(key, fallback string) string {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
		return fallback
	}
	return plugin.ConnectionInfo{
		"host":     valueOr("ADDP_TEST_DORIS_HOST", "127.0.0.1"),
		"port":     port,
		"database": valueOr("ADDP_TEST_DORIS_DATABASE", "addp_doris_disposable"),
		"user":     valueOr("ADDP_TEST_DORIS_USER", "root"),
		"password": valueOr("ADDP_TEST_DORIS_PASSWORD", ""),
	}
}

func TestIntegrationDorisGuardedQueryReadSession(t *testing.T) {
	if os.Getenv("ADDP_DORIS_INTEGRATION") != "1" {
		t.Skip("use make test-common-doris-decimal")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	p := &DorisPlugin{}
	info := dorisIntegrationConnInfo(t)
	database := plugin.GetString(info, "database")
	const reader = "addp_guard_reader"
	password := "DorisFixture9-" + uuid.NewString()
	const engineID uint = 93003
	first := plugin.TabularItemPath(engineID, "database", database, "addp_guard_source")
	second := plugin.TabularItemPath(engineID, "database", database, "addp_guard_other")
	dsn, err := p.serverDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	exec := func(statement string) {
		t.Helper()
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE DATABASE IF NOT EXISTS " + dorisDialect().QuoteIdentifier(database))
	exec("CREATE USER " + reader + " IDENTIFIED BY '" + password + "'")
	defer func() {
		if _, err := admin.ExecContext(context.Background(), "DROP USER "+reader); err != nil {
			t.Error(err)
		}
	}()
	tables := []string{
		dorisDialect().QualifiedTable(database, "addp_guard_source"),
		dorisDialect().QualifiedTable(database, "addp_guard_other"),
	}
	for _, table := range tables {
		exec("CREATE TABLE " + table + " (id INT NOT NULL) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES('replication_num'='1')")
		defer admin.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
		exec("INSERT INTO " + table + " VALUES (37)")
		exec("GRANT SELECT_PRIV ON " + table + " TO " + reader)
	}
	info["user"], info["password"] = reader, password
	single := "SELECT id FROM " + tables[0] + " WHERE id = :id"
	for _, tc := range []struct {
		name  string
		query string
		paths []plugin.EngineCatalogPath
		allow bool
	}{
		{"authorized", single, []plugin.EngineCatalogPath{first}, true},
		{"ungranted_join", "SELECT a.id FROM " + tables[0] + " a JOIN " + tables[1] + " b ON a.id=b.id WHERE a.id=:id", []plugin.EngineCatalogPath{first, second}, false},
		{"revoked", single, []plugin.EngineCatalogPath{first}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := p.PrepareQuery(ctx, info, plugin.QueryRequest{
				EngineID: engineID, Language: "sql", Query: tc.query,
				Options: plugin.QueryOptions{ReadOnly: true, Parameters: map[string]any{"id": 37}},
			})
			if err != nil {
				t.Fatal(err)
			}
			want, err := plugin.NewQueryReadSet(tc.paths...)
			if err != nil {
				t.Fatal(err)
			}
			denied := errors.New("fixture source authorization denied")
			calls := 0
			session, err := p.OpenGuardedQueryReadSession(ctx, prepared, func(_ context.Context, actual *plugin.QueryReadSet) error {
				calls++
				if !reflect.DeepEqual(actual, want) {
					t.Fatalf("Doris source paths = %v, want %v", actual.Paths, want.Paths)
				}
				if !tc.allow {
					return denied
				}
				return nil
			})
			if session != nil {
				defer session.Close(ctx)
			}
			if !tc.allow {
				if session != nil || !errors.Is(err, denied) || calls != 1 {
					t.Fatalf("expected denial before business cursor, calls=%d err=%v", calls, err)
				}
				return
			}
			if err != nil || calls != 1 {
				t.Fatalf("open Doris query, calls=%d err=%v", calls, err)
			}
			batch, err := session.ReadBatch(ctx, 2)
			if err != nil || len(batch.Rows) != 1 || batch.Rows[0]["id"] != int64(37) {
				t.Fatalf("Doris business rows = %+v, err=%v", batch, err)
			}
		})
	}
}
