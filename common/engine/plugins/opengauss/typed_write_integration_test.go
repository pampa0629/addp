package opengauss

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestIntegrationOpenGaussTypedWrites(t *testing.T) {
	if os.Getenv("ADDP_OPENGAUSS_INTEGRATION") != "1" {
		t.Skip("set ADDP_OPENGAUSS_INTEGRATION=1 to run openGauss integration test")
	}
	for _, method := range []string{"copy_session", "protocol_copy_batch", "protocol_insert", "upsert"} {
		t.Run(method, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
			defer cancel()
			p := &Plugin{}
			connInfo := openGaussIntegrationConnInfo()
			dsn, err := p.BuildDSN(connInfo)
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("postgres", dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			schema := "typed_write_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
			qualified := pq.QuoteIdentifier(schema) + `."target"`
			defer func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cleanupCancel()
				if _, err := db.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+pq.QuoteIdentifier(schema)+" CASCADE"); err != nil {
					t.Errorf("cleanup typed schema: %v", err)
				}
				var exists bool
				if err := db.QueryRowContext(cleanupCtx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&exists); err != nil || exists {
					t.Errorf("typed schema cleanup not proven: exists=%v error=%v", exists, err)
				}
			}()
			path := plugin.TabularItemPath(96001, plugin.EngineCatalogTermSchema, schema, "target")
			fields := []datatype.FieldInfo{
				{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
				{Name: "document", Type: datatype.FieldTypeJSON, Nullable: true},
				{Name: "members", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeString, Nullable: true},
				{Name: "payload", Type: datatype.FieldTypeBytes, Nullable: true},
				{Name: "label", Type: datatype.FieldTypeString, Nullable: true},
			}
			opts := plugin.TableUpsertOptions{Fields: fields, Keys: []string{"id"}}
			if err := p.PrepareTableUpsert(ctx, connInfo, path, opts); err != nil {
				t.Fatal(err)
			}
			var physical string
			if err := db.QueryRowContext(ctx, "SELECT data_type FROM information_schema.columns WHERE table_schema=$1 AND table_name='target' AND column_name='document'", schema).Scan(&physical); err != nil || physical != "jsonb" {
				t.Fatalf("JSON column=%s error=%v", physical, err)
			}
			cases := []struct {
				value    interface{}
				document string
			}{
				{[]interface{}{map[string]interface{}{"ok": true}, "中文", nil}, `[{"ok":true},"中文",null]`},
				{map[string]interface{}{"n": json.Number("9007199254740993")}, `{"n":9007199254740993}`},
				{[]interface{}{}, `[]`}, {map[string]interface{}{}, `{}`},
				{[]interface{}(nil), `null`}, {map[string]interface{}(nil), `null`},
				{nil, ""}, {[]byte(nil), ""}, {json.RawMessage(nil), ""},
				{true, `true`}, {int64(9007199254740993), `9007199254740993`}, {1.25, `1.25`},
				{json.Number("12345678901234567890.123456789"), `12345678901234567890.123456789`},
				{` {"n":9007199254740993} `, `{"n":9007199254740993}`},
				{[]byte(`[1,null]`), `[1,null]`}, {json.RawMessage(`{"raw":true}`), `{"raw":true}`},
				{`"中文\n字符串"`, `"中文\n字符串"`}, {`""`, `""`}, {"null", "null"},
				{map[string]interface{}{"large": strings.Repeat("x", 65536)}, `{"large":"` + strings.Repeat("x", 65536) + `"}`},
			}
			arrays := []struct {
				value    interface{}
				document string
			}{
				{[]interface{}{"NULL", nil, "", "中文", `a"b\c`}, `["NULL",null,"","中文","a\"b\\c"]`},
				{[]string{}, `[]`}, {[]string(nil), ""},
				{`{"a",NULL,""}`, `["a",null,""]`}, {[]byte(`{"a",NULL,""}`), `["a",null,""]`},
			}
			binaries := [][]byte{{0, 1, 255}, {}, nil, {39, 92, 10, 255}}
			rows := make([]map[string]interface{}, len(cases))
			for i, tc := range cases {
				rows[i] = map[string]interface{}{"id": int64(i + 1), "document": tc.value, "members": arrays[i%len(arrays)].value, "payload": binaries[i%len(binaries)], "label": "plain {"}
			}
			write := func(rows []map[string]interface{}) error {
				batch := &plugin.BatchData{Fields: append([]datatype.FieldInfo(nil), fields...), Rows: rows}
				if method == "protocol_insert" || method == "protocol_copy_batch" {
					writeMethod := "insert"
					if method == "protocol_copy_batch" {
						writeMethod = "copy"
					}
					// Exercise the PostgreSQL protocol kernel against openGauss.
					// This does not add a public openGauss batch writer.
					// Batch writers take their declared types from batch fields.
					return p.protocol().WriteBatch(ctx, connInfo, path, batch, plugin.BatchWriteOptions{Method: writeMethod})
				}
				// Session and upsert types come from their frozen options.
				batch.Fields[1].Type = datatype.FieldTypeString
				batch.Fields[2].Type = datatype.FieldTypeJSON
				if method == "upsert" {
					return p.UpsertBatch(ctx, connInfo, path, batch, opts)
				}
				openingFields := append([]datatype.FieldInfo(nil), fields...)
				session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: openingFields, Method: "copy"})
				if err != nil {
					return err
				}
				defer session.Abort(ctx)
				openingFields[1].Type = datatype.FieldTypeString
				if err := session.WriteBatch(ctx, batch); err != nil {
					return err
				}
				return session.Close(ctx)
			}
			if err := write(rows); err != nil {
				t.Fatal(err)
			}
			if method == "upsert" {
				rows[0]["document"] = map[string]interface{}{"updated": []interface{}{true, nil}}
				cases[0].document = `{"updated":[true,null]}`
				for attempt := 0; attempt < 2; attempt++ {
					if err := write(rows[:1]); err != nil {
						t.Fatal(err)
					}
				}
			}
			decode := func(document string) interface{} {
				if document == "" {
					return nil
				}
				decoder := json.NewDecoder(strings.NewReader(document))
				decoder.UseNumber()
				var value interface{}
				if err := decoder.Decode(&value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			for i, tc := range cases {
				var document, members sql.NullString
				var binary []byte
				var binaryNull, membersNull, documentNull bool
				var label string
				if err := db.QueryRowContext(ctx, `SELECT document::text,document IS NULL,array_to_json(members)::text,members IS NULL,payload,payload IS NULL,label FROM `+qualified+` WHERE id=$1`, i+1).Scan(&document, &documentNull, &members, &membersNull, &binary, &binaryNull, &label); err != nil {
					t.Fatal(err)
				}
				array := arrays[i%len(arrays)]
				wantBinary := binaries[i%len(binaries)]
				if documentNull != (tc.document == "") || (!documentNull && (!document.Valid || !reflect.DeepEqual(decode(document.String), decode(tc.document)))) {
					t.Fatalf("row %d JSON changed: SQL NULL=%v want=%v actual=%q expected=%q", i, documentNull, tc.document == "", typedJSONPrefix(document.String), typedJSONPrefix(tc.document))
				}
				// Check the native column's NULL separately from array_to_json:
				// the conversion function's handling of a NULL input is not
				// evidence that the array column retained SQL NULL.
				if membersNull && members.Valid {
					t.Logf("row %d array is SQL NULL; array_to_json returned %q", i, members.String)
				}
				if membersNull != (array.document == "") || (!membersNull && (!members.Valid || !reflect.DeepEqual(decode(members.String), decode(array.document)))) {
					t.Fatalf("row %d array changed: SQL NULL=%v want=%v actual=%q expected=%q", i, membersNull, array.document == "", members.String, array.document)
				}
				if !bytes.Equal(binary, wantBinary) || binaryNull != (wantBinary == nil) {
					t.Fatalf("row %d bytes changed: SQL NULL=%v want=%v actual=%x expected=%x", i, binaryNull, wantBinary == nil, binary, wantBinary)
				}
				if label != "plain {" {
					t.Fatalf("row %d text changed: %q", i, label)
				}
			}
			for _, bad := range []struct {
				field string
				value interface{}
			}{
				{"document", "{"}, {"document", []byte{}}, {"document", json.RawMessage(`[] []`)}, {"document", math.NaN()}, {"document", make(chan int)},
				{"document", string([]byte{'"', 0xff, '"'})}, {"document", []byte{'"', 0xff, '"'}}, {"document", json.RawMessage([]byte{'"', 0xff, '"'})},
				{"members", []interface{}{map[string]interface{}{"a": 1}}}, {"members", []int{1}}, {"members", []interface{}{[]string{"a"}}}, {"members", "{bad"},
			} {
				badRows := []map[string]interface{}{{"id": int64(1000), "document": true, "members": []string{}, "payload": nil, "label": nil}, {"id": int64(1001), "document": true, "members": []string{}, "payload": nil, "label": nil}}
				badRows[1][bad.field] = bad.value
				err := write(badRows)
				if err == nil || !strings.Contains(err.Error(), bad.field) {
					t.Fatalf("invalid %s accepted: %v", bad.field, err)
				}
				code, column, fieldType := plugin.TableWriteJSONInvalid, 2, datatype.FieldTypeJSON
				if bad.field == "members" {
					code, column, fieldType = plugin.TableWriteArrayInvalid, 3, datatype.FieldTypeArray
				}
				var typed *plugin.TableWriteError
				if !errors.As(err, &typed) || typed.Code != code || typed.Field == nil || typed.Field.ColumnIndex != column || typed.Field.DeclaredType != fieldType {
					t.Fatalf("native diagnostic: %#v error=%v", typed, err)
				}
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != len(cases) {
					t.Fatalf("invalid batch changed rows: count=%d error=%v", count, err)
				}
			}
			if method == "upsert" {
				err := write([]map[string]interface{}{{"id": "native_integer_invalid", "document": true, "members": []string{}, "payload": nil, "label": nil}})
				var typed *plugin.TableWriteError
				var native *pq.Error
				if !errors.As(err, &typed) || typed.Code != plugin.TableWriteFailed || typed.Field != nil || !errors.As(err, &native) {
					t.Fatalf("native MERGE error chain: %#v error=%v", typed, err)
				}
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != len(cases) {
					t.Fatalf("native MERGE failure changed rows: count=%d error=%v", count, err)
				}
			}

		})
	}
}

func typedJSONPrefix(document string) string {
	if len(document) > 128 {
		return document[:128] + "..."
	}
	return document
}
