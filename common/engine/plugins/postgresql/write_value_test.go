package postgresql

import (
	"bytes"
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
		{datatype.FieldTypeUnknown, []interface{}{1}, []interface{}{1}},
		{datatype.FieldTypeGeometry, bytes, "0001ff"},
		{datatype.FieldTypeGeometry, []byte{}, nil},
		{datatype.FieldTypeGeometry, "POINT(1 2)", "POINT(1 2)"},
	} {
		got, err := ProtocolTableWriteValue(tc.value, tc.typ)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("type %s: got %#v, %v; want %#v", tc.typ, got, err, tc.want)
		}
	}
}

func TestProtocolBytesDistinguishesTypedNilFromEmpty(t *testing.T) {
	var typedNil []byte
	value, err := ProtocolTableWriteValue(typedNil, datatype.FieldTypeBytes)
	if err != nil || value != nil {
		t.Fatalf("typed nil bytes must bind SQL NULL, got %#v error=%v", value, err)
	}
	for _, input := range [][]byte{{}, {0, 255, 92}} {
		value, err := ProtocolTableWriteValue(input, datatype.FieldTypeBytes)
		got, ok := value.([]byte)
		if err != nil || !ok || got == nil || !bytes.Equal(got, input) {
			t.Fatalf("non-nil bytes changed: got=%#v error=%v", value, err)
		}
	}
}
