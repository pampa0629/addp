package highgo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"strconv"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
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
	t.Cleanup(func() { plugin.ClosePool(987655) })
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	var probedVersion string
	if native {
		t.Cleanup(func() { plugin.ClosePool(0) })
		result, err := plugin.ProbeConnection(ctx, &plugin.Engine{EngineType: "highgo", ConnectionInfo: info})
		if err != nil {
			t.Fatalf("native V4.5 identity/SM3/TLS: %v", err)
		}
		probedVersion = result.ServerVersion
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
	if native && (probedVersion == "" || probedVersion != version) {
		t.Fatalf("probe returned %q, native version() returned %q", probedVersion, version)
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

	t.Run("guarded native pages", func(t *testing.T) { verifyHighGoGuardedPages(t, db, p, info, schema) })
	assertHighGoRelationalFacts(t, ctx, db, p, info, schema)

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
	assertCount := func(t *testing.T, want int) {
		t.Helper()
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != want {
			t.Fatalf("atomic row count: got=%d want=%d error=%v", count, want, err)
		}
	}
	assertCount(t, 3)
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
	assertCount(t, 3)
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
	assertCount(t, 3)
	// Force separate INSERT chunks; an error in chunk 2 must undo chunk 1.
	batch := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"id": int64(5)}, {"id": int64(1)}}, Hints: map[string]interface{}{"chunk_size": 1}}
	if err := writer.WriteBatch(ctx, info, path, batch, plugin.BatchWriteOptions{Method: "insert"}); err == nil {
		t.Fatal("duplicate key INSERT unexpectedly committed")
	}
	assertCount(t, 3)
	fingerprint := func(t *testing.T) string {
		t.Helper()
		var value string
		if err := db.QueryRowContext(ctx, "SELECT md5(coalesce(jsonb_agg(to_jsonb(r) ORDER BY id)::text,'[]')) FROM "+table+" r").Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	original := fingerprint(t)
	for _, mode := range []string{"abort", "failed_copy", "encoding_error", "prepare_error", "commit", "empty"} {
		t.Run("replace/"+mode, func(t *testing.T) {
			opts := plugin.TableWriteSessionOptions{Fields: fields, Method: "copy", Replace: true}
			if mode == "prepare_error" {
				opts.Fields = []datatype.FieldInfo{{Name: "missing_column", Type: datatype.FieldTypeBigInt}}
			}
			session, err := p.OpenTableWriteSession(ctx, info, path, opts)
			if mode == "prepare_error" {
				if err == nil {
					_ = session.Abort(ctx)
					t.Fatal("missing COPY column accepted")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer session.Abort(context.Background())
				var writeErr error
				if mode != "empty" {
					rows := []map[string]interface{}{{"id": int64(9), "label": "replacement"}}
					if mode == "encoding_error" {
						rows[0]["doc"] = "{"
					}
					if mode == "failed_copy" {
						rows = append(rows, rows[0])
					}
					writeErr = session.WriteBatch(ctx, &plugin.BatchData{Rows: rows})
				}
				if mode == "abort" {
					if err := session.Abort(ctx); err != nil {
						t.Fatal(err)
					}
				} else {
					closeErr := session.Close(ctx)
					if mode == "failed_copy" || mode == "encoding_error" {
						if writeErr == nil && closeErr == nil {
							t.Fatal("duplicate replacement committed")
						}
					} else if writeErr != nil || closeErr != nil {
						t.Fatalf("write=%v close=%v", writeErr, closeErr)
					}
				}
				marker := session.(plugin.CommitMarkerProvider).CommitMarker()
				if mode == "abort" || mode == "failed_copy" || mode == "encoding_error" {
					if marker != nil {
						t.Fatal("failed replacement exposed commit marker")
					}
				} else if marker == nil || marker.Provider != "highgo.table_write_session" {
					t.Fatal("replacement commit marker missing or identity lost")
				}
			}
			switch mode {
			case "commit":
				assertCount(t, 1)
				var id int64
				if err := db.QueryRowContext(ctx, "SELECT id FROM "+table).Scan(&id); err != nil || id != 9 {
					t.Fatalf("replacement row mismatch: %d %v", id, err)
				}
			case "empty":
				assertCount(t, 0)
			default:
				if fingerprint(t) != original {
					t.Fatal("replacement rollback changed existing values")
				}
			}
		})
	}
	if err := p.WriteBatch(ctx, info, path, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{row, {"id": int64(1)}, {"id": int64(2)}}}, plugin.BatchWriteOptions{Method: "copy"}); err != nil {
		t.Fatal(err)
	}
	assertCount(t, 3)
	keepPath := plugin.TabularItemPath(987655, plugin.EngineCatalogTermSchema, schema, `keep "quoted"`)
	keepTable := quotedSchema + `.` + pq.QuoteIdentifier(`keep "quoted"`)
	view := quotedSchema + `."contract_guard"`
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		for _, statement := range []string{"DROP VIEW IF EXISTS " + view + " RESTRICT", "DROP TABLE IF EXISTS " + keepTable + " RESTRICT"} {
			if _, err := db.ExecContext(cleanup, statement); err != nil {
				t.Errorf("cleanup deletion fixture: %v", err)
			}
		}
	})
	keepFields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}
	if err := p.PrepareTableWrite(ctx, info, keepPath, plugin.TableWriteOptions{Fields: keepFields}); err != nil {
		t.Fatal(err)
	}
	if err := p.WriteBatch(ctx, info, keepPath, &plugin.BatchData{Fields: keepFields, Rows: []map[string]interface{}{{"id": int64(42)}}}, plugin.BatchWriteOptions{Method: "insert"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE VIEW "+view+" AS SELECT id FROM "+table); err != nil {
		t.Fatal(err)
	}
	viewPath := plugin.TabularItemPath(987655, plugin.EngineCatalogTermSchema, schema, "contract_guard")
	if err := p.DeleteResource(ctx, info, viewPath); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("non-table relation was not rejected by ordinary table guard: %v", err)
	}
	if err := p.DeleteResource(ctx, info, path); err == nil {
		t.Fatal("dependent view must prevent deletion; no CASCADE")
	}
	assertCount(t, 3)
	if _, err := db.ExecContext(ctx, "DROP VIEW "+view); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteResource(ctx, info, path); err != nil {
		t.Fatalf("delete ordinary table: %v", err)
	}
	if err := p.DeleteResource(ctx, info, path); err != nil {
		t.Fatalf("repeat delete: %v", err)
	}
	var preserved bool
	if err := db.QueryRowContext(ctx, "SELECT count(*)=1 AND min(id)=42 FROM "+keepTable).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("sibling changed: %t %v", preserved, err)
	}
	if err := p.DeleteResource(ctx, info, keepPath); err != nil {
		t.Fatalf("delete quoted table: %v", err)
	}
	var remains bool
	if err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL OR to_regclass($2) IS NOT NULL", table, keepTable).Scan(&remains); err != nil || remains {
		t.Fatalf("table delete not verified: %t %v", remains, err)
	}
	t.Log("prepare, COPY commit, INSERT/COPY batches, exact types, Abort, failed-batch rollback, transactional replace commit/empty/Abort/failure, restricted delete, quoted identifiers and sibling preservation verified")
}

