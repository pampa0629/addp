package postgresql

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestPostgresArrayWriteValue(t *testing.T) {
	for _, tc := range []struct {
		value interface{}
		want  interface{}
	}{
		{[]interface{}{"a", nil, "NULL", ""}, `{"a",NULL,"NULL",""}`},
		{[]string{"a\"b", "a\\b", "户外"}, `{"a\"b","a\\b","户外"}`},
		{[1]string{"a"}, `{"a"}`}, {[]string{}, `{}`}, {[]interface{}{}, `{}`},
		{nil, nil}, {[]string(nil), nil}, {[]interface{}(nil), nil}, {[]byte(nil), nil},
		{`{a,NULL,"NULL",""}`, `{a,NULL,"NULL",""}`},
		{`{null,NuLl,"null", a b }`, `{null,NuLl,"null", a b }`},
	} {
		got, err := postgresWriteValue(tc.value, datatype.FieldTypeArray)
		if err != nil || got != tc.want {
			t.Fatalf("%#v: got %#v %v, want %#v", tc.value, got, err, tc.want)
		}
	}
	for _, bad := range []interface{}{[]interface{}{"a", 1}, []interface{}{"a", true}, []interface{}{"a", map[string]interface{}{}}, []interface{}{"a", []string{}}, []interface{}{"a", []byte{}}, []interface{}{"a", "\x00"}, []interface{}{"a", string([]byte{0xff})}} {
		_, err := postgresWriteRow(map[string]interface{}{"members": bad}, []string{"members"}, map[string]datatype.FieldType{"members": datatype.FieldTypeArray})
		if err == nil || !strings.Contains(err.Error(), `column "members"`) || !strings.Contains(err.Error(), "element 1") {
			t.Fatalf("%T: want located array error, got %v", bad, err)
		}
	}
}

func TestPostgresArrayDeclarationRejectedBeforeConnection(t *testing.T) {
	ctx := context.Background()
	pg := &PostgreSQLPlugin{}
	fields := []datatype.FieldInfo{{Name: "members", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON}}
	path := postgresPrepareTablePath("unused", "unused")
	// Missing connection information would fail differently if validation did
	// not run first. These calls cannot access any database.
	_, sessionErr := pg.OpenTableWriteSession(ctx, nil, path, plugin.TableWriteSessionOptions{Fields: fields})
	batch := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"members": nil}}}
	batchErr := pg.WriteBatch(ctx, nil, path, batch, plugin.BatchWriteOptions{})
	upsertErr := pg.UpsertBatch(ctx, nil, path, batch, plugin.TableUpsertOptions{Fields: fields, Keys: []string{"members"}})
	prepareErr := pg.PrepareTableWrite(ctx, nil, path, plugin.TableWriteOptions{Fields: fields})
	for _, err := range []error{sessionErr, batchErr, upsertErr, prepareErr} {
		if err == nil || !strings.Contains(err.Error(), `column "members"`) || !strings.Contains(err.Error(), "element_type") {
			t.Fatalf("want declaration error before connection: %v", err)
		}
	}
	for _, elementType := range []datatype.FieldType{"", datatype.FieldTypeString} {
		field := datatype.FieldInfo{Name: "members", Type: datatype.FieldTypeArray, ElementType: elementType}
		if sqlType, err := postgresSQLTypeForField(field, nil); err != nil || sqlType != "TEXT[]" {
			t.Fatalf("string array mapping: %s %v", sqlType, err)
		}
	}
}

func TestPostgresArrayNativeElementFacts(t *testing.T) {
	for _, tc := range []struct {
		native  string
		element datatype.FieldType
	}{
		{"text[]", datatype.FieldTypeString}, {"character varying(20)[]", datatype.FieldTypeString},
		{"integer[]", datatype.FieldTypeInt}, {"jsonb[]", datatype.FieldTypeJSON},
		{"numeric(8,2)[]", datatype.FieldTypeDecimal}, {"boolean[]", datatype.FieldTypeBool},
		{"custom_enum[]", datatype.FieldTypeUnknown},
	} {
		field := postgresFieldInfoFromColumn(postgresColumnInfo{Name: "members", DataType: "ARRAY", NativeType: tc.native})
		if field.Type != datatype.FieldTypeArray || field.ElementType != tc.element {
			t.Fatalf("%s: array facts lost: %#v", tc.native, field)
		}
	}
}
