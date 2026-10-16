//go:build dameng_official

package dameng

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestIntegrationDM8TypedJSONWrites(t *testing.T) {
	if os.Getenv("ADDP_DAMENG_INTEGRATION") != "1" {
		t.Skip("set ADDP_DAMENG_INTEGRATION=1 to run DM8 integration test")
	}
	t.Run("definition_before_ddl", testDM8DefinitionBeforeDDL)
	t.Run("session_failure_close", testDM8SessionFailureClose)
	for _, method := range []string{"session", "upsert"} {
		t.Run(method, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
			defer cancel()
			p := &Plugin{}
			connInfo := integrationConnInfo()
			db, err := p.openDB(connInfo)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			table := "ADDP_JSON_" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
			path := plugin.TabularItemPath(98001, plugin.EngineCatalogTermSchema, "ADDP_BUSINESS", table)
			qualified := `"ADDP_BUSINESS"."` + table + `"`
			defer func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cleanupCancel()
				if err := p.DeleteResource(cleanupCtx, connInfo, path); err != nil {
					t.Errorf("cleanup JSON table: %v", err)
				}
				var remaining int
				if err := db.QueryRowContext(cleanupCtx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_TYPE='TABLE' AND OBJECT_NAME=?", table).Scan(&remaining); err != nil || remaining != 0 {
					t.Errorf("JSON fixture cleanup not proven: tables=%d error=%v", remaining, err)
				}
			}()
			fields := []datatype.FieldInfo{
				{Name: "ID", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
				{Name: "DOC", Type: datatype.FieldTypeJSON, Nullable: true},
				{Name: "PAYLOAD", Type: datatype.FieldTypeBytes, Nullable: true},
				{Name: "LABEL", Type: datatype.FieldTypeString, Nullable: true},
			}
			opts := plugin.TableUpsertOptions{Fields: fields, Keys: []string{"ID"}}
			if err := p.PrepareTableUpsert(ctx, connInfo, path, opts); err != nil {
				t.Fatal(err)
			}
			// Repeated preparation must consume actual CLOB/BLOB catalog facts.
			if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
				t.Fatal(err)
			}
			var physical string
			if err := db.QueryRowContext(ctx, "SELECT DATA_TYPE FROM USER_TAB_COLUMNS WHERE TABLE_NAME=? AND COLUMN_NAME='DOC'", table).Scan(&physical); err != nil || physical != "CLOB" {
				t.Fatalf("JSON storage=%q want CLOB error=%v", physical, err)
			}
			cases := []struct {
				value interface{}
				want  string
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
				{map[string]interface{}{"large": strings.Repeat("x", 65536)}, `{"large":"` + strings.Repeat("x", 65536) + `"}`},
			}
			binaries := [][]byte{{0, 1, 255}, {}, nil, {39, 92, 10, 255}}
			rows := make([]map[string]interface{}, len(cases))
			for i, tc := range cases {
				rows[i] = map[string]interface{}{"ID": int64(i + 1), "DOC": tc.value, "PAYLOAD": binaries[i%len(binaries)], "LABEL": "plain {"}
			}
			write := func(rows []map[string]interface{}) error {
				batch := &plugin.BatchData{Fields: append([]datatype.FieldInfo(nil), fields...), Rows: rows}
				batch.Fields[1].Type = datatype.FieldTypeString
				if method == "upsert" {
					return p.UpsertBatch(ctx, connInfo, path, batch, opts)
				}
				openingFields := append([]datatype.FieldInfo(nil), fields...)
				session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: openingFields})
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
				rows[0]["DOC"] = map[string]interface{}{"updated": []interface{}{true, nil}}
				cases[0].want = `{"updated":[true,null]}`
				for attempt := 0; attempt < 2; attempt++ {
					if err := write(rows[:1]); err != nil {
						t.Fatal(err)
					}
				}
			}
			for i, tc := range cases {
				var document sql.NullString
				var payload []byte
				var payloadNull int
				var label string
				if err := db.QueryRowContext(ctx, `SELECT "DOC", "PAYLOAD", CASE WHEN "PAYLOAD" IS NULL THEN 1 ELSE 0 END, "LABEL" FROM `+qualified+` WHERE "ID"=?`, i+1).Scan(&document, &payload, &payloadNull, &label); err != nil {
					t.Fatal(err)
				}
				wantBinary := binaries[i%len(binaries)]
				if document.Valid != (tc.want != "") || document.String != tc.want || !bytes.Equal(payload, wantBinary) || (payloadNull == 1) != (wantBinary == nil) || label != "plain {" {
					t.Fatalf("row %d JSON, SQL NULL, text or binary changed", i)
				}
			}
			for _, bad := range []interface{}{"{", []byte{}, json.RawMessage(`[] []`), math.NaN(), make(chan int)} {
				badRows := []map[string]interface{}{jsonUnitRow(1000, true), jsonUnitRow(1001, bad)}
				err := write(badRows)
				if err == nil || !strings.Contains(err.Error(), `row 1 field "DOC"`) {
					t.Fatalf("bad value %T: want field error, got %v", bad, err)
				}
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != len(cases) {
					t.Fatalf("failed write changed table: count=%d error=%v", count, err)
				}
			}
			// Replace DELETE and earlier valid batches must remain abortable even
			// when a later JSON batch is invalid; this does not include Prepare DDL.
			if method == "session" {
				session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: fields, Replace: true})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Abort(ctx)
				if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{jsonUnitRow(2000, true)}}); err != nil {
					t.Fatal(err)
				}
				if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{jsonUnitRow(2001, "{")}}); err == nil {
					t.Fatal("invalid JSON accepted")
				}
				if err := session.Abort(ctx); err != nil {
					t.Fatal(err)
				}
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != len(cases) {
					t.Fatalf("replace abort did not restore table: count=%d error=%v", count, err)
				}
			}
		})
	}
}

