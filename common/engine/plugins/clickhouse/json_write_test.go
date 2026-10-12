package clickhouse

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/mattn/go-sqlite3"
)

// SQLite observes the actual session's parameter binding without contacting an
// engine. Native ClickHouse types are covered by the Hosted integration gate.
func openClickHouseJSONUnitSession(t *testing.T, fields []datatype.FieldInfo, chunkSize int) *clickhouseTableWriteSession {
	t.Helper()
	p := &ClickHousePlugin{}
	opened, err := p.OpenTableWriteSession(t.Context(), plugin.ConnectionInfo{
		"host": "127.0.0.1", "port": 1, "user": "synthetic",
	}, plugin.TabularItemPath(1, plugin.EngineCatalogTermDatabase, "main", "typed_json"), plugin.TableWriteSessionOptions{Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	session := opened.(*clickhouseTableWriteSession)
	if err := session.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE typed_json (document TEXT, raw BLOB, label TEXT)"); err != nil {
		t.Fatal(err)
	}
	session.db, session.chunkSize = db, chunkSize
	return session
}

func TestClickHouseJSONSessionUsesOpeningTypes(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "document", Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "raw", Type: datatype.FieldTypeBytes, Nullable: true},
		{Name: "label", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "generated", Type: datatype.FieldTypeJSON, Generated: true},
	}
	session := openClickHouseJSONUnitSession(t, fields, 1)
	fields[0].Type = datatype.FieldTypeString
	rows := []map[string]interface{}{
		{"document": []interface{}{map[string]interface{}{"ok": true}, nil}, "raw": []byte{0, 255}, "label": "plain {", "generated": make(chan int)},
		{"document": json.RawMessage(` {"n":9007199254740993} `), "raw": []byte{}, "label": "plain {"},
		{"document": []byte(nil), "raw": nil, "label": "plain {"},
	}
	if err := session.WriteBatch(t.Context(), &plugin.BatchData{
		Fields: []datatype.FieldInfo{{Name: "document", Type: datatype.FieldTypeString}, {Name: "label", Type: datatype.FieldTypeJSON}}, Rows: rows,
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := session.db.Query("SELECT document, raw, raw IS NULL, label FROM typed_json ORDER BY rowid")
	if err != nil {
		t.Fatal(err)
	}
	defer stored.Close()
	wants := []string{`[{"ok":true},null]`, ` {"n":9007199254740993} `, ""}
	index := 0
	for stored.Next() {
		var document sql.NullString
		var raw []byte
		var rawNull bool
		var label string
		if err := stored.Scan(&document, &raw, &rawNull, &label); err != nil {
			t.Fatal(err)
		}
		if index >= len(wants) || document.Valid != (wants[index] != "") || document.String != wants[index] {
			t.Fatalf("row %d document=%#v", index, document)
		}
		wantRaw, _ := rows[index]["raw"].([]byte)
		if !bytes.Equal(raw, wantRaw) || rawNull != (wantRaw == nil) || label != "plain {" {
			t.Fatalf("row %d changed non-JSON values: bytes=%v null=%v label=%q", index, raw, rawNull, label)
		}
		index++
	}
	if err := stored.Err(); err != nil || index != len(wants) {
		t.Fatalf("read %d/%d rows: %v", index, len(wants), err)
	}
	if session.rowsWritten != 3 || session.batchesWritten != 1 {
		t.Fatalf("successful batch counters: rows=%d batches=%d", session.rowsWritten, session.batchesWritten)
	}
}

func TestClickHouseJSONSessionRejectsInvalidBatchBeforeFirstChunk(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value interface{}
	}{
		{"malformed", "{"}, {"empty bytes", []byte{}}, {"multiple documents", json.RawMessage("{} []")},
		{"nonfinite", math.NaN()}, {"container", make(chan int)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := openClickHouseJSONUnitSession(t, []datatype.FieldInfo{{Name: "document", Type: datatype.FieldTypeJSON}}, 1)
			err := session.WriteBatch(t.Context(), &plugin.BatchData{Rows: []map[string]interface{}{{"document": "{}"}, {"document": tc.value}}})
			if err == nil || !strings.Contains(err.Error(), `row 1 field "document"`) {
				t.Errorf("expected row/field encoding error, got %v", err)
			}
			var count int
			if err := session.db.QueryRow("SELECT COUNT(*) FROM typed_json").Scan(&count); err != nil || count != 0 {
				t.Fatalf("invalid batch wrote rows: count=%d error=%v", count, err)
			}
			if session.rowsWritten != 0 || session.batchesWritten != 0 {
				t.Fatal("invalid batch advanced commit counters")
			}
		})
	}
}

func TestClickHouseJSONSessionCancelledBatchDoesNotWrite(t *testing.T) {
	session := openClickHouseJSONUnitSession(t, []datatype.FieldInfo{{Name: "document", Type: datatype.FieldTypeJSON}}, 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{{"document": map[string]interface{}{"ok": true}}}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled batch error=%v", err)
	}
	var count int
	if err := session.db.QueryRow("SELECT COUNT(*) FROM typed_json").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cancelled batch wrote rows: count=%d error=%v", count, err)
	}
}

func TestClickHouseJSONSchemaValidationUsesWriteStorageMapping(t *testing.T) {
	for _, typ := range []datatype.FieldType{datatype.FieldTypeJSON, datatype.FieldTypeBytes, datatype.FieldTypeTime} {
		field := datatype.FieldInfo{Name: "value", Type: typ, Nullable: true}
		if !clickhouseColumnCompatibleWithField(clickhouseWriteColumnInfo{NativeType: "Nullable(String)"}, field) {
			t.Errorf("%s rejected its own String storage mapping", typ)
		}
	}
	for _, native := range []string{"Int64", "JSON", "FixedString(128)", "Nullable(FixedString(128))"} {
		if clickhouseColumnCompatibleWithField(clickhouseWriteColumnInfo{NativeType: native}, datatype.FieldInfo{Type: datatype.FieldTypeJSON}) {
			t.Errorf("JSON accepted incompatible storage %s", native)
		}
	}
	if !clickhouseColumnCompatibleWithField(clickhouseWriteColumnInfo{NativeType: "Array(Int64)"}, datatype.FieldInfo{Type: datatype.FieldTypeArray}) {
		t.Fatal("JSON storage validation changed the native array path")
	}
}

func TestClickHouseBytesBindAsBinaryStringWithoutReinterpretingArrays(t *testing.T) {
	types := map[string]datatype.FieldType{"raw": datatype.FieldTypeBytes, "array": datatype.FieldTypeArray, "geometry": datatype.FieldTypeGeometry}
	args, err := clickhouseTableWriteArgs(t.Context(), []string{"raw", "array", "geometry"}, types, []map[string]interface{}{
		{"raw": []byte{0, 255, 39, 92}, "array": []int{1, 2}, "geometry": []byte{1, 2}},
		{"raw": []byte{}}, {"raw": []byte(nil)}, {"raw": nil},
	})
	want := []interface{}{string([]byte{0, 255, 39, 92}), []int{1, 2}, []byte{1, 2}, "", nil, nil, nil, nil, nil, nil, nil, nil}
	if err != nil || !reflect.DeepEqual(args, want) {
		t.Fatalf("binary binding changed values: args=%#v error=%v", args, err)
	}
}
