package avro

import (
	"bytes"
	"encoding/json"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"math"
	"os"
	"reflect"
	"testing"
)

func TestMapPreviewPreservesDynamicKeysAndDeclaredValues(t *testing.T) {
	raw, err := os.ReadFile("testdata/synthetic-map.avro")
	if err != nil {
		t.Fatal(err)
	}
	p := NewPlugin()
	info, err := p.DescribeTable(t.Context(), bytes.NewReader(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	fields := info.Table.Fields
	if *info.Table.RowCount != 43 || len(fields) != 6 || fields[1].MapValue.Type != datatype.FieldTypeBigInt || !fields[1].Nullable || !fields[1].MapValue.Nullable {
		t.Fatalf("map declaration %#v", fields)
	}
	d := fields[2].MapValue.Fields
	if d[1].Precision != 38 || d[1].Scale != 6 || d[3].Temporal.Unit != "ns" || d[3].Temporal.Timezone != "UTC" || fields[3].MapValue.Enum.Name != "demo.MapState" || fields[4].Element.MapValue.Element.Enum.Name != "demo.MapState" || fields[5].MapValue.MapValue.Type != datatype.FieldTypeBigInt {
		t.Fatal("recursive value schema lost")
	}
	for _, selection := range []*format.FieldSelectionOptions{nil, {Include: []string{"id"}}} {
		opts := format.DefaultParseOptions()
		opts.FieldSelection = selection
		if _, err := p.OpenTableReader(t.Context(), bytes.NewReader(raw), opts); err == nil {
			t.Fatal("map batch source accepted")
		}
	}
	r, err := p.open(t.Context(), bytes.NewReader(raw), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(t.Context())
	rows, err := r.ReadRows(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	public, err := datatype.RowsForJSON(fields, rows)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(public[1])
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected interface{}
	if err = json.Unmarshal(wire, &actual); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(`{
  "id": "-9007199254740994",
  "metrics": {
    "": "-9007199254740994",
    "sensor.A": null,
    "a/b": "-9007199254740992",
    "中文 ": "-9007199254740991",
    "__proto__": null,
    "constructor": "-9007199254740989"
  },
  "details": {
    "内容": {
      "uid": "-9007199254740994",
      "amount": "-99999999999999999999999999999990.000100",
      "day": "1969-12-31",
      "stamp": "1969-12-31T23:59:59.999999999Z",
      "payload": "AP8B"
    },
    "none": null
  },
  "states": {
    "z": "A",
    "A": "a"
  },
  "groups": [
    {},
    {
      "a.b": [
        null,
        "A"
      ]
    }
  ],
  "nested": {
    "outer": {
      "": "-9007199254740994",
      "nil": null
    },
    "empty": {}
  }
}`), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("map values differ: %s", wire)
	}
	if public[0]["metrics"] != nil {
		t.Fatal("NULL map changed")
	}
}

func TestMapFramingAndDuplicateKeys(t *testing.T) {
	schema := `{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"map","values":"long"}}]}`
	entry := func(key []byte, value int64) []byte {
		var b bytes.Buffer
		putBytes(&b, key)
		putLong(&b, value)
		return b.Bytes()
	}
	block := func(count int64, raw []byte, size int64) []byte {
		var b bytes.Buffer
		putLong(&b, count)
		if count < 0 {
			putLong(&b, size)
		}
		b.Write(raw)
		return b.Bytes()
	}
	e := entry([]byte("中文 . "), 9007199254740993)
	for _, count := range []int64{1, -1} {
		data := append(block(count, e, int64(len(e))), 0)
		if _, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(nestedOCF(t, schema, "null", data)), nil); err != nil {
			t.Fatal(err)
		}
	}
	duplicate := append(block(1, entry([]byte("x"), 1), 0), block(-1, entry([]byte("x"), 2), 3)...)
	duplicate = append(duplicate, 0)
	for name, raw := range map[string][]byte{
		"duplicate across blocks":  duplicate,
		"wrong negative byte size": append(block(-1, e, int64(len(e)+1)), 0),
		"negative size":            append(block(-1, e, -1), 0),
		"truncated":                block(1, []byte{4, 'x'}, 0),
		"bad UTF8":                 append(block(1, entry([]byte{255}, 1), 0), 0),
		"noncanonical key length":  append(block(1, []byte{0x80, 0, 2}, 0), 0),
		"count overflow":           block(math.MinInt64, nil, 0),
		"value budget":             block(32769, nil, 0),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(nestedOCF(t, schema, "null", raw)), nil); err == nil {
				t.Fatal("bad map accepted")
			}
		})
	}
	for _, s := range []string{`{"type":"map"}`, `{"type":"map","values":["int","string"]}`, `{"type":"map","values":"missing"}`, `{"type":"map","values":"null"}`} {
		raw := rawHeader(`{"type":"record","name":"R","fields":[{"name":"v","type":`+s+`}]}`, "null")
		if _, err := newReader(t.Context(), bytes.NewReader(raw)); err == nil {
			t.Fatal("unsupported value schema accepted")
		}
	}
}

func TestMapSchemaBudgetAndNamedValueReferences(t *testing.T) {
	value := `"long"`
	for i := 0; i < 18; i++ {
		value = `{"type":"map","values":` + value + `}`
	}
	if _, _, _, err := recordSchema([]byte(`{"type":"record","name":"R","fields":[{"name":"v","type":` + value + `}]}`)); err == nil {
		t.Fatal("map depth limit bypassed")
	}
	schema := `{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"map","values":{"type":"enum","name":"array","symbols":["z","A"]}}},{"name":"copy","type":{"type":"map","values":["null","array"]}}]}`
	var raw bytes.Buffer
	putLong(&raw, 1)
	putBytes(&raw, []byte("a.b"))
	putLong(&raw, 0)
	putLong(&raw, 0)
	putLong(&raw, -1)
	var entry bytes.Buffer
	putBytes(&entry, []byte(""))
	putLong(&entry, 1)
	putLong(&entry, 1)
	putLong(&raw, int64(entry.Len()))
	raw.Write(entry.Bytes())
	putLong(&raw, 0)
	p := NewPlugin()
	data := nestedOCF(t, schema, "null", raw.Bytes())
	r, err := p.open(t.Context(), bytes.NewReader(data), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(t.Context())
	rows, err := r.ReadRows(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["v"].(map[string]interface{})["a.b"] != "z" || rows[0]["copy"].(map[string]interface{})[""] != "A" {
		t.Fatal(rows)
	}
}
