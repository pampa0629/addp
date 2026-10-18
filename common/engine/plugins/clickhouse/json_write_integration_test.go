package clickhouse

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/writetest"
	"github.com/google/uuid"
)

func TestIntegrationClickHouseTypedJSONWrites(t *testing.T) {
	if os.Getenv("ADDP_CLICKHOUSE_INTEGRATION") != "1" {
		t.Skip("set ADDP_CLICKHOUSE_INTEGRATION=1 to run ClickHouse integration test")
	}
	t.Run("definition_before_ddl", testClickHouseDefinitionBeforeDDL)
	t.Run("session_failures", func(t *testing.T) {
		writetest.AutoCommitSessionFailures(t, openClickHouseFailureIntegrationSession)
	})
	t.Run("definition_preserves_target_and_projection", testClickHouseDefinitionPreservesTargetAndProjection)
	for _, method := range []string{"session", "batch"} {
		t.Run(method, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
			defer cancel()
			connInfo := clickhouseIntegrationConnInfo(t)
			p := &ClickHousePlugin{}
			database := plugin.GetString(connInfo, "database")
			table := "addp_json_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
			path := plugin.TabularItemPath(93003, plugin.EngineCatalogTermDatabase, database, table)
			dsn, err := p.serverDSN(connInfo)
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("clickhouse", dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cleanupCancel()
				defer db.Close()
				if err := p.DeleteResource(cleanupCtx, connInfo, path); err != nil {
					t.Errorf("cleanup JSON table: %v", err)
				}
				var remaining uint64
				if err := db.QueryRowContext(cleanupCtx, "SELECT count() FROM system.tables WHERE database=? AND name=?", database, table).Scan(&remaining); err != nil || remaining != 0 {
					t.Errorf("JSON fixture cleanup not proven: tables=%d error=%v", remaining, err)
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
			var physical string
			if err := db.QueryRowContext(ctx, "SELECT type FROM system.columns WHERE database=? AND table=? AND name='document'", database, table).Scan(&physical); err != nil || physical != "Nullable(String)" {
				t.Fatalf("JSON storage type=%q want Nullable(String) error=%v", physical, err)
			}
			cases := []struct {
				value interface{}
				text  string // Empty means SQL NULL; a JSON document is never empty.
			}{
				{[]interface{}{map[string]interface{}{"ok": true}, "中文", nil}, `[{"ok":true},"中文",null]`},
				{map[string]interface{}{"items": []interface{}{true, nil}}, `{"items":[true,null]}`},
				{[]interface{}{}, "[]"}, {map[string]interface{}{}, "{}"},
				{[]interface{}(nil), "null"}, {map[string]interface{}(nil), "null"},
				{nil, ""}, {[]byte(nil), ""}, {json.RawMessage(nil), ""},
				{false, "false"}, {int64(9007199254740993), "9007199254740993"}, {1.25, "1.25"},
				{json.Number("12345678901234567890.123456789"), "12345678901234567890.123456789"},
				{` {"n":9007199254740993} `, ` {"n":9007199254740993} `},
				{[]byte(`[1,null]`), `[1,null]`}, {json.RawMessage(`{"raw":true}`), `{"raw":true}`},
				{`"中文\n字符串"`, `"中文\n字符串"`}, {`""`, `""`}, {"null", "null"},
				{map[string]interface{}{"large": strings.Repeat("x", 8192)}, `{"large":"` + strings.Repeat("x", 8192) + `"}`},
			}
			binaries := [][]byte{{0, 1, 255}, {}, nil, {39, 92, 10, 255}}
			rows := make([]map[string]interface{}, len(cases))
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
				if err := session.WriteBatch(ctx, &plugin.BatchData{Fields: []datatype.FieldInfo{{Name: "document", Type: datatype.FieldTypeString}}, Rows: rows}); err != nil {
					return err
				}
				return session.Close(ctx)
			}
			if err := write(rows); err != nil {
				t.Fatalf("write JSON matrix: %v", err)
			}
			if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
				t.Fatalf("revalidate JSON/bytes storage: %v", err)
			}
			qualified := clickhouseDialect().QualifiedTable(database, table)
			stored, err := db.QueryContext(ctx, "SELECT id, document, payload, isNull(payload), label FROM "+qualified+" ORDER BY id")
			if err != nil {
				t.Fatal(err)
			}
			defer stored.Close()
			index := 0
			for stored.Next() {
				var id int64
				var document sql.NullString
				var payload []byte
				var payloadNull bool
				var label string
				if err := stored.Scan(&id, &document, &payload, &payloadNull, &label); err != nil {
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
					t.Fatalf("row %d bytes=%v null=%v label=%q", id, payload, payloadNull, label)
				}
				index++
			}
			if err := stored.Err(); err != nil || index != len(cases) {
				t.Fatalf("read %d/%d rows error=%v", index, len(cases), err)
			}
			stored.Close()
			for _, invalid := range []interface{}{"{", []byte{}, json.RawMessage("{} []"), math.NaN(), make(chan int), string([]byte{'"', 0xff, '"'}), []byte{'"', 0xff, '"'}, json.RawMessage([]byte{'"', 0xff, '"'})} {
				invalidRows := make([]map[string]interface{}, clickhouseDefaultInsertChunkSize+1)
				for i := range invalidRows {
					invalidRows[i] = map[string]interface{}{"id": int64(100 + i), "document": map[string]interface{}{"valid": true}}
				}
				invalidRows[len(invalidRows)-1]["document"] = invalid
				writeErr := write(invalidRows)
				var typed *plugin.TableWriteError
				if !errors.As(writeErr, &typed) || typed.Code != plugin.TableWriteJSONInvalid || typed.Field == nil || typed.Field.ColumnIndex != 2 || typed.Field.DeclaredType != datatype.FieldTypeJSON {
					t.Fatalf("lost structured JSON column: %#v error=%v", typed, writeErr)
				}

				if writeErr == nil || !strings.Contains(writeErr.Error(), `row 1000 field "document"`) {
					t.Fatalf("invalid %T expected row/field error, got %v", invalid, writeErr)
				}
				var count uint64
				if err := db.QueryRowContext(ctx, "SELECT count() FROM "+qualified).Scan(&count); err != nil || count != uint64(len(cases)) {
					t.Fatalf("invalid batch changed rows: count=%d want=%d error=%v", count, len(cases), err)
				}
			}
			chunkRows := make([]map[string]interface{}, clickhouseDefaultInsertChunkSize+1)
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
				if id != int64(100+chunkIndex) || document != `{"row":`+strconv.Itoa(chunkIndex)+`}` {
					t.Fatalf("cross-chunk row %d JSON=%q", id, document)
				}
				chunkIndex++
			}
			if err := chunkStored.Err(); err != nil || chunkIndex != len(chunkRows) {
				t.Fatalf("cross-chunk read %d/%d rows error=%v", chunkIndex, len(chunkRows), err)
			}
		})
	}
}