func testDM8SessionFailureClose(t *testing.T) {
	for _, replace := range []bool{false, true} {
		for _, failure := range dm8SessionFailureCases {
			t.Run(fmt.Sprintf("replace=%t/%s", replace, failure), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
				defer cancel()
				p := &Plugin{}
				info := integrationConnInfo()
				db, err := p.openDB(info)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				schema := plugin.GetString(info, "user")
				table := "ADDP_FAIL_" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
				path := plugin.TabularItemPath(98001, plugin.EngineCatalogTermSchema, schema, table)
				qualified := `"` + strings.ReplaceAll(schema, `"`, `""`) + `"."` + table + `"`
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=?", table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("occupied fixture: count=%d err=%v", count, err)
				}
				defer func() {
					cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					if err := p.DeleteResource(cleanup, info, path); err != nil {
						t.Error(err)
					}
					if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_NAME=?", table).Scan(&count); err != nil || count != 0 {
						t.Errorf("fixture cleanup not proven: count=%d err=%v", count, err)
					}
				}()
				fields := dm8FailureFields()
				if err := p.PrepareTableWrite(ctx, info, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
					t.Fatal(err)
				}
				open := func(replace bool) plugin.TableWriteSession {
					session, err := p.OpenTableWriteSession(ctx, info, path, plugin.TableWriteSessionOptions{Fields: fields, Replace: replace})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = session.Abort(context.Background()) })
					return session
				}
				commit := func(id int64) {
					session := open(false)
					if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{jsonUnitRow(id, map[string]interface{}{"sentinel": true})}}); err != nil {
						t.Fatal(err)
					}
					if err := session.Close(ctx); err != nil {
						t.Fatal(err)
					}
					if err := session.Close(ctx); err != nil || session.(plugin.CommitMarkerProvider).CommitMarker() == nil {
						t.Fatalf("successful session Close/marker: %v", err)
					}
				}
				commit(1)
				assertDM8SessionFailureClose(t, ctx, open(replace), failure)
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != 1 {
					t.Fatalf("failed session changed committed rows: count=%d want=1 err=%v", count, err)
				}
				commit(2)
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != 2 {
					t.Fatalf("new session recovery: count=%d want=2 err=%v", count, err)
				}
				for _, id := range []int64{1, 2} {
					var document, label string
					var payload []byte
					if err := db.QueryRowContext(ctx, `SELECT "DOC","PAYLOAD","LABEL" FROM `+qualified+` WHERE "ID"=?`, id).Scan(&document, &payload, &label); err != nil || document != `{"sentinel":true}` || !bytes.Equal(payload, []byte{0, 1, 255}) || label != "plain {" {
						t.Fatalf("JSON/BLOB/sentinel changed: doc=%q payload=%v label=%q err=%v", document, payload, label, err)
					}
				}
			})
		}
	}
}

