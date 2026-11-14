package avro

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/golang/snappy"
)

const nestedSchema = `{"type":"record","name":"n.Root","fields":[{"name":"id","type":"long"},{"name":"detail","type":["null",{"type":"record","name":"Detail","fields":[{"name":"x","type":["long","null"]},{"name":"amount","type":["null",{"type":"bytes","logicalType":"decimal","precision":18,"scale":4}]},{"name":"day","type":["null",{"type":"int","logicalType":"date"}]},{"name":"stamp","type":["null",{"type":"long","logicalType":"timestamp-nanos"}]},{"name":"blob","type":["null","bytes"]}]}]},{"name":"items","type":[{"type":"array","items":["Detail","null"]},"null"]},{"name":"grid","type":{"type":"array","items":{"type":"array","items":["null","long"]}}}]}`

func nestedOCF(t *testing.T, schema, codec string, records ...[]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	out.Write(rawHeader(schema, codec))
	for start := 0; start < len(records); start += 7 {
		raw := bytes.Join(records[start:min(start+7, len(records))], nil)
		data := raw
		switch codec {
		case "deflate":
			var b bytes.Buffer
			w, _ := flate.NewWriter(&b, flate.DefaultCompression)
			_, _ = w.Write(raw)
			_ = w.Close()
			data = b.Bytes()
		case "snappy":
			data = snappy.Encode(nil, raw)
			var crc [4]byte
			binary.BigEndian.PutUint32(crc[:], crc32.ChecksumIEEE(raw))
			data = append(data, crc[:]...)
		}
		out.Write(rawBlock(nil, int64(min(7, len(records)-start)), data))
	}
	return out.Bytes()
}
func nestedRecord(i int) ([]byte, map[string]interface{}) {
	var raw bytes.Buffer
	putLong(&raw, int64(9007199254740993+i))
	row := map[string]interface{}{"id": fmt.Sprint(9007199254740993 + int64(i))}
	detail := func(empty bool) map[string]interface{} {
		if empty {
			for _, n := range []int64{1, 0, 0, 0, 0} {
				putLong(&raw, n)
			}
			return map[string]interface{}{"x": nil, "amount": nil, "day": nil, "stamp": nil, "blob": nil}
		}
		putLong(&raw, 0)
		putLong(&raw, 9007199254740993)
		putLong(&raw, 1)
		putBytes(&raw, []byte{0x30, 0x0c})
		putLong(&raw, 1)
		putLong(&raw, -1)
		putLong(&raw, 1)
		putLong(&raw, -1)
		putLong(&raw, 1)
		putBytes(&raw, []byte{0, 255})
		return map[string]interface{}{"x": "9007199254740993", "amount": "1.2300", "day": "1969-12-31", "stamp": "1969-12-31T23:59:59.999999999Z", "blob": "AP8="}
	}
	if i%3 == 0 {
		putLong(&raw, 0)
		row["detail"] = nil
	} else {
		putLong(&raw, 1)
		row["detail"] = detail(i%3 == 2)
	}
	switch i % 4 {
	case 0:
		putLong(&raw, 1)
		row["items"] = nil
	case 1:
		putLong(&raw, 0)
		putLong(&raw, 0)
		row["items"] = []interface{}{}
	default:
		putLong(&raw, 0)
		putLong(&raw, 1)
		putLong(&raw, 0)
		item := detail(false)
		putLong(&raw, -1)
		putLong(&raw, 1)
		putLong(&raw, 1)
		putLong(&raw, 0)
		row["items"] = []interface{}{item, nil}
	}
	// Two inner arrays: [null, a large integer] and [], with mixed block forms.
	putLong(&raw, 2)
	putLong(&raw, 2)
	putLong(&raw, 0)
	putLong(&raw, 1)
	putLong(&raw, 9007199254740993)
	putLong(&raw, 0)
	putLong(&raw, 0)
	putLong(&raw, 0)
	row["grid"] = []interface{}{[]interface{}{nil, "9007199254740993"}, []interface{}{}}
	return raw.Bytes(), row
}
func TestAvroNestedDescribePreviewAndBatchBoundary(t *testing.T) {
	var raw [][]byte
	var expected []map[string]interface{}
	for i := 0; i < 25; i++ {
		b, row := nestedRecord(i)
		raw = append(raw, b)
		expected = append(expected, row)
	}
	for _, codec := range []string{"null", "deflate", "snappy"} {
		t.Run(codec, func(t *testing.T) {
			data := nestedOCF(t, nestedSchema, codec, raw...)
			p := NewPlugin()
			info, err := p.DescribeTable(t.Context(), bytes.NewReader(data), nil)
			if err != nil {
				t.Fatal(err)
			}
			f := info.Table.Fields
			if *info.Table.RowCount != 25 || f[1].Type != datatype.FieldTypeJSON || !f[1].Nullable || len(f[1].Fields) != 5 || f[2].ElementType != datatype.FieldTypeJSON || !f[2].Element.Nullable || f[3].Element.Type != datatype.FieldTypeArray {
				t.Fatalf("schema %#v", f)
			}
			original, _ := json.Marshal(info.FormatInfo["schema"])
			var a, b interface{}
			_ = json.Unmarshal(original, &a)
			_ = json.Unmarshal([]byte(nestedSchema), &b)
			if !reflect.DeepEqual(a, b) {
				t.Fatal("source schema changed")
			}
			rows, err := p.SampleTable(t.Context(), bytes.NewReader(data), 5, 15, nil)
			if err != nil {
				t.Fatal(err)
			}
			public, err := datatype.RowsForJSON(f, rows)
			if err == nil {
				encoded, marshalErr := json.Marshal(public)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				if decodeErr := json.Unmarshal(encoded, &public); decodeErr != nil {
					t.Fatal(decodeErr)
				}
			}
			if err != nil || !reflect.DeepEqual(public, expected[5:20]) {
				t.Fatalf("nested values lost: %#v %v", public, err)
			}
			selection := &format.ParseOptions{FieldSelection: &format.FieldSelectionOptions{Include: []string{"id"}}}
			for _, opts := range []*format.ParseOptions{nil, selection} {
				if _, err := p.OpenTableReader(t.Context(), bytes.NewReader(data), opts); err == nil {
					t.Fatal("nested batch admitted, including scalar projection")
				}
			}
			selected, err := p.SampleTable(t.Context(), bytes.NewReader(data), 0, 1, selection)
			if err != nil || len(selected[0]) != 1 {
				t.Fatalf("preview projection %v %v", selected, err)
			}
			r, err := p.open(t.Context(), bytes.NewReader(data), nil, true)
			if err != nil {
				t.Fatal(err)
			}
			copy := r.Fields()
			copy[1].Fields[0].Name = "mutated"
			copy[2].Element.Fields[0].Name = "also-mutated"
			if r.Fields()[1].Fields[0].Name != "x" || r.Fields()[2].Element.Fields[0].Name != "x" {
				t.Fatal("schema mutable alias")
			}
		})
	}
}
func TestAvroArrayLegalBlocksAndInvalidFraming(t *testing.T) {
	schema := `{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"array","items":"long"}}]}`
	for _, raw := range [][]byte{{4, 6, 54, 0}, {2, 6, 2, 54, 0}, {3, 4, 6, 54, 0}, {1, 2, 6, 2, 54, 0}} {
		rows, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(nestedOCF(t, schema, "null", raw)), 0, 1, nil)
		if err != nil || !reflect.DeepEqual(rows[0]["v"], []interface{}{int64(3), int64(27)}) {
			t.Fatalf("legal blocks %x: %v %v", raw, rows, err)
		}
	}
	var huge, minimum bytes.Buffer
	putLong(&huge, 1<<40)
	putLong(&minimum, math.MinInt64)
	for _, raw := range [][]byte{{3, 2, 6, 54, 0}, {3, 6, 6, 54, 0}, {4, 6}, {0x84, 0, 6, 54, 0}, huge.Bytes(), minimum.Bytes(), {2, 0x86, 0, 0}} {
		r, err := newReader(t.Context(), bytes.NewReader(nestedOCF(t, schema, "null", raw)))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := r.ReadRows(t.Context(), 1)
		if err == nil || rows != nil {
			t.Fatalf("bad array accepted %x: %v", raw, rows)
		}
		if _, again := r.ReadRows(t.Context(), 1); again == nil {
			t.Fatal("failed reader resumed")
		}
	}
}
func TestAvroNestedSchemaRejectionsAndBudgets(t *testing.T) {
	schemas := []string{
		`{"type":"record","name":"R","fields":[{"name":"detail","type":{"type":"record","name":"N","fields":[{"name":"a.x","type":"long"}]}}]}`,
		`{"type":"record","name":"bad-name","fields":[{"name":"x","type":"long"}]}`,
		`{"type":"record","namespace":"a..b","name":"R","fields":[{"name":"x","type":"long"}]}`,
		`{"type":"record","name":"R","fields":[{"name":"f","type":{"type":"fixed","name":"bad-name","size":8,"logicalType":"decimal","precision":18}}]}`,
		`{"type":"record","name":"a.long","fields":[{"name":"x","type":"long"}]}`,
		`{"type":"record","name":"R","fields":[{"name":"f","type":{"type":"fixed","name":"a.long","size":8,"logicalType":"decimal","precision":18}}]}`,

		`{"type":"record","name":"R","fields":[{"name":"v","type":["null","R"]}]}`,
		`{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"record","name":"E","fields":[]}}]}`,
		`{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"array","items":["int","string"]}}]}`,
		`{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"array","items":{"type":"map","values":"int"}}}]}`,
		`{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"record","name":"N","fields":[{"name":"x","type":{"type":"long","logicalType":"local-timestamp-nanos"}}]}}]}`,
	}
	typ := `"long"`
	for i := 0; i < 17; i++ {
		typ = `{"type":"array","items":` + typ + `}`
	}
	schemas = append(schemas, `{"type":"record","name":"R","fields":[{"name":"v","type":`+typ+`}]}`)
	for _, schema := range schemas {
		if _, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(rawHeader(schema, "null")), nil); err == nil {
			t.Fatalf("unsupported schema accepted %s", schema)
		}
	}
	schema := `{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"array","items":"string"}}]}`
	var raw bytes.Buffer
	putLong(&raw, 1)
	putBytes(&raw, []byte(strings.Repeat("\x00", maxStructureJSON/6+1)))
	putLong(&raw, 0)
	if _, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(nestedOCF(t, schema, "null", raw.Bytes())), 0, 1, nil); err == nil {
		t.Fatal("oversized public JSON accepted")
	}
	schema = `{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"array","items":"long"}}]}`
	raw.Reset()
	putLong(&raw, maxStructureValues)
	raw.Write(make([]byte, maxStructureValues))
	putLong(&raw, 0)
	if _, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(nestedOCF(t, schema, "null", raw.Bytes())), 0, 1, nil); err == nil {
		t.Fatal("oversized structure count accepted")
	}
}

