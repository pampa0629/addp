// Package writetest owns the cross-provider JSON write acceptance matrix.
// It is imported only by provider integration tests, never production code.
package writetest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
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
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified+" WHERE id>=10000").Scan(&count); err != nil || count != 0 {
					t.Fatalf("failed write not rolled back: count=%d error=%v", count, err)
				}
			}
		})
	}
}