func testDM8DefinitionBeforeDDL(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	p := &Plugin{}
	connInfo := integrationConnInfo()
	db, err := p.openDB(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	table := "ADDP_JSON_" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
	path := plugin.TabularItemPath(98001, plugin.EngineCatalogTermSchema, "ADDP_BUSINESS", table)
	qualified := `"ADDP_BUSINESS"."` + table + `"`
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := p.DeleteResource(cleanup, connInfo, path); err != nil {
			t.Errorf("cleanup: %v", err)
		}
		var count int
		if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_TYPE='TABLE' AND OBJECT_NAME=?", table).Scan(&count); err != nil || count != 0 {
			t.Errorf("fixture cleanup not proven: count=%d error=%v", count, err)
		}
	}()
	for _, typ := range []datatype.FieldType{datatype.FieldTypeJSON, datatype.FieldTypeBytes} {
		field := datatype.FieldInfo{Name: "DOC", Type: typ}
		physical, err := dmSQLType(field)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+` ("DOC" `+physical+` PRIMARY KEY)`); err == nil {
			t.Fatal("official DM8 accepted a LOB primary key")
		}
	}
	for _, field := range []datatype.FieldInfo{{Name: "DOC", Type: datatype.FieldTypeArray}, {Name: "DOC", Type: datatype.FieldTypeGeometry}, {Name: "DOC", Type: datatype.FieldTypeJSON, PrimaryKey: true}, {Name: "DOC", Type: datatype.FieldTypeBytes, PrimaryKey: true}} {
		if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: []datatype.FieldInfo{field}}); err == nil || !strings.Contains(err.Error(), "target_definition") {
			t.Fatalf("definition not rejected: %v", err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_TYPE='TABLE' AND OBJECT_NAME=?", table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("unsupported definition created table: %d %v", count, err)
		}
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE `+qualified+` ("ID" BIGINT PRIMARY KEY, "DOC" VARCHAR(255), "PAYLOAD" BLOB, "AMOUNT" DECIMAL(18,2))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO `+qualified+` ("ID","DOC") VALUES (1,'sentinel')`); err != nil {
		t.Fatal(err)
	}
	fields := []datatype.FieldInfo{{Name: "NEW_DOC", Type: datatype.FieldTypeJSON, Nullable: true}, {Name: "DOC", Type: datatype.FieldTypeJSON, Nullable: true}}
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err == nil || !strings.Contains(err.Error(), "existing_column_incompatible") {
		t.Fatalf("incompatible JSON storage accepted: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_TAB_COLUMNS WHERE TABLE_NAME=? AND COLUMN_NAME='NEW_DOC'", table).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial ALTER occurred: %d %v", count, err)
	}
	fields[1].Type = datatype.FieldTypeString
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatal(err)
	}
	actual, err := p.listColumns(ctx, connInfo, "ADDP_BUSINESS", table)
	if err != nil {
		t.Fatal(err)
	}
	definition := plugin.TableWriteDefinitionFromOptions(plugin.TableWriteOptions{Fields: []datatype.FieldInfo{{Name: "NEW_DOC", Type: datatype.FieldTypeJSON, Nullable: true}, {Name: "PAYLOAD", Type: datatype.FieldTypeBytes, Nullable: true}, {Name: "AMOUNT", Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2, Nullable: true}}})
	definition.Existing = &datatype.TableInfo{Fields: actual}
	for _, check := range p.CheckTableWriteDefinition(definition).Checks {
		if check.Status != plugin.DefinitionSupported {
			t.Fatalf("actual facts not accepted: %+v facts=%+v", check, actual)
		}
	}
	definition.Fields[2].Target.Scale = 3
	if p.CheckTableWriteDefinition(definition).UnsupportedError() == nil {
		t.Fatal("decimal mismatch accepted")
	}
	// A typed nil BLOB must fail before the first row SQL; abort restores DELETE.
	session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: []datatype.FieldInfo{{Name: "ID", Type: datatype.FieldTypeBigInt}, {Name: "PAYLOAD", Type: datatype.FieldTypeBytes}}, Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Abort(context.Background())
	if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{{"ID": int64(2), "PAYLOAD": []byte{}}, {"ID": int64(3), "PAYLOAD": []byte(nil)}}}); err == nil || !strings.Contains(err.Error(), `row 1 field "PAYLOAD" is NOT NULL`) {
		t.Fatalf("required binary NULL accepted: %v", err)
	}
	if err := session.(*tableWriteSession).tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid batch executed an earlier INSERT: %d %v", count, err)
	}
	if err := session.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	var sentinel string
	if err := db.QueryRowContext(ctx, `SELECT "DOC" FROM `+qualified+` WHERE "ID"=1`).Scan(&sentinel); err != nil || sentinel != "sentinel" {
		t.Fatalf("sentinel changed: %q %v", sentinel, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed batch leaked rows: %d %v", count, err)
	}
}