func TestAvroNestedNamedTypesAndScalarMetadata(t *testing.T) {
	schema := `{"type":"record","name":"a.Root","fields":[{"name":"first","type":{"type":"record","name":"array","fields":[{"name":"v","type":"long"},{"name":"money","type":{"type":"fixed","name":"Money","size":8,"logicalType":"decimal","precision":18,"scale":4}}]}},{"name":"copy","type":"array"},{"name":"money_array","type":{"type":"array","items":["null","Money"]}},{"name":"stamp","type":{"type":"long","logicalType":"timestamp-nanos","fields":[1],"items":{"custom":"metadata"}}}]}`
	var raw bytes.Buffer
	putLong(&raw, 9007199254740993)
	raw.Write([]byte{0, 0, 0, 0, 0, 0, 0x30, 0x0c})
	putLong(&raw, -1)
	raw.Write([]byte{255, 255, 255, 255, 255, 255, 0xcf, 0xf4})
	putLong(&raw, 1)
	putLong(&raw, 1)
	raw.Write([]byte{0, 0, 0, 0, 0, 0, 0x30, 0x0c})
	putLong(&raw, 0)
	putLong(&raw, -1)
	data := nestedOCF(t, schema, "null", raw.Bytes())
	info, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Table.Fields[0].Fields[0].Type != datatype.FieldTypeBigInt || info.Table.Fields[1].NativeType != "a.array" || info.Table.Fields[2].Element.Precision != 18 {
		t.Fatal("named type binding lost")
	}
	rows, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(data), 0, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["first"].(map[string]interface{})["money"] != "1.2300" || rows[0]["copy"].(map[string]interface{})["money"] != "-1.2300" || rows[0]["money_array"].([]interface{})[0] != "1.2300" {
		t.Fatalf("named fixed values %#v", rows)
	}
}
func TestAvroNestedBatchBudgetDoesNotLimitMetadataOrOffset(t *testing.T) {
	schema := `{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"array","items":"long"}}]}`
	var raw bytes.Buffer
	putLong(&raw, 33000)
	raw.Write(make([]byte, 33000))
	putLong(&raw, 0)
	data := nestedOCF(t, schema, "null", raw.Bytes(), raw.Bytes())
	info, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(data), nil)
	if err != nil || *info.Table.RowCount != 2 {
		t.Fatalf("metadata depends on internal chunk: %v", err)
	}
	if _, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(data), 1, 1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(data), 0, 2, nil); err == nil {
		t.Fatal("oversized response batch accepted")
	}
}

func TestAvroFlatFloatEncodingIsUnchangedAndNestedJSONRejectsNonFinite(t *testing.T) {
	schema := `{"type":"record","name":"R","fields":[{"name":"f","type":"float"},{"name":"d","type":"double"}]}`
	var raw [12]byte
	binary.LittleEndian.PutUint32(raw[:4], 0x7fc00001)
	binary.LittleEndian.PutUint64(raw[4:], 0x7ff8000000000001)
	reader, err := NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(nestedOCF(t, schema, "null", raw[:])), nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := reader.ReadRows(t.Context(), 1)
	if err != nil || math.Float32bits(rows[0]["f"].(float32)) != 0x7fc00001 || math.Float64bits(rows[0]["d"].(float64)) != 0x7ff8000000000001 {
		t.Fatalf("flat floating encoding changed: %v %v", rows, err)
	}
	schema = `{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"array","items":"float"}}]}`
	data := append([]byte{2}, raw[:4]...)
	data = append(data, 0)
	if _, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(nestedOCF(t, schema, "null", data)), 0, 1, nil); err == nil {
		t.Fatal("non-finite nested JSON accepted")
	}
}
