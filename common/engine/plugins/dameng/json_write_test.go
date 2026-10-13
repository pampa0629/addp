package dameng

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/mattn/go-sqlite3"
)

// SQLite observes the INSERT session's bound values without loading or
// pretending to certify the official DM driver. Real CLOB/MERGE coverage is T2.
func jsonUnitSession(t *testing.T, nullable bool) *tableWriteSession {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE target (ID INTEGER, DOC TEXT, PAYLOAD BLOB, LABEL TEXT)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO target VALUES (?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	s := &tableWriteSession{db: db, tx: tx, stmt: stmt, fields: []datatype.FieldInfo{
		{Name: "ID", Type: datatype.FieldTypeBigInt},
		{Name: "DOC", Type: datatype.FieldTypeJSON, Nullable: nullable},
		{Name: "PAYLOAD", Type: datatype.FieldTypeBytes, Nullable: true},
		{Name: "LABEL", Type: datatype.FieldTypeString, Nullable: true},
	}}
	t.Cleanup(func() { _ = s.Abort(context.Background()) })
	return s
}

func jsonUnitRow(id int64, value interface{}) map[string]interface{} {
	return map[string]interface{}{"ID": id, "DOC": value, "PAYLOAD": []byte{0, 1, 255}, "LABEL": "plain {"}
}

func TestDM8SessionTypedJSONBindings(t *testing.T) {
	s := jsonUnitSession(t, true)
	cases := []struct {
		value interface{}
		want  string
	}{
		{[]interface{}{map[string]interface{}{"ok": true}, nil}, `[{"ok":true},null]`},
		{map[string]interface{}{"中文": []interface{}{1}}, `{"中文":[1]}`},
		{[]interface{}{}, `[]`}, {map[string]interface{}{}, `{}`},
		{[]interface{}(nil), `null`}, {map[string]interface{}(nil), `null`},
		{nil, ""}, {[]byte(nil), ""}, {json.RawMessage(nil), ""},
		{true, `true`}, {int64(9007199254740993), `9007199254740993`}, {1.25, `1.25`},
		{json.Number("12345678901234567890.123456789"), `12345678901234567890.123456789`},
		{` {"n":9007199254740993} `, ` {"n":9007199254740993} `},
		{[]byte(`[1,null]`), `[1,null]`}, {json.RawMessage(`{"raw":true}`), `{"raw":true}`},
		{`"中文\n字符串"`, `"中文\n字符串"`}, {`""`, `""`}, {"null", "null"},
	}
	rows := make([]map[string]interface{}, len(cases))
	for i, tc := range cases {
		rows[i] = jsonUnitRow(int64(i+1), tc.value)
	}
	// Per-batch metadata must not override the session's declared types.
	if err := s.WriteBatch(t.Context(), &plugin.BatchData{Fields: []datatype.FieldInfo{{Name: "DOC", Type: datatype.FieldTypeString}}, Rows: rows}); err != nil {
		t.Fatal(err)
	}
	for i, tc := range cases {
		var document sql.NullString
		var payload []byte
		var label string
		if err := s.tx.QueryRow(`SELECT DOC, PAYLOAD, LABEL FROM target WHERE ID=?`, i+1).Scan(&document, &payload, &label); err != nil {
			t.Fatal(err)
		}
		if document.Valid != (tc.want != "") || document.String != tc.want || !bytes.Equal(payload, []byte{0, 1, 255}) || label != "plain {" {
			t.Fatalf("row %d: JSON NULL, text or bytes changed", i)
		}
	}
}

func TestDM8SessionRejectsWholeInvalidBatch(t *testing.T) {
	for _, bad := range []interface{}{"{", []byte{}, json.RawMessage(`[] []`), math.NaN(), make(chan int)} {
		t.Run(strings.ReplaceAll(strings.TrimSpace(fmt.Sprintf("%T", bad)), " ", "_"), func(t *testing.T) {
			s := jsonUnitSession(t, true)
			err := s.WriteBatch(t.Context(), &plugin.BatchData{Rows: []map[string]interface{}{jsonUnitRow(1, true), jsonUnitRow(2, bad)}})
			if err == nil || !strings.Contains(err.Error(), `row 1 field "DOC"`) {
				t.Fatalf("want row/field JSON error, got %v", err)
			}
			var count int
			if err := s.tx.QueryRow(`SELECT COUNT(*) FROM target`).Scan(&count); err != nil || count != 0 || s.rowsWritten != 0 || s.batchesWritten != 0 {
				t.Fatalf("invalid batch wrote %d rows, error=%v", count, err)
			}
		})
	}
}

func TestDM8SessionEncodedNullAndRequiredFields(t *testing.T) {
	for _, value := range []interface{}{nil, []byte(nil), json.RawMessage(nil)} {
		s := jsonUnitSession(t, false)
		if err := s.WriteBatch(t.Context(), &plugin.BatchData{Rows: []map[string]interface{}{jsonUnitRow(1, value)}}); err == nil || !strings.Contains(err.Error(), `field "DOC" is NOT NULL`) {
			t.Fatalf("encoded SQL NULL: %v", err)
		}
	}
	s := jsonUnitSession(t, false)
	if err := s.WriteBatch(t.Context(), &plugin.BatchData{Rows: []map[string]interface{}{jsonUnitRow(1, []interface{}(nil))}}); err != nil {
		t.Fatal(err)
	}
	missing := jsonUnitRow(3, true)
	delete(missing, "DOC")
	err := s.WriteBatch(t.Context(), &plugin.BatchData{Rows: []map[string]interface{}{jsonUnitRow(2, true), missing}})
	if err == nil || !strings.Contains(err.Error(), `row 1 is missing field "DOC"`) {
		t.Fatal(err)
	}
	var count int
	if err := s.tx.QueryRow(`SELECT COUNT(*) FROM target`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("missing field batch wrote rows: count=%d error=%v", count, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{jsonUnitRow(4, true)}}); err != context.Canceled {
		t.Fatalf("canceled batch: %v", err)
	}
}

func TestDM8ArrayAndSpatialWritesRemainUnsupported(t *testing.T) {
	for _, typ := range []datatype.FieldType{datatype.FieldTypeArray, datatype.FieldTypeGeometry} {
		if err := validateWriteFields([]datatype.FieldInfo{{Name: "VALUE", Type: typ}}, nil); err == nil {
			t.Fatalf("unexpected support for %s", typ)
		}
	}
}

func TestDM8UpsertRejectsJSONBeforeOpeningDriver(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "ID", Type: datatype.FieldTypeBigInt, PrimaryKey: true}, {Name: "DOC", Type: datatype.FieldTypeJSON}}
	path := plugin.TabularItemPath(98001, plugin.EngineCatalogTermSchema, "SYNTHETIC", "TARGET")
	for _, value := range []interface{}{"{", []byte(nil), json.RawMessage(nil), make(chan int)} {
		batch := &plugin.BatchData{Fields: []datatype.FieldInfo{{Name: "ID", Type: datatype.FieldTypeBigInt}, {Name: "DOC", Type: datatype.FieldTypeString}}, Rows: []map[string]interface{}{{"ID": int64(1), "DOC": true}, {"ID": int64(2), "DOC": value}}}
		err := (&Plugin{}).UpsertBatch(t.Context(), nil, path, batch, plugin.TableUpsertOptions{Fields: fields, Keys: []string{"ID"}})
		if err == nil || !strings.Contains(err.Error(), `row 1 field "DOC"`) {
			t.Fatalf("invalid JSON or encoded SQL NULL must fail before connection: %v", err)
		}
	}
}
