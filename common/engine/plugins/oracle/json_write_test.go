package oracle

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	go_ora "github.com/sijms/go-ora/v2"
)

func TestOracleJSONWriteValues(t *testing.T) {
	for _, nullable := range []bool{true, false} {
		field := datatype.FieldInfo{Name: "DOCUMENT", Type: datatype.FieldTypeJSON, Nullable: nullable}
		for _, tc := range []struct {
			value interface{}
			want  interface{}
		}{
			{map[string]interface{}{"ok": true}, `{"ok":true}`},
			{[]interface{}{1, false}, "[1,false]"},
			{json.RawMessage(`{"n":9007199254740993}`), `{"n":9007199254740993}`},
			{true, "true"}, {map[string]interface{}(nil), "null"},
			{nil, nil}, {[]byte(nil), nil}, {json.RawMessage(nil), nil},
		} {
			value, err := oracleTableWriteValue(field, tc.value, nil)
			expression, args, applyErr := oracleApplyValueExpression(field, tc.value, nil, 2)
			if tc.want == nil && !nullable {
				if err == nil || applyErr == nil || !strings.Contains(err.Error(), "NOT NULL") || !strings.Contains(applyErr.Error(), "NOT NULL") {
					t.Fatalf("non-null field accepted %T: insert=%v apply=%v", tc.value, err, applyErr)
				}
				continue
			}
			if err != nil || applyErr != nil || !reflect.DeepEqual(value, tc.want) {
				t.Fatalf("input %T got=%#v want=%#v insert=%v apply=%v", tc.value, value, tc.want, err, applyErr)
			}
			if tc.want == nil {
				if expression != "CAST(NULL AS JSON)" || len(args) != 0 {
					t.Fatalf("SQL NULL expression=%s args=%v", expression, args)
				}
			} else if expression != ":2" || !reflect.DeepEqual(args, []interface{}{tc.want}) {
				t.Fatalf("JSON expression=%s args=%v", expression, args)
			}
		}
		for _, invalid := range []interface{}{"plain", []byte{}, json.RawMessage("{"), make(chan int)} {
			_, insertErr := oracleTableWriteValue(field, invalid, nil)
			_, _, applyErr := oracleApplyValueExpression(field, invalid, nil, 1)
			if insertErr == nil || applyErr == nil || !strings.Contains(insertErr.Error(), "DOCUMENT") || !strings.Contains(applyErr.Error(), "DOCUMENT") {
				t.Fatalf("invalid %T: insert=%v apply=%v", invalid, insertErr, applyErr)
			}
		}
	}
	value, err := oracleTableWriteValue(datatype.FieldInfo{Name: "PAYLOAD", Type: datatype.FieldTypeBytes}, []byte{0, 255}, nil)
	if err != nil || !reflect.DeepEqual(value, go_ora.Blob{Data: []byte{0, 255}}) {
		t.Fatalf("binary semantics changed: %#v %v", value, err)
	}
}

func TestOracleWriteFailureUsesColumnRatherThanBindPosition(t *testing.T) {
	for _, tc := range []struct {
		typ   datatype.FieldType
		value interface{}
		code  plugin.TableWriteErrorCode
	}{
		{datatype.FieldTypeJSON, make(chan int), plugin.TableWriteJSONInvalid},
		{datatype.FieldTypeBytes, "private value", plugin.TableWriteBytesInvalid},
	} {
		fields := []datatype.FieldInfo{
			{Name: "NULL_ONE", Type: datatype.FieldTypeString, Nullable: true},
			{Name: "NULL_TWO", Type: datatype.FieldTypeJSON, Nullable: true},
			{Name: "VALUE", Type: tc.typ},
		}
		row := map[string]interface{}{"NULL_ONE": nil, "NULL_TWO": json.RawMessage(nil), "VALUE": tc.value}
		session := &oracleTableWriteSession{fields: fields}
		for name, err := range map[string]error{
			"insert": session.WriteBatch(t.Context(), &plugin.BatchData{Fields: []datatype.FieldInfo{{Name: "VALUE", Type: datatype.FieldTypeString}}, Rows: []map[string]interface{}{row}}),
			// Both preceding NULLs consume no MERGE bindings. VALUE is still column 3.
			"merge": mergeOracleApplyRow(t.Context(), nil, "SYNTHETIC", "TARGET", row, fields, nil, []string{"VALUE"}),
		} {
			var typed *plugin.TableWriteError
			if !errors.As(err, &typed) || typed.Code != tc.code || typed.Field == nil || typed.Field.ColumnIndex != 3 || typed.Field.DeclaredType != tc.typ {
				t.Fatalf("%s lost column/type: %#v error=%v", name, typed, err)
			}
			if tc.typ == datatype.FieldTypeJSON {
				var cause *json.UnsupportedTypeError
				if !errors.As(err, &cause) {
					t.Fatalf("%s lost encoder cause: %v", name, err)
				}
			}
		}
	}
}