// Native mode uses only the existing business login and SELECTs. The same
// shared runtime contract also runs in the disposable PostgreSQL gate.
func TestIntegrationHighGoSharedSQLPoolTimeout(t *testing.T) {
	native := os.Getenv("ADDP_HIGHGO_INTEGRATION") == "1"
	if !native && os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run scripts/test/common-postgres-gate.sh --test shared-sql-pool [--highgo-native]")
	}
	info := highgoWriteTestConnection(t, native)
	p := &Plugin{}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if native {
		identity, err := p.ProbeConnection(ctx, info)
		if err != nil {
			t.Fatalf("native V4.5 identity/SM3/TLS: %v", err)
		}
		t.Logf("native=true server=%s", identity.ServerVersion)
	}
	const engineID = 987657
	ctx = plugin.WithPoolEngineVersion(ctx, engineID, 1)
	// Reuse the connection returned by the queue control before starting the
	// SQL deadline; a cold authentication timeout is an acquisition result.
	info["pool_max_open_conns"], info["pool_max_idle_conns"] = 1, 1
	t.Cleanup(func() {
		if err := plugin.ClosePool(engineID); err != nil {
			t.Errorf("close owned pool: %v", err)
		}
	})
	pool, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{ID: engineID, EngineType: p.Type(), Version: 1, ConnectionInfo: info}, nil)
	if err != nil {
		t.Fatal(err)
	}
	db, err := pool.DB()
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	opts := plugin.QueryOptions{EngineID: engineID, EngineType: p.Type(), ReadOnly: true}
	wait, stop := context.WithTimeout(ctx, 150*time.Millisecond)
	_, err = p.ExecuteSQL(wait, info, "SELECT 1", opts)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) || plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeConnectionAcquisitionTimeout {
		t.Fatalf("HighGo pool wait lost its stage: %v (code=%q)", err, plugin.QueryErrorCodeOf(err))
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	if db.Stats().Idle != 1 {
		t.Fatal("SQL timeout control requires the released warm connection")
	}
	query, stop := context.WithTimeout(ctx, 150*time.Millisecond)
	started := time.Now()
	_, err = p.ExecuteSQL(query, info, "SELECT pg_sleep(10)", opts)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) || plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeSQLExecutionTimeout || time.Since(started) > 3*time.Second {
		t.Fatalf("HighGo SQL timeout lost its stage or exceeded cancellation bound: %v (code=%q)", err, plugin.QueryErrorCodeOf(err))
	}
	result, err := p.ExecuteSQL(ctx, info, "SELECT current_setting('transaction_read_only') AS mode", opts)
	if err != nil || len(result.Rows) != 1 || result.Rows[0]["mode"] != "on" {
		t.Fatalf("HighGo shared query did not recover its read-only transaction: %+v, %v", result, err)
	}
	t.Logf("native=%t shared pool acquisition timeout, SQL timeout and read-only recovery verified", native)
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

