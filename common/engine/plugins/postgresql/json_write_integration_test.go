package postgresql

import (
	"context"
	"encoding/json"
	"errors"
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
	t.Run("session_close_outcomes", testPostgresCopyCloseOutcomes)
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
			rows[2]["blob"] = []byte(nil)
			rows[3]["blob"] = nil
			rows[2]["location"] = []byte{}
			if err := write(rows); err != nil {
				t.Fatal(err)
			}
			for i, tc := range cases {
				var want interface{}
				if tc.json != "" {
					want = tc.json
				}
				var wantBytes interface{} = rows[i]["blob"]
				if value, ok := wantBytes.([]byte); ok && value == nil {
					wantBytes = nil
				}
				var jsonOK, bytesOK, geometryOK bool
				err := db.QueryRowContext(ctx, `SELECT "caredOutdoors" IS NOT DISTINCT FROM $1::jsonb,
					blob IS NOT DISTINCT FROM $2::bytea,
					ST_AsEWKB(location) IS NOT DISTINCT FROM $3::bytea
					FROM `+table+` WHERE id=$4 AND label=$5`, want, wantBytes, func() interface{} {
					if i == 2 {
						return nil
					}
					return ewkb
				}(), i+1, rows[i]["label"]).Scan(&jsonOK, &bytesOK, &geometryOK)
				if err != nil || !jsonOK || !bytesOK || !geometryOK {
					t.Fatalf("row %d: json=%v bytes=%v geometry=%v error=%v", i+1, jsonOK, bytesOK, geometryOK, err)
				}
			}
			for _, bad := range []interface{}{"{", []byte(`bad`), json.RawMessage(`[] []`), math.NaN(), make(chan int), string([]byte{'"', 0xff, '"'}), []byte{'"', 0xff, '"'}, json.RawMessage([]byte{'"', 0xff, '"'})} {
				// A valid row before the invalid value must roll back with its transaction.
				err := write([]map[string]interface{}{
					{"id": 1000, "label": "valid", "caredOutdoors": []interface{}{1}},
					{"id": 1001, "label": "bad", "caredOutdoors": bad},
				})
				if err == nil || !strings.Contains(err.Error(), `column "caredOutdoors"`) {
					t.Fatalf("invalid %T: %v", bad, err)
				}
				var typed *plugin.TableWriteError
				// Upsert has no batch.Fields here: its actual SQL column order
				// comes from sorted keys of these three input columns.
				columnIndex := 3
				if method == "upsert" {
					columnIndex = 1
				}
				if !errors.As(err, &typed) || typed.Code != plugin.TableWriteJSONInvalid || typed.Field == nil || typed.Field.ColumnIndex != columnIndex || typed.Field.DeclaredType != datatype.FieldTypeJSON {
					t.Fatalf("lost structured JSON field in %s: %#v error=%v", method, typed, err)
				}

				var count int
				if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != len(cases) {
					t.Fatalf("failed write changed rows: count=%d error=%v", count, err)
				}
			}
			// This is valid JSON syntax but JSONB cannot store U+0000. Preserve
			// the actual PostgreSQL error rather than guessing an encoding column.
			err := write([]map[string]interface{}{{"id": 2000, "label": "database rejection", "caredOutdoors": `"\u0000"`}})
			var typed *plugin.TableWriteError
			var native *pq.Error
			if !errors.As(err, &typed) || typed.Code != plugin.TableWriteFailed || typed.Field != nil || !errors.As(err, &native) || native.Code == "" {
				t.Fatalf("lost native SQLSTATE or invented a column in %s: %v", method, err)
			}
			var count int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != len(cases) {
				t.Fatalf("native rejection changed rows: count=%d error=%v", count, err)
			}
		})
	}
}

