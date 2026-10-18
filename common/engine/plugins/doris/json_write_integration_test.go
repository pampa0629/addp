package doris

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/writetest"
	"github.com/google/uuid"
)

func TestIntegrationDorisTypedJSONWrites(t *testing.T) {
	if os.Getenv("ADDP_DORIS_INTEGRATION") != "1" {
		t.Skip("set ADDP_DORIS_INTEGRATION=1 to run Doris integration test")
	}
	t.Run("definition_before_ddl", testDorisDefinitionBeforeDDL)
	t.Run("session_failures", func(t *testing.T) {
		writetest.AutoCommitSessionFailures(t, openDorisFailureIntegrationSession)
	})
	t.Run("floating_value_and_insert_projection", testDorisFloatingValueAndInsertProjection)
	for _, method := range []string{"session", "batch"} {
		t.Run(method, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			connInfo := dorisIntegrationConnInfo(t)
			p := &DorisPlugin{}
			database := plugin.GetString(connInfo, "database")
			table := "addp_json_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
			path := plugin.TabularItemPath(93002, plugin.EngineCatalogTermDatabase, database, table)
			defer func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cleanupCancel()
				if err := p.DeleteResource(cleanupCtx, connInfo, path); err != nil {
					t.Errorf("cleanup JSON table: %v", err)
				}
			}()
			fields := []datatype.FieldInfo{
				{Name: "id", Type: datatype.FieldTypeBigInt},
				{Name: "document", Type: datatype.FieldTypeJSON, Nullable: true},
				{Name: "payload", Type: datatype.FieldTypeBytes, Nullable: true},
				{Name: "label", Type: datatype.FieldTypeString, Nullable: true},
			}
			if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
				t.Fatal(err)
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
			qualified := dorisDialect().QualifiedTable(database, table)
			var columnType string
			if err := db.QueryRowContext(ctx, "SELECT column_type FROM information_schema.columns WHERE table_schema=? AND table_name=? AND column_name='document'", database, table).Scan(&columnType); err != nil || !strings.EqualFold(columnType, "string") {
				t.Fatalf("JSON storage type=%q want STRING err=%v", columnType, err)
			}
			cases := []struct {
				value interface{}
				text  string // Empty text means SQL NULL; valid JSON text is never empty.
			}{
				{[]interface{}{1, "中文", false, nil}, `[1,"中文",false,null]`},
				{map[string]interface{}{"items": []interface{}{true}}, `{"items":[true]}`},
				{[]interface{}{}, "[]"}, {map[string]interface{}{}, "{}"},
				{[]interface{}(nil), "null"}, {map[string]interface{}(nil), "null"},
				{nil, ""}, {[]byte(nil), ""}, {json.RawMessage(nil), ""},
				{true, "true"}, {int64(9007199254740993), "9007199254740993"},
				{json.Number("12345678901234567890.123456789"), "12345678901234567890.123456789"}, {1.25, "1.25"},
				{` {"n":9007199254740993} `, ` {"n":9007199254740993} `},
				{[]byte(`[1,null]`), `[1,null]`}, {json.RawMessage(`{"raw":true}`), `{"raw":true}`},
				{`"中文\n字符串"`, `"中文\n字符串"`}, {`""`, `""`}, {"null", "null"},
				{map[string]interface{}{"large": strings.Repeat("x", 8192)}, `{"large":"` + strings.Repeat("x", 8192) + `"}`},
			}
			rows := make([]map[string]interface{}, len(cases))
			binaries := [][]byte{{0, 1, 255}, {}, nil, {39, 92, 10, 255}}
			for i, tc := range cases {
				rows[i] = map[string]interface{}{"id": int64(i + 1), "document": tc.value, "payload": binaries[i%len(binaries)], "label": "plain text {"}
			}
			write := func(rows []map[string]interface{}) error {
				if method == "batch" {
					return p.WriteBatch(ctx, connInfo, path, &plugin.BatchData{Fields: fields, Rows: rows}, plugin.BatchWriteOptions{})
				}
				openingFields := append([]datatype.FieldInfo(nil), fields...)
				session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: openingFields})
				if err != nil {
					return err
				}
				defer session.Abort(ctx)
				openingFields[1].Type = datatype.FieldTypeString
				if err := session.WriteBatch(ctx, &plugin.BatchData{
					Fields: []datatype.FieldInfo{{Name: "document", Type: datatype.FieldTypeString}}, Rows: rows,
				}); err != nil {
					return err
				}
				return session.Close(ctx)
			}
			if err := write(rows); err != nil {
				t.Fatalf("write JSON matrix: %v", err)
			}
			if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
				t.Fatalf("revalidate existing JSON/bytes storage: %v", err)
			}
			stored, err := db.QueryContext(ctx, "SELECT id, document, payload, label, payload IS NULL FROM "+qualified+" ORDER BY id")
			if err != nil {
				t.Fatal(err)
			}
			defer stored.Close()
			index := 0
			for stored.Next() {
				var id int64
				var document sql.NullString
				var payload []byte
				var label string
				var payloadNull bool
				if err := stored.Scan(&id, &document, &payload, &label, &payloadNull); err != nil {
					stored.Close()
					t.Fatal(err)
				}
				if index >= len(cases) || id != int64(index+1) {
					t.Fatalf("unexpected row id=%d index=%d", id, index)
				}
				want := cases[index].text
				if document.Valid != (want != "") || document.String != want {
					t.Fatalf("row %d JSON=%#v want=%q", id, document, want)
				}
				wantBinary := binaries[index%len(binaries)]
				if !bytes.Equal(payload, wantBinary) || payloadNull != (wantBinary == nil) || label != "plain text {" {
					t.Fatalf("row %d binary=%v null=%v label=%q", id, payload, payloadNull, label)
				}
				index++
			}
			if err := stored.Err(); err != nil || index != len(cases) {
				t.Fatalf("read %d/%d rows err=%v", index, len(cases), err)
			}
			stored.Close()
			// The invalid JSON follows a complete INSERT chunk. Preflight must
			// reject the batch before the first chunk is committed.
			for _, invalid := range []interface{}{"{", []byte{}, json.RawMessage("{} []"), math.NaN(), make(chan int), string([]byte{'"', 0xff, '"'}), []byte{'"', 0xff, '"'}, json.RawMessage([]byte{'"', 0xff, '"'})} {
				invalidRows := make([]map[string]interface{}, dorisDefaultInsertChunkSize+1)
				for i := range invalidRows {
					invalidRows[i] = map[string]interface{}{"id": int64(100 + i), "document": map[string]interface{}{"valid": true}}
				}
				invalidRows[len(invalidRows)-1]["document"] = invalid
				writeErr := write(invalidRows)
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != len(cases) {
					t.Fatalf("failed batch changed committed rows: count=%d want=%d err=%v", count, len(cases), err)
				}
				var typed *plugin.TableWriteError
				if !errors.As(writeErr, &typed) || typed.Code != plugin.TableWriteJSONInvalid || typed.Field == nil || typed.Field.ColumnIndex != 2 || typed.Field.DeclaredType != datatype.FieldTypeJSON {
					t.Fatalf("lost structured JSON column: %#v error=%v", typed, writeErr)
				}

				if writeErr == nil || !strings.Contains(writeErr.Error(), `row 1000 field "document"`) {
					t.Fatalf("invalid %T expected row/field error, got %v", invalid, writeErr)
				}
			}
			chunkRows := make([]map[string]interface{}, dorisDefaultInsertChunkSize+1)
			for i := range chunkRows {
				chunkRows[i] = map[string]interface{}{"id": int64(100 + i), "document": map[string]interface{}{"row": i}}
			}
			if err := write(chunkRows); err != nil {
				t.Fatalf("valid cross-chunk JSON: %v", err)
			}
			chunkStored, err := db.QueryContext(ctx, "SELECT id, document FROM "+qualified+" WHERE id >= 100 ORDER BY id")
			if err != nil {
				t.Fatal(err)
			}
			defer chunkStored.Close()
			chunkIndex := 0
			for chunkStored.Next() {
				var id int64
				var document string
				if err := chunkStored.Scan(&id, &document); err != nil {
					t.Fatal(err)
				}
				want, _ := json.Marshal(map[string]interface{}{"row": chunkIndex})
				if id != int64(100+chunkIndex) || document != string(want) {
					t.Fatalf("chunk row id=%d JSON=%q want id=%d JSON=%q", id, document, 100+chunkIndex, want)
				}
				chunkIndex++
			}
			if err := chunkStored.Err(); err != nil || chunkIndex != len(chunkRows) {
				t.Fatalf("read cross-chunk rows=%d want=%d err=%v", chunkIndex, len(chunkRows), err)
			}
			chunkStored.Close()
			// Abort cannot undo the prior batch in the same autocommit session.
			if method == "session" {
				session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: fields})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Abort(ctx)
				if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{{"id": int64(9999), "document": []interface{}{true}}}}); err != nil {
					t.Fatal(err)
				}
				if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{{"id": int64(10000), "document": "bad"}}}); err == nil {
					t.Fatal("invalid JSON unexpectedly accepted")
				}
				if err := session.Abort(ctx); err != nil {
					t.Fatal(err)
				}
				if session.(plugin.CommitMarkerProvider).CommitMarker() != nil {
					t.Fatal("aborted session published a commit marker")
				}
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified+" WHERE id=9999").Scan(&count); err != nil || count != 1 {
					t.Fatalf("prior committed batch count=%d want=1 err=%v", count, err)
				}
			}
			if err := write([]map[string]interface{}{{"id": int64(20000), "document": []interface{}{true}}}); err != nil {
				t.Fatalf("write after rejection: %v", err)
			}
		})
	}
}