func openClickHouseFailureIntegrationSession(t *testing.T) (plugin.TableWriteSession, *sql.DB, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	conn := clickhouseIntegrationConnInfo(t)
	p := &ClickHousePlugin{}
	database := plugin.GetString(conn, "database")
	table := "addp_session_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	path := plugin.TabularItemPath(93003, plugin.EngineCatalogTermDatabase, database, table)
	dsn, err := p.serverDSN(conn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const catalogSQL = "SELECT count() FROM system.tables WHERE database=? AND name=?"
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
	qualified := clickhouseDialect().QualifiedTable(database, table)
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+` (id Int64, document Nullable(String), payload Nullable(String), label Nullable(String), CONSTRAINT fixture_label_length CHECK length(label) <= 8) ENGINE=MergeTree ORDER BY tuple()`); err != nil {
		t.Fatal(err)
	}
	if err := p.PrepareTableWrite(ctx, conn, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatal(err)
	}
	session, err := p.OpenTableWriteSession(ctx, conn, path, plugin.TableWriteSessionOptions{Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	return session, db, clickhouseDialect().QualifiedTable(database, table)
}

func testClickHouseDefinitionBeforeDDL(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	p := &ClickHousePlugin{}
	conn := clickhouseIntegrationConnInfo(t)
	dsn, err := p.serverDSN(conn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, field := range []datatype.FieldInfo{
		{Name: "payload", Type: datatype.FieldTypeArray},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3},
		{Name: "shape", Type: datatype.FieldTypeGeometry},
	} {
		t.Run(string(field.Type), func(t *testing.T) {
			database := "addp_rejected_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cleanupCancel()
				if _, err := db.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS "+clickhouseDialect().QuoteIdentifier(database)); err != nil {
					t.Errorf("cleanup rejected database: %v", err)
				}
				var count uint64
				if err := db.QueryRowContext(cleanupCtx, "SELECT count() FROM system.databases WHERE name=?", database).Scan(&count); err != nil || count != 0 {
					t.Errorf("rejected database cleanup not proven: count=%d error=%v", count, err)
				}
			})
			path := plugin.TabularItemPath(93003, plugin.EngineCatalogTermDatabase, database, "target")
			fields := []datatype.FieldInfo{field}
			if err := p.PrepareTableWrite(ctx, conn, path, plugin.TableWriteOptions{Fields: fields}); err == nil || !strings.Contains(err.Error(), "target_definition") {
				t.Fatalf("invalid definition reached DDL: %v", err)
			}
			// Exercise the private DDL path too, before its first CREATE DATABASE.
			if err := createClickHouseTableIfNotExists(ctx, db, database, "target", fields); err == nil {
				t.Fatal("private DDL accepted invalid definition")
			}
			var count uint64
			if err := db.QueryRowContext(ctx, "SELECT count() FROM system.databases WHERE name=?", database).Scan(&count); err != nil || count != 0 {
				t.Fatalf("invalid definition created database: count=%d error=%v", count, err)
			}
		})
	}
}

