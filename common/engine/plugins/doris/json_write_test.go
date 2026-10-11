package doris

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	pluginshared "github.com/addp/common/engine/plugins/shared"
)

func TestDorisTableWriteArgsUsesDeclaredTypes(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "document", Type: datatype.FieldTypeJSON},
		{Name: "raw", Type: datatype.FieldTypeBytes},
		{Name: "label", Type: datatype.FieldTypeString},
		{Name: "array", Type: datatype.FieldTypeArray},
	}
	types := pluginshared.TableWriteFieldTypes(fields)
	fields[0].Type = datatype.FieldTypeString
	args, err := dorisTableWriteArgs(context.Background(), []string{"document", "raw", "label", "array"}, types, []map[string]interface{}{
		{"document": []interface{}{map[string]interface{}{"ok": true}, nil}, "raw": []byte{0, 255}, "label": "plain", "array": []int{1, 2}},
		{"document": json.RawMessage(` {"n":9007199254740993} `)},
	})
	want := []interface{}{`[{"ok":true},null]`, "00ff", "plain", []int{1, 2}, ` {"n":9007199254740993} `, nil, nil, nil}
	if err != nil || !reflect.DeepEqual(args, want) {
		t.Fatalf("args=%#v want=%#v err=%v", args, want, err)
	}
}

func TestDorisBytesWriteUsesHexBinding(t *testing.T) {
	types := map[string]datatype.FieldType{"raw": datatype.FieldTypeBytes}
	statement := buildDorisInsertSQL("analytics", "target", []string{"raw"}, types, 3)
	if statement != "INSERT INTO `analytics`.`target` (`raw`) VALUES (UNHEX(?)), (UNHEX(?)), (UNHEX(?))" {
		t.Fatalf("binary SQL=%s", statement)
	}
	args, err := dorisTableWriteArgs(context.Background(), []string{"raw"}, types, []map[string]interface{}{
		{"raw": []byte(nil)}, {"raw": []byte{}}, {"raw": []byte{0, 255, 39, 92}},
	})
	if err != nil || !reflect.DeepEqual(args, []interface{}{nil, "", "00ff275c"}) {
		t.Fatalf("binary args=%#v err=%v", args, err)
	}
	if _, err := dorisTableWriteArgs(context.Background(), []string{"raw"}, types, []map[string]interface{}{{"raw": "text"}}); err == nil {
		t.Fatal("bytes field accepted non-byte text")
	}
}

func TestDorisTableWriteArgsRejectsLateInvalidJSON(t *testing.T) {
	rows := make([]map[string]interface{}, dorisDefaultInsertChunkSize+1)
	for i := range rows {
		rows[i] = map[string]interface{}{"document": `{"ok":true}`}
	}
	rows[len(rows)-1]["document"] = json.RawMessage("{} []")
	args, err := dorisTableWriteArgs(context.Background(), []string{"document"}, map[string]datatype.FieldType{"document": datatype.FieldTypeJSON}, rows)
	if err == nil || args != nil || !strings.Contains(err.Error(), `row 1000 field "document"`) {
		t.Fatalf("late invalid JSON: args=%v err=%v", args, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dorisTableWriteArgs(ctx, []string{"document"}, nil, rows); err != context.Canceled {
		t.Fatalf("cancel error=%v", err)
	}
}

func TestDorisSchemaValidationUsesWriteStorageMapping(t *testing.T) {
	for _, fieldType := range []datatype.FieldType{
		datatype.FieldTypeJSON, datatype.FieldTypeBytes, datatype.FieldTypeArray,
		datatype.FieldTypeMixed, datatype.FieldTypeUUID, datatype.FieldTypeTime,
	} {
		field := datatype.FieldInfo{Name: "value", Type: fieldType}
		column := dorisColumnInfo{Name: "value", NativeType: dorisSQLTypeForField(field)}
		statements, err := dorisSchemaEvolutionStatements("analytics", "target", []datatype.FieldInfo{field}, []dorisColumnInfo{column})
		if err != nil || len(statements) != 0 {
			t.Fatalf("type %s rejected its storage %s: statements=%v err=%v", fieldType, column.NativeType, statements, err)
		}
		column.NativeType = "INT"
		if _, err := dorisSchemaEvolutionStatements("analytics", "target", []datatype.FieldInfo{field}, []dorisColumnInfo{column}); err == nil {
			t.Fatalf("type %s accepted conflicting INT storage", fieldType)
		}
	}
}
