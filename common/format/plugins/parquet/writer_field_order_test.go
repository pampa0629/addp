package parquet

import (
	"bytes"
	"reflect"
	"sort"
	"testing"

	"github.com/addp/common/datatype"
)

func TestWriterCanonicalFieldOrderKeepsAvroScalarBindings(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "small", Type: datatype.FieldTypeInt},
		{Name: "flag", Type: datatype.FieldTypeBool, Nullable: true}, {Name: "label", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "ratio", Type: datatype.FieldTypeFloat}, {Name: "value", Type: datatype.FieldTypeDouble},
		{Name: "payload", Type: datatype.FieldTypeBytes, Nullable: true},
	}
	rows := []map[string]interface{}{
		{"id": int64(9007199254740993), "small": int32(-1251), "flag": nil, "label": "", "ratio": float32(1.25), "value": float64(-2.5), "payload": []byte{}},
		{"id": int64(-9007199254740994), "small": int32(-1250), "flag": false, "label": nil, "ratio": float32(2.25), "value": float64(-3.5), "payload": nil},
	}
	var buffer bytes.Buffer
	pl := NewPlugin()
	writer, err := pl.OpenTableWriter(t.Context(), &buffer, &datatype.TableInfo{Fields: fields}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteRows(t.Context(), rows); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	info, err := pl.DescribeTable(t.Context(), bytes.NewReader(buffer.Bytes()), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names, expected []string
	for _, field := range fields {
		expected = append(expected, field.Name)
	}
	sort.Strings(expected)
	for _, field := range info.Table.Fields {
		names = append(names, field.Name)
	}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("physical field order: %v, want canonical names %v", names, expected)
	}
	for _, want := range fields {
		for _, got := range info.Table.Fields {
			if got.Name == want.Name && (got.Type != want.Type || got.Nullable != want.Nullable) {
				t.Fatalf("field %s lost type/nullability: %#v", want.Name, got)
			}
		}
	}
	actual, err := pl.SampleTable(t.Context(), bytes.NewReader(buffer.Bytes()), 0, 2, nil)
	if err != nil || !reflect.DeepEqual(actual, rows) {
		t.Fatalf("name-based scalar bindings changed: %#v %v", actual, err)
	}
}
