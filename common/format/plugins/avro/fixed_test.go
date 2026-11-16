package avro

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestFixedDeclaredNamesWidthsAndExactBytes(t *testing.T) {
	for _, size := range []int{1, 4, 16, 32} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			schema := fmt.Sprintf(`{"type":"record","name":"R","namespace":"demo","fields":[{"name":"a","type":{"type":"fixed","name":"array","size":%d}},{"name":"b","type":["null","array"]},{"name":"c","type":{"type":"fixed","name":"other.array","namespace":"ignored","size":%d}},{"name":"items","type":{"type":"array","items":"array"}},{"name":"lookup","type":{"type":"map","values":["array","null"]}}]}`, size, size)
			value := make([]byte, size)
			for i := range value {
				value[i] = byte(i * 127)
			}
			// a, b=null, c, items=[a], lookup={x:a}. Fixed has no length prefix.
			payload := append(append([]byte{}, value...), 0)
			payload = append(payload, value...)
			payload = append(payload, 2)
			payload = append(payload, value...)
			payload = append(payload, 0, 2, 2, 'x', 0)
			payload = append(payload, value...)
			payload = append(payload, 0)
			raw := rawBlock(rawHeader(schema, "null"), 1, payload)
			p := NewPlugin()
			info, err := p.DescribeTable(t.Context(), bytes.NewReader(raw), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range []datatype.FieldInfo{info.Table.Fields[0], info.Table.Fields[1], *info.Table.Fields[3].Element, *info.Table.Fields[4].MapValue} {
				if f.Type != datatype.FieldTypeBytes || f.NativeType != "fixed" || f.Fixed == nil || f.Fixed.Name != "demo.array" || f.Fixed.Size != size || f.Size != 0 {
					t.Fatalf("lost fixed declaration: %#v", f)
				}
			}
			if info.Table.Fields[2].Fixed.Name != "other.array" {
				t.Fatal("equal-width names collapsed")
			}
			rows, err := p.SampleTable(t.Context(), bytes.NewReader(raw), 0, 10, nil)
			want := []map[string]interface{}{{"a": value, "b": nil, "c": value, "items": []interface{}{value}, "lookup": map[string]interface{}{"x": value}}}
			if err != nil || !reflect.DeepEqual(rows, want) {
				t.Fatalf("fixed bytes: %#v %v", rows, err)
			}
		})
	}
}

func TestFixedRejectsBadDeclarationsReferencesAndFraming(t *testing.T) {
	for _, decl := range []string{
		`{"type":"fixed","name":"F"}`, `{"type":"fixed","name":"F","size":0}`, `{"type":"fixed","name":"F","size":-1}`, `{"type":"fixed","name":"F","size":1.5}`, `{"type":"fixed","name":"F","size":"4"}`, `{"type":"fixed","name":"F","size":16777217}`, `{"type":"fixed","name":"int","size":4}`, `{"type":"fixed","name":"bad-name","size":4}`, `{"type":"fixed","name":"F","namespace":7,"size":4}`, `{"type":"fixed","name":"F","size":4,"logicalType":"uuid"}`, `{"type":"fixed","name":"F","size":12,"logicalType":"duration"}`,
	} {
		schema := `{"type":"record","name":"R","fields":[{"name":"x","type":` + decl + `}]}`
		if _, err := newReader(t.Context(), bytes.NewReader(rawHeader(schema, "null"))); err == nil {
			t.Fatalf("bad fixed accepted: %s", decl)
		}
	}
	for _, fields := range []string{
		`{"name":"a","type":"F"}`,
		`{"name":"a","type":{"type":"fixed","name":"F","size":4}},{"name":"b","type":{"type":"fixed","name":"F","size":8}}`,
		`{"name":"a","type":{"type":"fixed","name":"F","namespace":"n","size":4}},{"name":"b","type":"F"}`,
	} {
		if _, err := newReader(t.Context(), bytes.NewReader(rawHeader(`{"type":"record","name":"R","fields":[`+fields+`]}`, "null"))); err == nil {
			t.Fatalf("bad named reference accepted: %s", fields)
		}
	}
	schema := `{"type":"record","name":"R","fields":[{"name":"x","type":{"type":"fixed","name":"F","size":4}}]}`
	for _, payload := range [][]byte{{0, 255, 128}, {0, 255, 128, 0, 0}} {
		if _, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(rawBlock(rawHeader(schema, "null"), 1, payload)), nil); err == nil {
			t.Fatal("invalid block accepted")
		}
	}
}

func TestFixedProjectionCannotEnableBatchTransfer(t *testing.T) {
	schema := `{"type":"record","name":"R","fields":[{"name":"id","type":"int"},{"name":"value","type":{"type":"fixed","name":"F","size":4}}]}`
	for _, opts := range []*format.ParseOptions{nil, {FieldSelection: &format.FieldSelectionOptions{Include: []string{"id"}}}} {
		if _, err := NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(rawHeader(schema, "null")), opts); err == nil || !strings.Contains(err.Error(), "fixed fields support preview only") {
			t.Fatalf("fixed escaped before projection: %v", err)
		}
	}
}