// Runs both on the standard disposable PG protocol gate and explicitly on the
// native HighGo fixture. It owns only objects inside the parent's random schema.
func verifyHighGoGuardedPages(t *testing.T, db *sql.DB, p *Plugin, info plugin.ConnectionInfo, schema string) {
	t.Helper()
	ctx := plugin.WithPoolEngineVersion(t.Context(), 987642, 1)
	info = maps.Clone(info)
	info["pool_max_open_conns"], info["pool_max_idle_conns"] = 1, 0
	t.Cleanup(func() { _ = plugin.ClosePool(987642) })
	qualified := pq.QuoteIdentifier(schema) + `."guarded_page"`
	view := pq.QuoteIdentifier(schema) + `."guarded_view"`
	child := pq.QuoteIdentifier(schema) + `."guarded_child"`
	defer func() {
		for _, q := range []string{"DROP VIEW IF EXISTS " + view, "DROP TABLE IF EXISTS " + child, "DROP TABLE IF EXISTS " + qualified} {
			if _, e := db.ExecContext(context.Background(), q); e != nil {
				t.Error(e)
			}
		}
	}()
	for _, q := range []string{
		"CREATE TABLE " + qualified + " (id BIGINT PRIMARY KEY, value NUMERIC(38,18), label TEXT)",
		"INSERT INTO " + qualified + " VALUES (9007199254740993,12345678901234567890.123456789012345678,'kept '),(9007199254740994,NULL,'kept '),(9007199254740995,1,'other')",
		"CREATE VIEW " + view + " AS SELECT * FROM " + qualified,
	} {
		if _, e := db.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	path := plugin.TabularItemPath(987642, "schema", schema, "guarded_page")
	opts := plugin.TableReadSessionOptions{Page: &plugin.CatalogTablePageOptions{Limit: 1, Offset: 1, Predicate: `"label" = $1`, Args: []any{"kept "}}}
	checks := 0
	check := func(_ context.Context, set *plugin.QueryReadSet, fields []datatype.FieldInfo) error {
		checks++
		if len(set.Paths) != 1 || set.Paths[0].EngineID != path.EngineID || set.Paths[0].Segments[2].Name != "guarded_page" || len(fields) != 3 || fields[1].Precision != 38 || fields[1].Scale != 18 || !fields[0].PrimaryKey {
			t.Fatalf("locked actual source/fields: %#v %#v", set, fields)
		}
		return nil
	}
	denied := errors.New("current source revoked")
	if s, e := p.OpenGuardedTableReadSession(ctx, info, path, opts, func(c context.Context, r *plugin.QueryReadSet, f []datatype.FieldInfo) error {
		_ = check(c, r, f)
		return denied
	}); s != nil || !errors.Is(e, denied) {
		t.Fatalf("denied source opened: %v", e)
	}
	s, e := p.OpenGuardedTableReadSession(ctx, info, path, opts, check)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(context.Background())
	batch, e := s.ReadBatch(ctx, 10)
	if e != nil || len(batch.Rows) != 1 || batch.Offset != 1 || fmt.Sprint(batch.Rows[0]["id"]) != "9007199254740994" || batch.Rows[0]["value"] != nil || batch.Rows[0]["label"] != "kept " {
		t.Fatalf("bounded native page: %#v %v", batch, e)
	}
	set, e := s.(plugin.ReadSessionEvidence).ReadSet(ctx)
	if e != nil || len(set.Paths) != 1 || set.Paths[0].EngineID != path.EngineID {
		t.Fatalf("read session evidence: %#v %v", set, e)
	}
	// DDL cannot replace the source in the authorization/execution interval.
	ddl, stop := context.WithTimeout(t.Context(), 150*time.Millisecond)
	_, ddlErr := db.ExecContext(ddl, "ALTER TABLE "+qualified+" ADD COLUMN forbidden INT")
	stop()
	if ddlErr == nil {
		t.Fatal("concurrent DDL bypassed locked source")
	}
	if e := s.Close(ctx); e != nil {
		t.Fatal(e)
	}
	opts.Page.Offset = 0
	s, e = p.OpenGuardedTableReadSession(ctx, info, path, opts, check)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(context.Background())
	batch, e = s.ReadBatch(ctx, 1)
	if e != nil || len(batch.Rows) != 1 || fmt.Sprint(batch.Rows[0]["id"]) != "9007199254740993" || fmt.Sprint(batch.Rows[0]["value"]) != "12345678901234567890.123456789012345678" {
		t.Fatalf("exact recovered first page: %#v %v", batch, e)
	}
	if e := s.Close(ctx); e != nil {
		t.Fatal(e)
	}
	pool, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{ID: path.EngineID, EngineType: p.Type(), Version: 1, ConnectionInfo: info}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pooledDB, err := pool.DB()
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := pooledDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	wait, stop := context.WithTimeout(t.Context(), 150*time.Millisecond)
	wait = plugin.WithPoolEngineVersion(wait, path.EngineID, 1)
	_, waitErr := p.OpenGuardedTableReadSession(wait, info, path, opts, check)
	stop()
	if !errors.Is(waitErr, context.DeadlineExceeded) || plugin.QueryErrorCodeOf(waitErr) != plugin.QueryErrorCodeConnectionAcquisitionTimeout {
		t.Fatalf("guarded page bypassed shared pool wait: %v", waitErr)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	if pooledDB.Stats().InUse != 0 || pooledDB.PingContext(ctx) != nil {
		t.Fatal("guarded session closed or leaked shared pool")
	}
	before := checks
	viewPath := plugin.TabularItemPath(path.EngineID, "schema", schema, "guarded_view")
	if s, e := p.OpenGuardedTableReadSession(ctx, info, viewPath, opts, check); s != nil || !errors.Is(e, plugin.ErrQueryReadSetUnresolved) || checks != before {
		t.Fatalf("view reached guarded authorization: %v", e)
	}
	opts.Page.Limit = 10
	s, e = p.OpenGuardedTableReadSession(ctx, info, path, opts, func(c context.Context, set *plugin.QueryReadSet, fields []datatype.FieldInfo) error {
		if err := check(c, set, fields); err != nil {
			return err
		}
		// ACCESS SHARE does not freeze the inheritance graph. Add a child
		// after proof/authorization and before the provider opens its cursor.
		ddl, stop := context.WithTimeout(c, 3*time.Second)
		defer stop()
		if _, err := db.ExecContext(ddl, "CREATE TABLE "+child+" () INHERITS ("+qualified+")"); err != nil {
			return err
		}
		_, err := db.ExecContext(ddl, "INSERT INTO "+child+" VALUES (99,99,'kept ')")
		return err
	})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(context.Background())
	batch, e = s.ReadBatch(ctx, 10)
	if e != nil || len(batch.Rows) != 2 {
		t.Fatalf("concurrent inherited rows entered the read: %#v %v", batch, e)
	}
	for _, row := range batch.Rows {
		if fmt.Sprint(row["id"]) == "99" {
			t.Fatal("ONLY source boundary was lost")
		}
	}
	if e := s.Close(ctx); e != nil {
		t.Fatal(e)
	}
	before = checks
	if s, e := p.OpenGuardedTableReadSession(ctx, info, path, opts, check); s != nil || !errors.Is(e, plugin.ErrQueryReadSetUnresolved) || checks != before {
		t.Fatalf("inherited source reached guarded authorization: %v", e)
	}
	t.Log("actual HighGo read set, current authorization refusal, typed filtered page, DDL exclusion, view/inheritance refusal, shared-pool recovery verified")
}