// Both statements and deferred constraints can fail only when closing COPY.
// A failed replacement must preserve the original rows and repeat its failure.
func testPostgresCopyCloseOutcomes(t *testing.T) {
	db, pg, info := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	schema := fmt.Sprintf("copy_close_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if _, err := db.ExecContext(cleanup, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" RESTRICT"); err != nil {
			t.Errorf("cleanup close schema: %v", err)
		}
		var exists bool
		if err := db.QueryRowContext(cleanup, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&exists); err != nil || exists {
			t.Errorf("close schema cleanup not proven: exists=%v error=%v", exists, err)
		}
	})
	for _, replace := range []bool{false, true} {
		for _, outcome := range []string{"finalize_not_null", "deferred_unique", "encoding_failure", "canceled_close", "success", "empty_success"} {
			t.Run(fmt.Sprintf("replace=%t/%s", replace, outcome), func(t *testing.T) {
				table := fmt.Sprintf("target_%t_%s", replace, outcome)
				qualified := pq.QuoteIdentifier(schema) + "." + pq.QuoteIdentifier(table)
				if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+" (id BIGINT NOT NULL, document JSONB NOT NULL, payload BYTEA, PRIMARY KEY (id) DEFERRABLE INITIALLY DEFERRED)"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
					defer done()
					if _, err := db.ExecContext(cleanup, "DROP TABLE "+qualified+" RESTRICT"); err != nil {
						t.Errorf("cleanup close table: %v", err)
					}
					var gone bool
					if err := db.QueryRowContext(cleanup, "SELECT to_regclass($1) IS NULL", qualified).Scan(&gone); err != nil || !gone {
						t.Errorf("close table cleanup not proven: gone=%v error=%v", gone, err)
					}
				})
				if _, err := db.ExecContext(ctx, "INSERT INTO "+qualified+" VALUES (1,'{\"sentinel\":true}',$1)", []byte{0, 255}); err != nil {
					t.Fatal(err)
				}
				fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "document", Type: datatype.FieldTypeJSON}, {Name: "payload", Type: datatype.FieldTypeBytes, Nullable: true}}
				opened, err := pg.OpenTableWriteSession(ctx, info, postgresPrepareTablePath(schema, table), plugin.TableWriteSessionOptions{Fields: fields, Replace: replace, Method: "copy"})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := opened.Abort(context.Background()); err != nil {
						t.Errorf("abort close fixture: %v", err)
					}
				})
				if outcome != "empty_success" {
					if err := opened.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{{"id": int64(2), "document": map[string]interface{}{"ok": true}, "payload": []byte{0, 1, 255}}}}); err != nil {
						t.Fatal(err)
					}
				}
				var writeErr error
				if outcome == "finalize_not_null" || outcome == "deferred_unique" || outcome == "encoding_failure" {
					row := map[string]interface{}{"id": int64(3), "document": true, "payload": []byte{}}
					if outcome == "finalize_not_null" {
						row["document"] = nil
					}
					if outcome == "deferred_unique" {
						row["id"] = int64(2)
					}
					if outcome == "encoding_failure" {
						row["document"] = make(chan int)
					}
					writeErr = opened.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{row}})
					if outcome != "encoding_failure" && writeErr != nil {
						t.Fatalf("failure must occur during close: %v", writeErr)
					}
				}
				closeCtx := ctx
				if outcome == "canceled_close" {
					var done context.CancelFunc
					closeCtx, done = context.WithCancel(ctx)
					done()
				}
				first := opened.Close(closeCtx)
				success := outcome == "success" || outcome == "empty_success"
				if success {
					if first != nil {
						t.Fatal(first)
					}
					if err := opened.Close(ctx); err != nil {
						t.Fatalf("repeated successful Close: %v", err)
					}
					marker := opened.(plugin.CommitMarkerProvider).CommitMarker()
					wantRows := int64(1)
					if outcome == "empty_success" {
						wantRows = 0
					}
					if marker == nil || marker.CommitPosition["rows_committed"] != wantRows {
						t.Fatalf("success marker: %#v", marker)
					}
				} else {
					var typed *plugin.TableWriteError
					if !errors.As(first, &typed) {
						t.Fatalf("missing close diagnostic: %v", first)
					}
					if outcome == "encoding_failure" {
						var native *json.UnsupportedTypeError
						if typed.Code != plugin.TableWriteJSONInvalid || typed.Field == nil || typed.Field.ColumnIndex != 2 || !errors.As(first, &native) || !errors.Is(first, writeErr) {
							t.Fatalf("encoding close evidence: %#v error=%v", typed, first)
						}
					} else {
						if typed.Code != plugin.TableWriteFailed || typed.Field != nil {
							t.Fatalf("close failure guessed column: %#v", typed)
						}
						if outcome == "canceled_close" {
							if !errors.Is(first, context.Canceled) {
								t.Fatalf("cancel cause lost: %v", first)
							}
						} else {
							var native *pq.Error
							code, stage := "23502", "finalize postgresql copy session"
							if outcome == "deferred_unique" {
								code, stage = "23505", "commit postgresql copy session"
							}
							if !errors.As(first, &native) || string(native.Code) != code || !strings.Contains(first.Error(), stage) {
								t.Fatalf("native close stage/error lost: %v", first)
							}
						}
					}
					for i := 0; i < 2; i++ {
						if err := opened.Close(ctx); !errors.Is(err, first) {
							t.Errorf("repeated Close lost failure: got=%v first=%v", err, first)
						}
					}
					if opened.(plugin.CommitMarkerProvider).CommitMarker() != nil {
						t.Error("failed close published success marker")
					}
				}
				wantIDs := "1"
				if success && replace {
					wantIDs = ""
				}
				if outcome == "success" {
					if replace {
						wantIDs = "2"
					} else {
						wantIDs = "1,2"
					}
				}
				var ids string
				if err := db.QueryRowContext(ctx, "SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM "+qualified).Scan(&ids); err != nil || ids != wantIDs {
					t.Fatalf("close changed rows: ids=%q want=%q error=%v", ids, wantIDs, err)
				}
				if !success || !replace {
					var intact bool
					if err := db.QueryRowContext(ctx, "SELECT document='{"+"\"sentinel\":true}'::jsonb AND payload=$1 FROM "+qualified+" WHERE id=1", []byte{0, 255}).Scan(&intact); err != nil || !intact {
						t.Fatalf("sentinel changed: intact=%v error=%v", intact, err)
					}
				}
			})
		}
	}
}
