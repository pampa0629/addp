// Package writetest owns the cross-provider JSON write acceptance matrix.
// It is imported only by provider integration tests, never production code.
package writetest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/wkb"
)

type MySQLJSONProvider interface {
	plugin.TableWritePreparer
	plugin.TableWriteSessionProvider
	plugin.TableUpsertProvider
}

// MySQLJSONWrites exercises real database values and transaction rollback using
// the same matrix on MySQL, OceanBase and TiDB. The caller owns the test database.
func MySQLJSONWrites(t *testing.T, db *sql.DB, p MySQLJSONProvider, connInfo plugin.ConnectionInfo, database string, spatial *datatype.SpatialInfo, geometry []byte) {
	t.Helper()
	t.Run("session_failure_close", func(t *testing.T) {
		mysqlSessionFailureClose(t, db, p, connInfo, database, spatial, geometry)
	})
	ctx := context.Background()
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
		{Name: "label", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "payload", Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "blob", Type: datatype.FieldTypeBytes, Nullable: true},
	}
	var standardWKB []byte
	if spatial != nil {
		fields = append(fields, datatype.FieldInfo{Name: "shape", Type: datatype.FieldTypeGeometry, Nullable: true})
		g, err := ewkb.Unmarshal(geometry)
		if err != nil {
			t.Fatal(err)
		}
		standardWKB, err = wkb.Marshal(g, wkb.NDR)
		if err != nil {
			t.Fatal(err)
		}
	}
	type jsonCase struct {
		value    interface{}
		document string
	}
	cases := []jsonCase{
		{[]interface{}{map[string]interface{}{"id": "activity", "nested": []interface{}{1, true, nil}}, "户外"}, `[{"id":"activity","nested":[1,true,null]},"户外"]`},
		{map[string]interface{}{"count": json.Number("9007199254740993")}, `{"count":9007199254740993}`},
		{[]interface{}{}, `[]`}, {map[string]interface{}{}, `{}`},
		{nil, ""}, {[]interface{}(nil), `null`}, {map[string]interface{}(nil), `null`},
		{true, `true`}, {int64(9007199254740993), `9007199254740993`}, {1.25, `1.25`},
		{`"户外\n活动"`, `"户外\n活动"`}, {`""`, `""`},
		{` {"large":9007199254740993} `, `{"large":9007199254740993}`},
		{[]byte(`[1,"two"]`), `[1,"two"]`}, {json.RawMessage(`{"raw":true}`), `{"raw":true}`},
		{"null", `null`}, {[]byte(nil), ""}, {json.RawMessage(nil), ""},
	}
	// A native array must fail before preparation can create a target table.
	t.Run("invalid_definition_before_ddl", func(t *testing.T) {
		table := fmt.Sprintf("invalid_array_%d", time.Now().UnixNano())
		path := plugin.TabularItemPath(0, plugin.EngineCatalogTermDatabase, database, table)
		qualified := "`" + strings.ReplaceAll(database, "`", "``") + "`.`" + table + "`"
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if _, err := db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+qualified); err != nil {
				t.Errorf("cleanup invalid definition table: %v", err)
			}
			var count int
			if err := db.QueryRowContext(cleanupCtx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?", database, table).Scan(&count); err != nil || count != 0 {
				t.Errorf("invalid definition cleanup not proven: count=%d error=%v", count, err)
			}
		})
		invalidFields := []datatype.FieldInfo{{Name: "payload", Type: datatype.FieldTypeArray, Nullable: true}}
		if spatial != nil {
			invalidFields = append(invalidFields, datatype.FieldInfo{Name: "shape", Type: datatype.FieldTypeGeometry, Nullable: true})
		}
		err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: invalidFields, SpatialInfo: spatial})
		if err == nil || !strings.Contains(err.Error(), `column "payload": field_definition_unsupported`) {
			t.Fatalf("native array definition reached DDL: %v", err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?", database, table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("invalid definition created target: count=%d error=%v", count, err)
		}
	})
	methods := []string{"session", "upsert"}
	if _, ok := p.(plugin.BatchWritableProvider); ok {
		methods = append(methods, "batch")
	}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			cases := append([]jsonCase(nil), cases...)
			table := fmt.Sprintf("json_write_%d_%s", time.Now().UnixNano(), method)
			path := plugin.TabularItemPath(0, plugin.EngineCatalogTermDatabase, database, table)
			quote := func(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
			qualified := quote(database) + "." + quote(table)
			t.Cleanup(func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if _, err := db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+qualified); err != nil {
					t.Errorf("cleanup JSON table: %v", err)
				}
				var count int
				if err := db.QueryRowContext(cleanupCtx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?", database, table).Scan(&count); err != nil || count != 0 {
					t.Errorf("JSON table cleanup not proven: count=%d error=%v", count, err)
				}
			})
			opts := plugin.TableUpsertOptions{Fields: fields, SpatialInfo: spatial, Keys: []string{"id"}}
			if err := p.PrepareTableUpsert(ctx, connInfo, path, opts); err != nil {
				t.Fatal(err)
			}
			var columnType string
			if err := db.QueryRowContext(ctx, "SELECT data_type FROM information_schema.columns WHERE table_schema=? AND table_name=? AND column_name='payload'", database, table).Scan(&columnType); err != nil || !strings.EqualFold(columnType, "json") {
				t.Fatalf("payload type=%q error=%v", columnType, err)
			}
			write := func(rows []map[string]interface{}) error {
				batch := &plugin.BatchData{Fields: fields, Rows: rows, Spatial: spatial}
				switch method {
				case "session":
					sessionFields := append([]datatype.FieldInfo(nil), fields...)
					session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: sessionFields, SpatialInfo: spatial, Method: "insert"})
					if err != nil {
						return err
					}
					defer session.Abort(ctx)
					// The opened session owns a type snapshot; neither input slice mutation
					// nor conflicting per-batch metadata may change the JSON encoding.
					sessionFields[2].Type = datatype.FieldTypeString
					batch.Fields = sessionFields
					for _, row := range rows {
						batch.Rows = []map[string]interface{}{row}
						if err := session.WriteBatch(ctx, batch); err != nil {
							return err
						}
					}
					return session.Close(ctx)
				case "upsert":
					batch.Fields = append([]datatype.FieldInfo(nil), fields...)
					batch.Fields[2].Type = datatype.FieldTypeString
					return p.UpsertBatch(ctx, connInfo, path, batch, opts)
				default:
					return p.(plugin.BatchWritableProvider).WriteBatch(ctx, connInfo, path, batch, plugin.BatchWriteOptions{Method: "insert"})
				}
			}
			rows := make([]map[string]interface{}, len(cases))
			for i, tc := range cases {
				rows[i] = map[string]interface{}{"id": i + 1, "label": `{"plain":"text"}`, "payload": tc.value, "blob": []byte{0, 1, 255}}
				if spatial != nil {
					rows[i]["shape"] = geometry
				}
			}
			rows[1]["blob"] = []byte{}
			rows[2]["blob"] = nil
			if spatial != nil {
				rows[2]["shape"] = nil
			}
			if err := write(rows); err != nil {
				t.Fatal(err)
			}
			if method == "upsert" {
				rows[0]["payload"] = map[string]interface{}{"updated": []interface{}{true, nil}}
				cases[0].document = `{"updated":[true,null]}`
				if err := write(rows[:1]); err != nil {
					t.Fatal(err)
				}
			}
			for i, tc := range cases {
				var label string
				var document sql.NullString
				var blob []byte
				var blobNull bool
				query := "SELECT " + quote("label") + "," + quote("payload") + "," + quote("blob") + "," + quote("blob") + " IS NULL"
				dest := []interface{}{&label, &document, &blob, &blobNull}
				var gotWKB []byte
				var srid sql.NullInt64
				if spatial != nil {
					query += ",ST_AsWKB(shape),ST_SRID(shape)"
					dest = append(dest, &gotWKB, &srid)
				}
				err := db.QueryRowContext(ctx, query+" FROM "+qualified+" WHERE id=?", i+1).Scan(dest...)
				if err != nil {
					t.Fatal(err)
				}
				if document.Valid != (tc.document != "") {
					t.Fatalf("row %d SQL NULL mismatch", i+1)
				}
				decode := func(text string) interface{} {
					d := json.NewDecoder(strings.NewReader(text))
					d.UseNumber()
					var v interface{}
					if err := d.Decode(&v); err != nil {
						t.Fatalf("invalid stored JSON: %v", err)
					}
					return v
				}
				if document.Valid && !reflect.DeepEqual(decode(document.String), decode(tc.document)) {
					t.Fatalf("row %d JSON value changed or double encoded", i+1)
				}
				wantBlob, _ := rows[i]["blob"].([]byte)
				if label != rows[i]["label"] || !bytes.Equal(blob, wantBlob) || blobNull != (i == 2) {
					t.Fatalf("row %d ordinary text or binary value changed", i+1)
				}
				if spatial != nil && (i == 2 && (gotWKB != nil || srid.Valid) || i != 2 && (!bytes.Equal(gotWKB, standardWKB) || !srid.Valid || srid.Int64 != 4326)) {
					t.Fatalf("row %d geometry changed", i+1)
				}
			}
			for _, bad := range []interface{}{"{", []byte("bad"), json.RawMessage(`[] []`), math.NaN(), make(chan int)} {
				// Cross the INSERT chunk boundary so a successful first chunk must roll
				// back when encoding a later chunk fails (upsert and batch paths).
				badRows := make([]map[string]interface{}, 1001)
				for i := range badRows {
					badRows[i] = map[string]interface{}{"id": 10000 + i, "payload": []interface{}{1}}
					if spatial != nil {
						badRows[i]["shape"] = nil
					}
				}
				badRows[1000]["payload"] = bad
				err := write(badRows)
				if err == nil || !strings.Contains(err.Error(), `column "payload"`) {
					t.Fatalf("bad value %T: want field error, got %v", bad, err)
				}
				assertMySQLWriteDiagnostic(t, err, true, 3)
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified+" WHERE id>=10000").Scan(&count); err != nil || count != 0 {
					t.Fatalf("failed write not rolled back: count=%d error=%v", count, err)
				}
			}
			if method == "upsert" {
				err := write([]map[string]interface{}{{"id": "native_integer_invalid", "payload": true, "blob": []byte{0, 255}}})
				assertMySQLWriteDiagnostic(t, err, false, 0)
				var native *mysqlDriver.MySQLError
				if !errors.As(err, &native) {
					t.Fatalf("upsert lost native MySQL error: %v", err)
				}
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != len(cases) {
					t.Fatalf("native upsert failure changed rows: count=%d error=%v", count, err)
				}
			}

		})
	}
}

