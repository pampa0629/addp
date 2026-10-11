package postgresql

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
)

func TestPostgresWriteValueJSON(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value interface{}
		want  interface{}
	}{
		{"SQL null", nil, nil},
		{"nil bytes", []byte(nil), nil},
		{"nil raw", json.RawMessage(nil), nil},
		{"nil array", []interface{}(nil), "null"},
		{"nil object", map[string]interface{}(nil), "null"},
		{"empty array", []interface{}{}, "[]"},
		{"empty object", map[string]interface{}{}, "{}"},
		{"array", []interface{}{1, "户外", false, nil}, `[1,"户外",false,null]`},
		{"typed array", []string{"a", "b"}, `["a","b"]`},
		{"object", map[string]interface{}{"items": []interface{}{true}}, `{"items":[true]}`},
		{"bool", true, "true"},
		{"integer", int64(9007199254740993), "9007199254740993"},
		{"number", json.Number("12345678901234567890.123456789"), "12345678901234567890.123456789"},
		{"float", 1.25, "1.25"},
		{"encoded object", ` {"n":9007199254740993} `, ` {"n":9007199254740993} `},
		{"encoded array bytes", []byte(`[1,null]`), `[1,null]`},
		{"encoded raw", json.RawMessage(`{"a":1}`), `{"a":1}`},
		{"string scalar", `"户外\n活动"`, `"户外\n活动"`},
		{"empty string scalar", `""`, `""`},
		{"JSON null", "null", "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := postgresWriteValue(tc.value, datatype.FieldTypeJSON)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, %v; want %#v", got, err, tc.want)
			}
		})
	}
}

func TestPostgresWriteValueRejectsInvalidJSON(t *testing.T) {
	cycle := map[string]interface{}{}
	cycle["self"] = cycle
	for _, value := range []interface{}{
		"", "plain text", "{", "{} []", []byte{}, []byte("bad"), json.RawMessage(`{"a":}`),
		json.Number("01"), math.NaN(), math.Inf(1), make(chan int), func() {}, cycle,
	} {
		_, err := postgresWriteRow(map[string]interface{}{"caredOutdoors": value}, []string{"caredOutdoors"}, map[string]datatype.FieldType{"caredOutdoors": datatype.FieldTypeJSON})
		if err == nil || !strings.Contains(err.Error(), `column "caredOutdoors"`) {
			t.Fatalf("value %T: want located JSON error, got %v", value, err)
		}
	}
}

func TestPostgresWriteValuePreservesOtherTypes(t *testing.T) {
	bytes := []byte{0, 1, 0xff}
	for _, tc := range []struct {
		typ   datatype.FieldType
		value interface{}
		want  interface{}
	}{
		{datatype.FieldTypeBytes, bytes, bytes},
		{datatype.FieldTypeBytes, []byte{}, []byte{}},
		{datatype.FieldTypeString, `{"a":1}`, `{"a":1}`},
		{datatype.FieldTypeArray, []string{"a"}, []string{"a"}},
		{datatype.FieldTypeUnknown, []interface{}{1}, []interface{}{1}},
		{datatype.FieldTypeGeometry, bytes, "0001ff"},
		{datatype.FieldTypeGeometry, []byte{}, nil},
		{datatype.FieldTypeGeometry, "POINT(1 2)", "POINT(1 2)"},
	} {
		got, err := postgresWriteValue(tc.value, tc.typ)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("type %s: got %#v, %v; want %#v", tc.typ, got, err, tc.want)
		}
	}
}
