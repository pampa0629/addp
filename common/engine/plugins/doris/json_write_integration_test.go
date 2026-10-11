package doris

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestIntegrationDorisTypedJSONWrites(t *testing.T) {
	if os.Getenv("ADDP_DORIS_INTEGRATION") != "1" {
		t.Skip("set ADDP_DORIS_INTEGRATION=1 to run Doris integration test")
	}
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
			for _, invalid := range []interface{}{"{", []byte{}, json.RawMessage("{} []"), math.NaN(), make(chan int)} {
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