// The Provider must roll back failed sessions even when its direct caller uses
// Close instead of the explicit Abort used by Transfer and the batch adapter.
func mysqlSessionFailureClose(t *testing.T, db *sql.DB, p MySQLJSONProvider, info plugin.ConnectionInfo, database string, spatial *datatype.SpatialInfo, geometry []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	table := fmt.Sprintf("session_failure_%d", time.Now().UnixNano())
	qualified := "`" + strings.ReplaceAll(database, "`", "``") + "`.`" + table + "`"
	path := plugin.TabularItemPath(0, plugin.EngineCatalogTermDatabase, database, table)
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?", database, table).Scan(&count); err != nil || count != 0 {
		t.Fatalf("occupied fixture: count=%d err=%v", count, err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanup, "DROP TABLE IF EXISTS "+qualified); err != nil {
			t.Error(err)
		}
		if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?", database, table).Scan(&count); err != nil || count != 0 {
			t.Errorf("fixture cleanup not proven: count=%d err=%v", count, err)
		}
	})
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
		{Name: "payload", Type: datatype.FieldTypeJSON},
		{Name: "blob", Type: datatype.FieldTypeBytes},
	}
	if spatial != nil {
		fields = append(fields, datatype.FieldInfo{Name: "shape", Type: datatype.FieldTypeGeometry})
	}
	if err := p.PrepareTableWrite(ctx, info, path, plugin.TableWriteOptions{Fields: fields, SpatialInfo: spatial}); err != nil {
		t.Fatal(err)
	}
	row := func(id int64) map[string]interface{} {
		value := map[string]interface{}{"id": id, "payload": map[string]interface{}{"sentinel": true}, "blob": []byte{0, 1, 255}}
		if spatial != nil {
			value["shape"] = geometry
		}
		return value
	}
	open := func(t *testing.T) plugin.TableWriteSession {
		t.Helper()
		session, err := p.OpenTableWriteSession(ctx, info, path, plugin.TableWriteSessionOptions{Fields: fields, SpatialInfo: spatial})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Abort(context.Background()) })
		return session
	}
	commit := func(t *testing.T, id int64) {
		t.Helper()
		session := open(t)
		if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{row(id)}}); err != nil {
			t.Fatal(err)
		}
		if err := session.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if err := session.Close(ctx); err != nil || session.(plugin.CommitMarkerProvider).CommitMarker() == nil {
			t.Fatalf("successful session Close/marker: %v", err)
		}
	}
	commit(t, 1)
	for index, failure := range []string{"invalid_json", "duplicate_key", "not_null", "canceled_write", "canceled_close"} {
		t.Run(failure, func(t *testing.T) {
			session := open(t)
			base := int64(10000 * (index + 1))
			if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{row(base)}}); err != nil {
				t.Fatal(err)
			}
			// A later chunk fails after a whole prior batch and 1000 more rows.
			rows := make([]map[string]interface{}, 1001)
			for i := range rows {
				rows[i] = row(base + int64(i) + 1)
			}
			switch failure {
			case "invalid_json":
				rows[1000]["payload"] = "{"
			case "duplicate_key":
				rows[1000]["id"] = int64(1)
			case "not_null":
				rows[1000]["payload"] = nil
			}
			writeCtx, closeCtx := ctx, ctx
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if failure == "canceled_write" {
				writeCtx = canceled
			}
			var firstErr error
			if failure == "canceled_close" {
				closeCtx, firstErr = canceled, context.Canceled
			} else {
				firstErr = session.WriteBatch(writeCtx, &plugin.BatchData{Rows: rows})
				if firstErr == nil {
					t.Fatal("expected write failure")
				}
				if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{row(base + 5000)}}); !errors.Is(err, firstErr) {
					t.Errorf("write after failure lost original error: got=%v first=%v", err, firstErr)
				}
			}
			closeErr := session.Close(closeCtx)
			if !errors.Is(closeErr, firstErr) {
				t.Errorf("Close lost failure: got=%v first=%v", closeErr, firstErr)
			}
			assertMySQLWriteDiagnostic(t, closeErr, failure == "invalid_json", 2)
			if failure == "duplicate_key" || failure == "not_null" {
				var native *mysqlDriver.MySQLError
				if !errors.As(closeErr, &native) {
					t.Fatalf("lost native MySQL error chain: %v", closeErr)
				}
			}
			if err := session.Close(ctx); !errors.Is(err, firstErr) {
				t.Errorf("repeated Close lost failure: %v", err)
			}
			if session.(plugin.CommitMarkerProvider).CommitMarker() != nil {
				t.Error("failed session published success marker")
			}
			if err := session.Abort(ctx); err != nil {
				t.Error(err)
			}
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != 1 {
				t.Errorf("failed session changed committed rows: count=%d want=1 err=%v", count, err)
			}
		})
	}
	commit(t, 2)
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != 2 {
		t.Fatalf("recovery/sentinel rows=%d want=2 err=%v", count, err)
	}
	for _, id := range []int64{1, 2} {
		var document string
		var blob []byte
		if err := db.QueryRowContext(ctx, "SELECT `payload`,`blob` FROM "+qualified+" WHERE id=?", id).Scan(&document, &blob); err != nil {
			t.Fatal(err)
		}
		var value map[string]interface{}
		if err := json.Unmarshal([]byte(document), &value); err != nil || !reflect.DeepEqual(value, map[string]interface{}{"sentinel": true}) || !bytes.Equal(blob, []byte{0, 1, 255}) {
			t.Fatalf("JSON/BLOB changed: document=%s blob=%v err=%v", document, blob, err)
		}
		if spatial != nil {
			var encoded []byte
			var srid int
			if err := db.QueryRowContext(ctx, "SELECT ST_AsWKB(shape),ST_SRID(shape) FROM "+qualified+" WHERE id=?", id).Scan(&encoded, &srid); err != nil {
				t.Fatal(err)
			}
			g, err := ewkb.Unmarshal(geometry)
			if err != nil {
				t.Fatal(err)
			}
			want, err := wkb.Marshal(g, wkb.NDR)
			if err != nil || !bytes.Equal(encoded, want) || srid != 4326 {
				t.Fatalf("geometry changed: srid=%d err=%v", srid, err)
			}
		}
	}
}

// SQL protocol errors carry no guessed field, even if the driver mentions one.
func assertMySQLWriteDiagnostic(t *testing.T, err error, jsonFailure bool, column int) {
	t.Helper()
	var typed *plugin.TableWriteError
	if !errors.As(err, &typed) {
		t.Fatalf("lost typed write failure: %v", err)
	}
	if jsonFailure {
		if typed.Code != plugin.TableWriteJSONInvalid || typed.Field == nil || typed.Field.ColumnIndex != column || typed.Field.DeclaredType != datatype.FieldTypeJSON {
			t.Fatalf("JSON column diagnostic: %#v", typed)
		}
	} else if typed.Code != plugin.TableWriteFailed || typed.Field != nil {
		t.Fatalf("unproven field attached: %#v", typed)
	}
}
