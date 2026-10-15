package highgo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/lib/pq"
)

// The same protocol contract runs in the existing disposable PostgreSQL gate.
// Native certification is explicit, requires V4.5 + TLS, and owns only a new
// random schema. Neither mode resets a database or touches pre-existing tables.
func TestIntegrationHighGoTableWrite(t *testing.T) {
	native := os.Getenv("ADDP_HIGHGO_INTEGRATION") == "1"
	if !native && os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run scripts/test/common-postgres-gate.sh --test highgo-write")
	}
	info := highgoWriteTestConnection(t, native)
	p := &Plugin{}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	if native {
		t.Cleanup(func() { plugin.ClosePool(0) })
		if err := p.TestConnection(ctx, info); err != nil {
			t.Fatalf("native V4.5 identity/SM3/TLS: %v", err)
		}
		entries, err := p.ListChildren(ctx, info, plugin.TabularNamespacePath(0, plugin.EngineCatalogTermSchema, "public"), plugin.ListOptions{})
		if err != nil {
			t.Fatalf("native business catalog: %v", err)
		}
		for _, entry := range entries {
			if entry.Name == "hg_t_audit_log" || entry.Name == "pg_buffercache" || entry.Name == "pg_stat_statements" {
				t.Fatal("native audit/monitoring object leaked into the business catalog")
			}
		}
		hidden := plugin.TabularItemPath(0, plugin.EngineCatalogTermSchema, "public", "hg_t_audit_log")
		if _, err := p.ResolvePath(ctx, info, hidden); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
			t.Fatalf("native audit table old path must be not_found: %v", err)
		}
	}
	dsn, err := p.BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("highgo", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	var version string
	if err := db.QueryRowContext(ctx, "SELECT version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Logf("native=%t server=%s", native, version)

	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "addp_highgo_write_" + hex.EncodeToString(random)
	quotedSchema := pq.QuoteIdentifier(schema)
	// No IF NOT EXISTS: ownership is established only by successful creation.
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema %s: %v (creation outcome/cleanup unverified)", schema, err)
	}
	table := quotedSchema + `."contract"`
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		// RESTRICT prevents removal of objects outside this fixture's known set.
		if _, err := db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+table+" RESTRICT"); err != nil {
			t.Errorf("cleanup owned test table: %v", err)
		}
		if _, err := db.ExecContext(cleanupCtx, "DROP SCHEMA "+quotedSchema+" RESTRICT"); err != nil {
			t.Errorf("cleanup owned test schema %s: %v", schema, err)
		}
		var exists bool
		if err := db.QueryRowContext(cleanupCtx, "SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname=$1)", schema).Scan(&exists); err != nil || exists {
			t.Errorf("zero residual not proven: schema=%s exists=%t error=%v", schema, exists, err)
		} else {
			t.Logf("owned schema removed; zero residual: %s", schema)
		}
	})

	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
		{Name: "label", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 38, Scale: 18, Nullable: true},
		{Name: "active", Type: datatype.FieldTypeBool, Nullable: true},
		{Name: "at", Type: datatype.FieldTypeTimestamp, Nullable: true},
		{Name: "raw", Type: datatype.FieldTypeBytes, Nullable: true},
		{Name: "doc", Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "tags", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeString, Nullable: true},
		{Name: "day", Type: datatype.FieldTypeDate, Nullable: true},
		{Name: "clock", Type: datatype.FieldTypeTime, Nullable: true},
		{Name: "uid", Type: datatype.FieldTypeUUID, Nullable: true},
		{Name: "n", Type: datatype.FieldTypeInt, Nullable: true},
		{Name: "f", Type: datatype.FieldTypeFloat, Nullable: true},
		{Name: "d", Type: datatype.FieldTypeDouble, Nullable: true},
	}
	path := plugin.TabularItemPath(987655, plugin.EngineCatalogTermSchema, schema, "contract")
	writer := p
	for range 2 {
		if err := writer.PrepareTableWrite(ctx, info, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
			t.Fatalf("prepare/idempotent prepare: %v", err)
		}
	}
	const decimal = "12345678901234567890.123456789012345678"
	const label = "合成数据\t'引号'\n反斜线\\"
	stamp := time.Date(2026, 10, 14, 1, 2, 3, 123456000, time.UTC)
	row := map[string]interface{}{
		"id": int64(9223372036854775807), "label": label, "amount": decimal, "active": true, "at": stamp,
		"raw": []byte{0, 1, 255}, "doc": map[string]interface{}{"value": "合成", "large": int64(9007199254740993)},
		"tags": []string{"中文", "a,b", "\\quote\""}, "day": "2026-10-14", "clock": "01:02:03.123456",
		"uid": "12345678-1234-1234-1234-123456789abc", "n": int32(-2147483648), "f": float32(1.25), "d": float64(-2.5),
	}
	session, err := writer.OpenTableWriteSession(ctx, info, path, plugin.TableWriteSessionOptions{Method: "copy", Fields: fields})
	if err != nil {
		t.Fatalf("open COPY: %v", err)
	}
	defer session.Abort(context.Background())
	if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{row}}); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("commit COPY: %v", err)
	}
	marker, ok := session.(plugin.CommitMarkerProvider)
	if !ok || marker.CommitMarker() == nil || marker.CommitMarker().Provider != "highgo.table_write_session" {
		t.Fatal("successful COPY must expose commit marker")
	}
	var roundTrip bool
	if err := db.QueryRowContext(ctx, `SELECT label=$1 AND amount=$2::numeric AND active AND "at"=$3::timestamp
		AND raw=$4::bytea AND doc=$5::jsonb AND tags=$6::text[] AND day=DATE '2026-10-14'
		AND clock=TIME '01:02:03.123456' AND uid='12345678-1234-1234-1234-123456789abc'::uuid
		AND n=-2147483648 AND f=1.25 AND d=-2.5 FROM `+table+` WHERE id=9223372036854775807`,
		label, decimal, stamp, row["raw"], `{"value":"合成","large":9007199254740993}`, pq.Array(row["tags"])).Scan(&roundTrip); err != nil || !roundTrip {
		t.Fatalf("lossless COPY type roundtrip: equal=%t error=%v", roundTrip, err)
	}
	for _, method := range []string{"insert", "copy"} {
		id := int64(1)
		if method == "copy" {
			id = 2
		}
		batch := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"id": id, "raw": []byte{}, "doc": "null", "tags": []string{}}}}
		if err := writer.WriteBatch(ctx, info, path, batch, plugin.BatchWriteOptions{Method: method}); err != nil {
			t.Fatalf("batch %s: %v", method, err)
		}
		var nulls bool
		if err := db.QueryRowContext(ctx, `SELECT label IS NULL AND amount IS NULL AND active IS NULL AND "at" IS NULL
			AND raw='\x'::bytea AND doc='null'::jsonb AND tags='{}'::text[] FROM `+table+` WHERE id=$1`, id).Scan(&nulls); err != nil || !nulls {
			t.Fatalf("batch %s NULL/empty roundtrip: equal=%t error=%v", method, nulls, err)
		}
	}
	assertCount := func(want int) {
		t.Helper()
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != want {
			t.Fatalf("atomic row count: got=%d want=%d error=%v", count, want, err)
		}
	}
	assertCount(3)
	aborted, err := writer.OpenTableWriteSession(ctx, info, path, plugin.TableWriteSessionOptions{Method: "copy", Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	defer aborted.Abort(context.Background())
	if err := aborted.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{{"id": int64(3)}}}); err != nil {
		t.Fatal(err)
	}
	if err := aborted.Abort(ctx); err != nil {
		t.Fatalf("COPY abort: %v", err)
	}
	if provider := aborted.(plugin.CommitMarkerProvider); provider.CommitMarker() != nil {
		t.Fatal("aborted COPY exposed commit marker")
	}
	assertCount(3)
	failed, err := writer.OpenTableWriteSession(ctx, info, path, plugin.TableWriteSessionOptions{Method: "copy", Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	defer failed.Abort(context.Background())
	writeErr := failed.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{{"id": int64(4)}, {"id": int64(1)}}})
	closeErr := failed.Close(ctx)
	if writeErr == nil && closeErr == nil {
		t.Fatal("duplicate key COPY unexpectedly committed")
	}
	if provider := failed.(plugin.CommitMarkerProvider); provider.CommitMarker() != nil {
		t.Fatal("failed COPY exposed commit marker")
	}
	assertCount(3)
	// Force separate INSERT chunks; an error in chunk 2 must undo chunk 1.
	batch := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"id": int64(5)}, {"id": int64(1)}}, Hints: map[string]interface{}{"chunk_size": 1}}
	if err := writer.WriteBatch(ctx, info, path, batch, plugin.BatchWriteOptions{Method: "insert"}); err == nil {
		t.Fatal("duplicate key INSERT unexpectedly committed")
	}
	assertCount(3)
	t.Log("prepare, COPY commit, INSERT/COPY batches, exact types, Abort and failed-batch atomic rollback verified")
}

func highgoWriteTestConnection(t *testing.T, native bool) plugin.ConnectionInfo {
	t.Helper()
	prefix := "ADDP_TEST_POSTGRES_"
	defaults := map[string]string{"HOST": "localhost", "DATABASE": "addp_test", "USER": "addp", "SSLMODE": "disable"}
	if native {
		prefix = "ADDP_TEST_HIGHGO_"
		defaults = nil
	}
	value := func(key string) string {
		if result := os.Getenv(prefix + key); result != "" {
			return result
		}
		if result := defaults[key]; result != "" {
			return result
		}
		t.Fatalf("%s%s is required", prefix, key)
		return ""
	}
	port, err := strconv.Atoi(value("PORT"))
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("test port must be between 1 and 65535")
	}
	info := plugin.ConnectionInfo{"host": value("HOST"), "port": port, "database": value("DATABASE"), "user": value("USER"), "password": value("PASSWORD")}
	if native {
		info["sslmode"] = "require"
	} else {
		info["sslmode"] = value("SSLMODE")
	}
	return info
}
