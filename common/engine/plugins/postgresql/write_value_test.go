package postgresql

import (
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
)

func TestPostgresWriteRowLocatesJSONError(t *testing.T) {
	_, err := postgresWriteRow(map[string]interface{}{"caredOutdoors": "{"}, []string{"caredOutdoors"}, map[string]datatype.FieldType{"caredOutdoors": datatype.FieldTypeJSON})
	if err == nil || !strings.Contains(err.Error(), `column "caredOutdoors"`) {
		t.Fatalf("want located JSON error, got %v", err)
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
