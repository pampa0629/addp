package opengauss

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestBuildOpenGaussMergeSQLUsesNativeMerge(t *testing.T) {
	statement, err := buildOpenGaussMergeSQL("target_schema", "target_table", []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "name", Type: datatype.FieldTypeString},
		{Name: "amount", Type: datatype.FieldTypeDecimal},
	}, []string{"id"})
	if err != nil {
		t.Fatal(err)
	}
	want := `MERGE INTO "target_schema"."target_table" target USING (SELECT CAST($1 AS BIGINT) AS "id", CAST($2 AS TEXT) AS "name", CAST($3 AS NUMERIC) AS "amount") source ON (target."id" = source."id") WHEN MATCHED THEN UPDATE SET "name" = source."name", "amount" = source."amount" WHEN NOT MATCHED THEN INSERT ("id", "name", "amount") VALUES (source."id", source."name", source."amount")`
	if statement != want {
		t.Fatalf("buildOpenGaussMergeSQL() = %q, want %q", statement, want)
	}
	if strings.Contains(statement, "ON CONFLICT") {
		t.Fatalf("openGauss MERGE must not contain PostgreSQL ON CONFLICT: %s", statement)
	}
}

func TestBuildOpenGaussMergeSQLDoesNotUpdateCompositeKeys(t *testing.T) {
	statement, err := buildOpenGaussMergeSQL("public", "items", []datatype.FieldInfo{
		{Name: "tenant_id", Type: datatype.FieldTypeBigInt},
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "value", Type: datatype.FieldTypeString},
	}, []string{"tenant_id", "id"})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`target."tenant_id" = source."tenant_id" AND target."id" = source."id"`,
		`UPDATE SET "value" = source."value"`,
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("MERGE = %q, want fragment %q", statement, fragment)
		}
	}
	if strings.Contains(statement, `UPDATE SET "tenant_id"`) || strings.Contains(statement, `"id" = source."id",`) {
		t.Fatalf("MERGE updates a configured key: %s", statement)
	}
}

func TestBuildOpenGaussMergeSQLSupportsKeyOnlyTable(t *testing.T) {
	statement, err := buildOpenGaussMergeSQL("public", "keys_only", []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
	}, []string{"id"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(statement, "WHEN MATCHED") {
		t.Fatalf("key-only MERGE must not update matching rows: %s", statement)
	}
	if !strings.Contains(statement, `WHEN NOT MATCHED THEN INSERT ("id") VALUES (source."id")`) {
		t.Fatalf("key-only MERGE does not insert unmatched rows: %s", statement)
	}
}

func TestBuildOpenGaussMergeSQLRejectsMissingKeyColumn(t *testing.T) {
	_, err := buildOpenGaussMergeSQL("public", "items", []datatype.FieldInfo{
		{Name: "name", Type: datatype.FieldTypeString},
	}, []string{"id"})
	if err == nil || !strings.Contains(err.Error(), `missing key field "id"`) {
		t.Fatalf("missing key error = %v", err)
	}
}

func TestOpenGaussUpsertRowArgsRejectsMissingField(t *testing.T) {
	_, err := openGaussUpsertRowArgs(map[string]interface{}{"id": int64(1)}, []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "name", Type: datatype.FieldTypeString}})
	if err == nil || !strings.Contains(err.Error(), `missing field "name"`) {
		t.Fatalf("missing field error = %v", err)
	}
}

func TestOpenGaussUpsertRejectsInvalidJSONBeforeConnecting(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true}, {Name: "document", Type: datatype.FieldTypeJSON}}
	path := plugin.TabularItemPath(96001, plugin.EngineCatalogTermSchema, "synthetic", "target")
	for _, bad := range []interface{}{"{", []byte{}, json.RawMessage(`[] []`), math.NaN(), make(chan int), []byte(nil), json.RawMessage(nil)} {
		batch := &plugin.BatchData{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "document", Type: datatype.FieldTypeString}}, Rows: []map[string]interface{}{{"id": int64(1), "document": true}, {"id": int64(2), "document": bad}}}
		err := (&Plugin{}).UpsertBatch(t.Context(), nil, path, batch, plugin.TableUpsertOptions{Fields: fields, Keys: []string{"id"}})
		if err == nil || !strings.Contains(err.Error(), `row 1`) || !strings.Contains(err.Error(), `field "document"`) {
			t.Fatalf("JSON must fail before opening a connection: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := (&Plugin{}).UpsertBatch(ctx, nil, path, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"id": int64(1), "document": true}}}, plugin.TableUpsertOptions{Fields: fields, Keys: []string{"id"}})
	if err != context.Canceled {
		t.Fatalf("canceled upsert: %v", err)
	}
}

func TestOpenGaussUpsertUsesDeclaredProtocolTypes(t *testing.T) {
	columns := []string{"document", "members", "payload", "label"}
	fields, err := openGaussUpsertFields(columns, []datatype.FieldInfo{
		{Name: "document", Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "members", Type: datatype.FieldTypeArray, Nullable: true},
		{Name: "payload", Type: datatype.FieldTypeBytes, Nullable: true},
		{Name: "label", Type: datatype.FieldTypeString, Nullable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		value interface{}
		want  interface{}
	}{
		{[]interface{}{map[string]interface{}{"ok": true}, nil}, `[{"ok":true},null]`},
		{map[string]interface{}{}, `{}`}, {[]interface{}{}, `[]`},
		{[]interface{}(nil), `null`}, {map[string]interface{}(nil), `null`},
		{nil, nil}, {[]byte(nil), nil}, {json.RawMessage(nil), nil},
		{true, `true`}, {int64(9007199254740993), `9007199254740993`},
		{json.Number("12345678901234567890.123456789"), `12345678901234567890.123456789`},
		{` {"n":9007199254740993} `, ` {"n":9007199254740993} `},
		{[]byte(`[1,null]`), `[1,null]`}, {json.RawMessage(`{"raw":true}`), `{"raw":true}`},
		{`"中文"`, `"中文"`}, {`""`, `""`}, {`null`, `null`},
	} {
		binary := []byte{0, 1, 255}
		args, err := openGaussUpsertRowArgs(map[string]interface{}{"document": tc.value, "members": []interface{}{"NULL", nil, ""}, "payload": binary, "label": "plain {"}, fields)
		want := []interface{}{tc.want, `{"NULL",NULL,""}`, binary, "plain {"}
		if err != nil || !reflect.DeepEqual(args, want) {
			t.Fatalf("value %T: got %#v, %v; want %#v", tc.value, args, err, want)
		}
	}
	for _, value := range []interface{}{[]string(nil), []string{}, `{"a",NULL}`, []byte(`{"a",NULL}`)} {
		_, err := openGaussUpsertRowArgs(map[string]interface{}{"document": true, "members": value, "payload": nil, "label": nil}, fields)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []interface{}{[]int{1}, []interface{}{map[string]interface{}{"a": 1}}, []interface{}{[]string{"a"}}, "{bad", []string{"\x00"}} {
		_, err := openGaussUpsertRowArgs(map[string]interface{}{"document": true, "members": bad, "payload": nil, "label": nil}, fields)
		if err == nil || !strings.Contains(err.Error(), `field "members"`) {
			t.Fatalf("array %T accepted: %v", bad, err)
		}
	}
	if _, err := openGaussUpsertFields([]string{"extra"}, fields); err == nil {
		t.Fatal("batch field absent from options was accepted")
	}
}