func openDorisFailureIntegrationSession(t *testing.T) (plugin.TableWriteSession, *sql.DB, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	conn := dorisIntegrationConnInfo(t)
	p := &DorisPlugin{}
	database := plugin.GetString(conn, "database")
	table := "addp_session_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	path := plugin.TabularItemPath(93002, plugin.EngineCatalogTermDatabase, database, table)
	dsn, err := p.serverDSN(conn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const catalogSQL = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?"
	var count uint64
	if err := db.QueryRowContext(ctx, catalogSQL, database, table).Scan(&count); err != nil || count != 0 {
		t.Fatalf("fixture name unavailable: count=%d error=%v", count, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := p.DeleteResource(cleanupCtx, conn, path); err != nil {
			t.Errorf("cleanup session table: %v", err)
		}
		if err := db.QueryRowContext(cleanupCtx, catalogSQL, database, table).Scan(&count); err != nil || count != 0 {
			t.Errorf("session cleanup not proven: count=%d error=%v", count, err)
		}
	})
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "document", Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "payload", Type: datatype.FieldTypeBytes, Nullable: true},
		{Name: "label", Type: datatype.FieldTypeString, Nullable: true},
	}
	qualified := dorisDialect().QualifiedTable(database, table)
	// This disposable fixture must reject filtered rows rather than silently
	// accept them. Newly opened writer sessions inherit the global setting.
	if _, err := db.ExecContext(ctx, "SET GLOBAL enable_insert_strict=true"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+` (id BIGINT NOT NULL, document STRING NULL, payload STRING NULL, label STRING NULL) DUPLICATE KEY(id) PARTITION BY RANGE(id) (PARTITION bounded_write VALUES LESS THAN ("1002")) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES ("replication_num"="1")`); err != nil {
		t.Fatal(err)
	}
	if err := p.PrepareTableWrite(ctx, conn, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatal(err)
	}
	session, err := p.OpenTableWriteSession(ctx, conn, path, plugin.TableWriteSessionOptions{Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	return session, db, dorisDialect().QualifiedTable(database, table)
}

func testDorisDefinitionBeforeDDL(t *testing.T) {
	p := &DorisPlugin{}
	conn := dorisIntegrationConnInfo(t)
	dsn, err := p.serverDSN(conn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, fields := range [][]datatype.FieldInfo{
		{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "payload", Type: datatype.FieldTypeArray}},
		{{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3}},
		{{Name: "payload", Type: datatype.FieldTypeJSON}},
		{{Name: "score", Type: datatype.FieldTypeDouble}},
	} {
		t.Run(string(fields[len(fields)-1].Type), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			database := "addp_rejected_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cleanupCancel()
				if _, err := db.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS "+dorisDialect().QuoteIdentifier(database)); err != nil {
					t.Errorf("cleanup rejected definition: %v", err)
				}
				var count int
				if err := db.QueryRowContext(cleanupCtx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name=?", database).Scan(&count); err != nil || count != 0 {
					t.Errorf("rejected definition cleanup not proven: count=%d error=%v", count, err)
				}
			})
			path := plugin.TabularItemPath(93002, plugin.EngineCatalogTermDatabase, database, "target")
			if err := p.PrepareTableWrite(ctx, conn, path, plugin.TableWriteOptions{Fields: fields}); err == nil || !strings.Contains(err.Error(), "target_definition") {
				t.Fatalf("invalid definition reached DDL: %v", err)
			}
			var count int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name=?", database).Scan(&count); err != nil || count != 0 {
				t.Fatalf("invalid definition created database: count=%d error=%v", count, err)
			}
		})
	}
}