func testClickHouseDefinitionPreservesTargetAndProjection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	p := &ClickHousePlugin{}
	conn := clickhouseIntegrationConnInfo(t)
	database := plugin.GetString(conn, "database")
	table := "addp_definition_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	path := plugin.TabularItemPath(93003, plugin.EngineCatalogTermDatabase, database, table)
	dsn, err := p.serverDSN(conn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := p.DeleteResource(cleanupCtx, conn, path); err != nil {
			t.Errorf("cleanup definition table: %v", err)
		}
		var count uint64
		if err := db.QueryRowContext(cleanupCtx, "SELECT count() FROM system.tables WHERE database=? AND name=?", database, table).Scan(&count); err != nil || count != 0 {
			t.Errorf("definition cleanup not proven: count=%d error=%v", count, err)
		}
	}()
	// ClickHouse requires no Doris-style key, and identifiers are case-sensitive.
	fields := []datatype.FieldInfo{{Name: "document", Type: datatype.FieldTypeJSON, Nullable: true}, {Name: "Document", Type: datatype.FieldTypeString, Nullable: true}, {Name: "payload", Type: datatype.FieldTypeBytes, Nullable: true}}
	if err := p.PrepareTableWrite(ctx, conn, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatal(err)
	}
	if err := p.WriteBatch(ctx, conn, path, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"document": map[string]interface{}{"sentinel": true}, "Document": "preserve"}}}, plugin.BatchWriteOptions{}); err != nil {
		t.Fatal(err)
	}
	invalid := []datatype.FieldInfo{{Name: "document", Type: datatype.FieldTypeArray}}
	if err := p.PrepareTableWrite(ctx, conn, path, plugin.TableWriteOptions{Fields: invalid}); err == nil {
		t.Fatal("native array accepted")
	}
	if err := p.WriteBatch(ctx, conn, path, &plugin.BatchData{Fields: invalid, Rows: []map[string]interface{}{{"document": []int{1}}}}, plugin.BatchWriteOptions{}); err == nil {
		t.Fatal("native array batch accepted")
	}
	var stored, label string
	qualified := clickhouseDialect().QualifiedTable(database, table)
	if err := db.QueryRowContext(ctx, "SELECT document, Document FROM "+qualified).Scan(&stored, &label); err != nil || stored != `{"sentinel":true}` || label != "preserve" {
		t.Fatalf("rejected definition changed target: json=%q label=%q error=%v", stored, label, err)
	}
	snapshot := &datatype.TableInfo{Fields: fields}
	for i := range snapshot.Fields {
		snapshot.Fields[i].NativeType = "Nullable(String)"
	}
	definition := plugin.TableWriteDefinitionFromOptions(plugin.TableWriteOptions{Fields: fields})
	definition.Existing = snapshot
	report := p.CheckTableWriteDefinition(definition)
	for _, check := range report.Checks {
		if check.Status != plugin.DefinitionSupported {
			t.Fatalf("own physical structure rejected: %+v", report)
		}
	}
	projection := []datatype.FieldInfo{{Name: "document", Type: datatype.FieldTypeJSON, Nullable: true}, {Name: "computed", Type: datatype.FieldTypeArray, Generated: true}}
	if err := p.WriteBatch(ctx, conn, path, &plugin.BatchData{Fields: projection, Rows: []map[string]interface{}{{"document": []interface{}{true, nil}, "computed": []int{1}}}}, plugin.BatchWriteOptions{}); err != nil {
		t.Fatalf("JSON-only/generated projection: %v", err)
	}
	var count uint64
	if err := db.QueryRowContext(ctx, "SELECT count() FROM "+qualified+" WHERE document='[true,null]' AND isNull(Document)").Scan(&count); err != nil || count != 1 {
		t.Fatalf("projection not preserved: count=%d error=%v", count, err)
	}
	// Exec formats non-JSON slices as native arrays; implicit casts to String
	// are not certified as binary encoding or misreported as SQL NULL.
	nullRow := map[string]interface{}{"document": json.RawMessage(nil), "Document": []byte(nil), "payload": json.RawMessage(nil)}
	nullDefinition := plugin.TableWriteDefinition{}
	for _, field := range fields {
		nullDefinition.Fields = append(nullDefinition.Fields, plugin.TableWriteFieldDefinition{Target: field, Constant: &plugin.TableWriteConstant{Value: nullRow[field.Name]}})
	}
	constantChecks := 0
	for _, check := range p.CheckTableWriteDefinition(nullDefinition).Checks {
		if check.Check != "constant_encoding" {
			continue
		}
		constantChecks++
		want := plugin.DefinitionUnknown
		if check.Field == "document" {
			want = plugin.DefinitionSupported
		}
		if check.Status != want {
			t.Fatalf("native constant precheck diverged: %+v", check)
		}
	}
	if constantChecks != len(fields) {
		t.Fatal("missing native constant checks")
	}
	if err := p.WriteBatch(ctx, conn, path, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{nullRow}}, plugin.BatchWriteOptions{}); err != nil {
		t.Fatal(err)
	}
	var text, binary sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT Document, payload FROM "+qualified+" WHERE isNull(document)").Scan(&text, &binary); err != nil || !text.Valid || text.String != "[]" || !binary.Valid || binary.String != "[]" {
		t.Fatalf("native Exec cast semantics changed: text=%+v bytes=%+v error=%v", text, binary, err)
	}

}
