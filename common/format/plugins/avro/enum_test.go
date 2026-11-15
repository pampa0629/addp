package avro

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const enumSchema = `{"type":"record","name":"Root","namespace":"demo","fields":[{"name":"state","type":{"type":"enum","name":"Status","symbols":["z","A","a"],"default":"A"}},{"name":"optional","type":["null","Status"]},{"name":"detail","type":{"type":"record","name":"Detail","fields":[{"name":"state","type":"Status"}]}},{"name":"items","type":{"type":"array","items":["Status","null"]}}]}`

func TestEnumDeclaredNamesSymbolsAndValues(t *testing.T) {
	// z, null, a, [A, null, z]. The indices are encoded independently.
	raw := rawBlock(rawHeader(enumSchema, "null"), 1, []byte{0, 0, 4, 6, 0, 2, 2, 0, 0, 0})
	p := NewPlugin()
	info, err := p.DescribeTable(t.Context(), bytes.NewReader(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := &datatype.EnumInfo{Name: "demo.Status", Symbols: []string{"z", "A", "a"}}
	for _, field := range []datatype.FieldInfo{info.Table.Fields[0], info.Table.Fields[1], info.Table.Fields[2].Fields[0], *info.Table.Fields[3].Element} {
		if field.Type != datatype.FieldTypeString || field.NativeType != "enum" || !reflect.DeepEqual(field.Enum, want) {
			t.Fatalf("enum declaration lost: %#v", field)
		}
	}
	if !info.Table.Fields[1].Nullable || !info.Table.Fields[3].Element.Nullable {
		t.Fatal("nullable enum lost")
	}
	rows, err := p.SampleTable(t.Context(), bytes.NewReader(raw), 0, 20, nil)
	expected := []map[string]interface{}{{"state": "z", "optional": nil, "detail": map[string]interface{}{"state": "a"}, "items": []interface{}{"A", nil, "z"}}}
	if err != nil || !reflect.DeepEqual(rows, expected) {
		t.Fatalf("enum values: %#v %v", rows, err)
	}
	schema := info.FormatInfo["schema"].(map[string]interface{})
	declared := schema["fields"].([]interface{})[0].(map[string]interface{})["type"].(map[string]interface{})
	if declared["name"] != "Status" || declared["default"] != "A" {
		t.Fatal("raw schema rewritten")
	}
	if *info.Table.RowCount != 1 {
		t.Fatal("wrong count")
	}
}

func TestEnumNamesAndCodecDoNotCollide(t *testing.T) {
	schema := `{"type":"record","name":"R","fields":[{"name":"a","type":{"type":"enum","name":"array","symbols":["FIRST","LAST"]}},{"name":"b","type":{"type":"enum","name":"b.array","namespace":"ignored","symbols":["X","Y"]}},{"name":"c","type":["array","null"]},{"name":"d","type":{"type":"array","items":"b.array"}}]}`
	rows, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(rawBlock(rawHeader(schema, "null"), 1, []byte{2, 0, 0, 0, 2, 2, 0})), 0, 1, nil)
	expected := []map[string]interface{}{{"a": "LAST", "b": "X", "c": "FIRST", "d": []interface{}{"Y"}}}
	if err != nil || !reflect.DeepEqual(rows, expected) {
		t.Fatalf("named enums collide: %#v %v", rows, err)
	}
}

func TestEnumBadDeclarationsAndIndices(t *testing.T) {
	for _, declaration := range []string{
		`{"type":"enum","name":"E","symbols":[]}`,
		`{"type":"enum","name":"E","symbols":["A","A"]}`,
		`{"type":"enum","name":"E","symbols":["A.B"]}`,
		`{"type":"enum","name":"E","symbols":[""]}`,
		`{"type":"enum","name":"E","symbols":[1]}`,
		`{"type":"enum","name":"int","symbols":["A"]}`,
		`{"type":"enum","name":"bad-name","symbols":["A"]}`,
		`{"type":"enum","name":"E","namespace":7,"symbols":["A"]}`,
		`{"type":"enum","name":"E","symbols":["A"],"default":"B"}`,
		`{"type":"enum","name":"E","symbols":["A"],"default":null}`,
		`{"type":"enum","name":"E","symbols":["A"],"logicalType":"uuid"}`,
	} {
		schema := `{"type":"record","name":"R","fields":[{"name":"x","type":` + declaration + `}]}`
		if _, err := newReader(t.Context(), bytes.NewReader(rawHeader(schema, "null"))); err == nil {
			t.Fatalf("bad enum accepted: %s", declaration)
		}
	}
	for _, fields := range []string{
		`{"name":"a","type":"E"},{"name":"b","type":{"type":"enum","name":"E","symbols":["A"]}}`,
		`{"name":"a","type":{"type":"enum","name":"E","symbols":["A"]}},{"name":"b","type":{"type":"record","name":"E","fields":[{"name":"x","type":"int"}]}}`,
		`{"name":"a","type":{"type":"enum","name":"E","namespace":"n","symbols":["A"]}},{"name":"b","type":"E"}`,
	} {
		if _, err := newReader(t.Context(), bytes.NewReader(rawHeader(`{"type":"record","name":"R","fields":[`+fields+`]}`, "null"))); err == nil {
			t.Fatalf("bad named reference accepted: %s", fields)
		}
	}
	schema := `{"type":"record","name":"R","fields":[{"name":"x","type":{"type":"enum","name":"E","symbols":["A","B"],"default":"A"}}]}`
	for _, bad := range [][]byte{{1}, {4}, {128, 0}, {128}} {
		if _, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(rawBlock(rawHeader(schema, "null"), 1, bad)), nil); err == nil {
			t.Fatalf("bad index accepted or default substituted: %x", bad)
		}
	}
}

func TestEnumProjectionCannotEnableBatchTransfer(t *testing.T) {
	for _, schema := range []string{
		`{"type":"record","name":"R","fields":[{"name":"id","type":"int"},{"name":"value","type":{"type":"enum","name":"E","symbols":["A"]}}]}`,
		enumSchema,
	} {
		for _, opts := range []*format.ParseOptions{nil, {FieldSelection: &format.FieldSelectionOptions{Include: []string{"id"}}}} {
			if _, err := NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(rawHeader(schema, "null")), opts); err == nil || !strings.Contains(err.Error(), "support preview only") {
				t.Fatalf("enum reader escaped before projection: %v", err)
			}
		}
	}
}

func TestEnumExpansionBudget(t *testing.T) {
	symbols := make([]string, maxFields)
	for i := range symbols {
		symbols[i] = "S" + strings.Repeat("_", i)
	}
	declaration := map[string]interface{}{"type": "enum", "name": "E", "symbols": symbols}
	schema := map[string]interface{}{"type": "record", "name": "R", "fields": []interface{}{map[string]interface{}{"name": "x", "type": declaration}}}
	raw, _ := json.Marshal(schema)
	if _, _, _, err := recordSchema(raw); err == nil {
		t.Fatal("enum expansion exceeded budget")
	}
}