func testDorisFloatingValueAndInsertProjection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	p := &DorisPlugin{}
	conn := dorisIntegrationConnInfo(t)
	database := plugin.GetString(conn, "database")
	table := "addp_definition_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	path := plugin.TabularItemPath(93002, plugin.EngineCatalogTermDatabase, database, table)
	dsn, err := p.serverDSN(conn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := p.DeleteResource(cleanupCtx, conn, path); err != nil {
			t.Errorf("cleanup definition table: %v", err)
		}
		var count int
		if err := db.QueryRowContext(cleanupCtx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?", database, table).Scan(&count); err != nil || count != 0 {
			t.Errorf("definition table cleanup not proven: count=%d error=%v", count, err)
		}
	}()
	fields := []datatype.FieldInfo{{Name: "score", Type: datatype.FieldTypeDouble, Nullable: true}, {Name: "id", Type: datatype.FieldTypeBigInt, Nullable: true}, {Name: "document", Type: datatype.FieldTypeJSON, Nullable: true}}
	if err := p.PrepareTableWrite(ctx, conn, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatal(err)
	}
	var first string
	if err := db.QueryRowContext(ctx, "SELECT column_name FROM information_schema.columns WHERE table_schema=? AND table_name=? AND ordinal_position=1", database, table).Scan(&first); err != nil || first != "id" {
		t.Fatalf("floating column chosen as key: first=%s error=%v", first, err)
	}
	if err := p.WriteBatch(ctx, conn, path, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"id": int64(1), "score": 1.25, "document": map[string]interface{}{"ok": true}}}}, plugin.BatchWriteOptions{}); err != nil {
		t.Fatal(err)
	}
	qualified := dorisDialect().QualifiedTable(database, table)
	var score float64
	if err := db.QueryRowContext(ctx, "SELECT score FROM "+qualified+" WHERE id=1").Scan(&score); err != nil || score != 1.25 {
		t.Fatalf("floating value changed: %v error=%v", score, err)
	}
	invalid := append(append([]datatype.FieldInfo(nil), fields...), datatype.FieldInfo{Name: "payload", Type: datatype.FieldTypeArray, Nullable: true})
	if err := p.PrepareTableWrite(ctx, conn, path, plugin.TableWriteOptions{Fields: invalid}); err == nil || !strings.Contains(err.Error(), `column "payload"`) {
		t.Fatalf("existing table accepted native array: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=? AND table_name=? AND column_name='payload'", database, table).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid field added to target: count=%d error=%v", count, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != 1 {
		t.Fatalf("sentinel changed: count=%d error=%v", count, err)
	}
	// A session writes a projection of an existing table; it need not include
	// the key column required by full-table preparation.
	session, err := p.OpenTableWriteSession(ctx, conn, path, plugin.TableWriteSessionOptions{Fields: fields[2:]})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{{"document": []interface{}{true}}}}); err != nil {
		_ = session.Abort(ctx)
		t.Fatal(err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != 2 {
		t.Fatalf("projection insert failed: count=%d error=%v", count, err)
	}
}
