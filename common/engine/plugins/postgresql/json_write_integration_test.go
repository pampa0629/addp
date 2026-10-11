package postgresql

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/lib/pq"
)

func TestIntegrationPostgresTypedJSONWrites(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, true)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	schema := fmt.Sprintf("json_write_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pq.QuoteIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("cleanup JSON write schema: %v", err)
		}
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, schema).Scan(&exists); err != nil || exists {
			t.Errorf("JSON fixture cleanup not proven: exists=%v error=%v", exists, err)
		}
	})
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
		{Name: "label", Type: datatype.FieldTypeString},
		{Name: "caredOutdoors", Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "blob", Type: datatype.FieldTypeBytes, Nullable: true},
		{Name: "location", Type: datatype.FieldTypeGeometry, Nullable: true},
	}
	// The third field reproduces the original []interface{} COPY failure.
	cases := []struct {
		value interface{}
		json  string // empty means SQL NULL
	}{
		{[]interface{}{map[string]interface{}{"id": "activity", "nested": []interface{}{1, true, nil}}, "户外"}, `[{"id":"activity","nested":[1,true,null]},"户外"]`},
		{map[string]interface{}{"count": json.Number("9007199254740993")}, `{"count":9007199254740993}`},
		{[]interface{}{}, `[]`}, {map[string]interface{}{}, `{}`},
		{nil, ""}, {[]interface{}(nil), `null`}, {map[string]interface{}(nil), `null`},
		{true, `true`}, {int64(9007199254740993), `9007199254740993`}, {1.25, `1.25`},
		{`"户外\n活动"`, `"户外\n活动"`}, {`""`, `""`},
		{` {"large":12345678901234567890.123456789} `, `{"large":12345678901234567890.123456789}`},
		{[]byte(`[1,"two"]`), `[1,"two"]`}, {json.RawMessage(`{"raw":true}`), `{"raw":true}`},
		{"null", "null"}, {[]byte(nil), ""}, {json.RawMessage(nil), ""},
	}
	var ewkb []byte
	if err := db.QueryRowContext(ctx, `SELECT ST_AsEWKB(ST_SetSRID(ST_MakePoint(121.5,31.2),4326))`).Scan(&ewkb); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"session", "copy", "insert", "upsert"} {
		t.Run(method, func(t *testing.T) {
			path := postgresPrepareTablePath(schema, method)
			if err := pg.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
				t.Fatal(err)
			}
			assertPostgresPrepareColumn(t, ctx, db, schema, method, "caredOutdoors", "jsonb", "YES", "")
			table := pq.QuoteIdentifier(schema) + "." + pq.QuoteIdentifier(method)
			write := func(rows []map[string]interface{}) error {
				batch := &plugin.BatchData{Fields: fields, Rows: rows, Hints: map[string]interface{}{"chunk_size": 2}}
				if method == "session" {
					session, err := pg.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: fields, Method: "copy"})
					if err != nil {
						return err
					}
					defer session.Abort(ctx)
					// Session options own the types, independent of per-batch metadata.
					batch.Fields = nil
					for start := 0; start < len(rows); start += 2 {
						end := min(start+2, len(rows))
						batch.Rows = rows[start:end]
						if err := session.WriteBatch(ctx, batch); err != nil {
							return err
						}
					}
					return session.Close(ctx)
				}
				if method == "upsert" {
					batch.Fields = nil
					return pg.UpsertBatch(ctx, connInfo, path, batch, plugin.TableUpsertOptions{Fields: fields, Keys: []string{"id"}})
				}
				return pg.WriteBatch(ctx, connInfo, path, batch, plugin.BatchWriteOptions{Method: method})
			}
			rows := make([]map[string]interface{}, len(cases))
			for i, tc := range cases {
				rows[i] = map[string]interface{}{"id": i + 1, "label": `{"plain":"text"}`, "caredOutdoors": tc.value, "blob": []byte{0, 1, 0xff}, "location": ewkb}
			}
			// Verify empty BYTEA and SQL NULL independently of JSON byte encoding.
			rows[1]["blob"] = []byte{}
			rows[2]["blob"] = nil
			rows[2]["location"] = []byte{}
			if err := write(rows); err != nil {
				t.Fatal(err)
			}
			for i, tc := range cases {
				var want interface{}
				if tc.json != "" {
					want = tc.json
				}
				var jsonOK, bytesOK, geometryOK bool
				err := db.QueryRowContext(ctx, `SELECT "caredOutdoors" IS NOT DISTINCT FROM $1::jsonb,
					blob IS NOT DISTINCT FROM $2::bytea,
					ST_AsEWKB(location) IS NOT DISTINCT FROM $3::bytea
					FROM `+table+` WHERE id=$4 AND label=$5`, want, rows[i]["blob"], func() interface{} {
					if i == 2 {
						return nil
					}
					return ewkb
				}(), i+1, rows[i]["label"]).Scan(&jsonOK, &bytesOK, &geometryOK)
				if err != nil || !jsonOK || !bytesOK || !geometryOK {
					t.Fatalf("row %d: json=%v bytes=%v geometry=%v error=%v", i+1, jsonOK, bytesOK, geometryOK, err)
				}
			}
			for _, bad := range []interface{}{"{", []byte(`bad`), json.RawMessage(`[] []`), math.NaN(), make(chan int)} {
				// A valid row before the invalid value must roll back with its transaction.
				err := write([]map[string]interface{}{
					{"id": 1000, "label": "valid", "caredOutdoors": []interface{}{1}},
					{"id": 1001, "label": "bad", "caredOutdoors": bad},
				})
				if err == nil || !strings.Contains(err.Error(), `column "caredOutdoors"`) {
					t.Fatalf("invalid %T: %v", bad, err)
				}
				var count int
				if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != len(cases) {
					t.Fatalf("failed write changed rows: count=%d error=%v", count, err)
				}
			}
		})
	}
}
