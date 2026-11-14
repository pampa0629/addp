package avro

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	parquetplugin "github.com/addp/common/format/plugins/parquet"
	"github.com/golang/snappy"
	parquetgo "github.com/parquet-go/parquet-go"
)

const logicalSchema = `{"type":"record","name":"money.Logical","fields":[{"name":"day","type":[{"type":"int","logicalType":"date"},"null"]},{"name":"amount","type":["null",{"type":"bytes","logicalType":"decimal","precision":38,"scale":4}]},{"name":"price","type":{"type":"fixed","name":"Price","size":8,"logicalType":"decimal","precision":18,"scale":2}},{"name":"copy","type":["Price","null"]}]}`

var logicalDays = []int32{-719162, -1, 0, 11016, 2932896}
var logicalDates = []string{"0001-01-01", "1969-12-31", "1970-01-01", "2000-02-29", "9999-12-31"}
var logicalAmounts = []string{"0.0000", "1.2345", "-1.2345", "9999999999999999999999999999999999.9999"}
var logicalAmountHex = []string{"000000", "003039", "ffffcfc7", "004b3b4ca85a86c47a098a223fffffffff"}
var logicalPrices = []int64{125, -125, 0, 999999999999999999, -999999999999999999}
var logicalPriceText = []string{"1.25", "-1.25", "0.00", "9999999999999999.99", "-9999999999999999.99"}

// Encode OCF records independently of the production codec. Hex integer
// examples include valid redundant sign extension; all three codecs share
// exactly the same source schema and semantic values.
func logicalFixture(t *testing.T, compression string, count int) ([]byte, []map[string]interface{}) {
	t.Helper()
	var output bytes.Buffer
	output.Write(rawHeader(logicalSchema, compression))
	var want []map[string]interface{}
	for start := 0; start < count; start += 7 {
		var records bytes.Buffer
		for i := start; i < min(start+7, count); i++ {
			j := i % 5
			row := map[string]interface{}{"day": logicalDates[j], "price": logicalPriceText[j], "copy": nil, "amount": nil}
			if i%11 == 10 {
				putLong(&records, 1)
				row["day"] = nil
			} else {
				putLong(&records, 0)
				putLong(&records, int64(logicalDays[j]))
			}
			if j == 4 {
				putLong(&records, 0)
			} else {
				putLong(&records, 1)
				data, err := hex.DecodeString(logicalAmountHex[j])
				if err != nil {
					t.Fatal(err)
				}
				putBytes(&records, data)
				row["amount"] = logicalAmounts[j]
			}
			var fixed [8]byte
			binary.BigEndian.PutUint64(fixed[:], uint64(logicalPrices[j]))
			records.Write(fixed[:])
			if j == 4 {
				putLong(&records, 1)
			} else {
				putLong(&records, 0)
				records.Write(fixed[:])
				row["copy"] = logicalPriceText[j]
			}
			want = append(want, row)
		}
		data := records.Bytes()
		switch compression {
		case "deflate":
			var compressed bytes.Buffer
			writer, err := flate.NewWriter(&compressed, flate.DefaultCompression)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			data = compressed.Bytes()
		case "snappy":
			compressed := snappy.Encode(nil, data)
			var crc [4]byte
			binary.BigEndian.PutUint32(crc[:], crc32.ChecksumIEEE(data))
			data = append(compressed, crc[:]...)
		}
		block := rawBlock(nil, int64(min(7, count-start)), data)
		output.Write(block)
	}
	return output.Bytes(), want
}

func TestAvroLogicalDatesDecimalsAndPagination(t *testing.T) {
	for _, compression := range []string{"null", "deflate", "snappy"} {
		t.Run(compression, func(t *testing.T) {
			data, want := logicalFixture(t, compression, 25)
			plugin := NewPlugin()
			info, err := plugin.DescribeTable(t.Context(), bytes.NewReader(data), nil)
			if err != nil {
				t.Fatal(err)
			}
			fields := info.Table.Fields
			if *info.Table.RowCount != 25 || len(fields) != 4 || fields[0].Type != datatype.FieldTypeDate || !fields[0].Nullable || fields[0].NativeType != "int" {
				t.Fatalf("date definition: %#v", info.Table)
			}
			for i, precision := range []int{38, 18, 18} {
				field := fields[i+1]
				scale := 2
				if i == 0 {
					scale = 4
				}
				if field.Type != datatype.FieldTypeDecimal || field.Precision != precision || field.Scale != scale || field.Nullable != (i != 1) {
					t.Fatalf("decimal definition: %#v", field)
				}
			}
			if fields[2].NativeType != "money.Price" || fields[3].NativeType != "money.Price" {
				t.Fatal("named fixed identity lost")
			}
			source := info.FormatInfo["schema"].(map[string]interface{})["fields"].([]interface{})
			decimal := source[1].(map[string]interface{})["type"].([]interface{})[1].(map[string]interface{})
			if decimal["logicalType"] != "decimal" || decimal["precision"] != json.Number("38") || decimal["scale"] != json.Number("4") {
				t.Fatal("physical codec mutated source schema")
			}
			reader, err := plugin.OpenTableReader(t.Context(), bytes.NewReader(data), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close(t.Context())
			var all []map[string]interface{}
			for {
				rows, err := reader.ReadRows(t.Context(), 4)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) == 0 {
					break
				}
				all = append(all, rows...)
			}
			if !reflect.DeepEqual(all, want) {
				t.Fatalf("values: %#v", all)
			}
			for _, window := range [][2]int64{{6, 12}, {24, 5}, {25, 1}} {
				rows, err := plugin.SampleTable(t.Context(), bytes.NewReader(data), window[0], window[1], nil)
				expected := want[window[0]:min(int64(len(want)), window[0]+window[1])]
				if err != nil || !reflect.DeepEqual(rows, expected) {
					t.Fatalf("page %v: %#v %v", window, rows, err)
				}
			}
		})
	}
}

func TestAvroLogicalTransferToParquetPhysicalReadback(t *testing.T) {
	data, want := logicalFixture(t, "deflate", 25)
	reader, err := NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(t.Context())
	var output bytes.Buffer
	writer, err := parquetplugin.NewPlugin().OpenTableWriter(t.Context(), &output, &datatype.TableInfo{Fields: reader.Fields()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for {
		rows, err := reader.ReadRows(t.Context(), 6)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		if err := writer.WriteRows(t.Context(), rows); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Independent of ADDP's Parquet reader: inspect the physical library rows,
	// nulls and logical annotations, with exact expected scaled integers.
	file, err := parquetgo.OpenFile(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"day", "amount", "price", "copy"} {
		leaf, ok := file.Schema().Lookup(name)
		if !ok {
			t.Fatal(name)
		}
		logical := leaf.Node.Type().LogicalType()
		if name == "day" {
			if logical.Date == nil {
				t.Fatal("DATE annotation lost")
			}
		} else {
			precision, scale := 18, 2
			if name == "amount" {
				precision, scale = 38, 4
			}
			if logical.Decimal == nil || logical.Decimal.Precision != int32(precision) || logical.Decimal.Scale != int32(scale) {
				t.Fatalf("DECIMAL annotation: %v", logical)
			}
		}
	}
	physical := parquetgo.NewReader(bytes.NewReader(output.Bytes()))
	defer physical.Close()
	for i := 0; i < len(want); i++ {
		rows := make([]parquetgo.Row, 1)
		n, err := physical.ReadRows(rows)
		if n != 1 || (err != nil && err != io.EOF) {
			t.Fatalf("physical row %d: %d %v", i, n, err)
		}
		for name, expected := range want[i] {
			leaf, _ := file.Schema().Lookup(name)
			var got parquetgo.Value
			for _, value := range rows[0] {
				if value.Column() == leaf.ColumnIndex {
					got = value
				}
			}
			if expected == nil {
				if !got.IsNull() {
					t.Fatalf("%s null lost", name)
				}
				continue
			}
			if got.IsNull() {
				t.Fatal("value became null")
			}
			switch name {
			case "day":
				if got.Int32() != logicalDays[i%5] {
					t.Fatal("calendar day shifted")
				}
			case "price", "copy":
				if got.Int64() != logicalPrices[i%5] {
					t.Fatal("fixed decimal scaled integer changed")
				}
			case "amount":
				encoded, _ := hex.DecodeString(logicalAmountHex[i%5])
				pad := byte(0)
				if encoded[0]&0x80 != 0 {
					pad = 255
				}
				for len(encoded) > 16 {
					encoded = encoded[1:]
				}
				exact := append(bytes.Repeat([]byte{pad}, 16-len(encoded)), encoded...)
				if !bytes.Equal(got.ByteArray(), exact) {
					t.Fatalf("decimal magnitude/sign changed: %x", got.ByteArray())
				}
			}
		}
	}
}

func TestAvroLogicalRejectsUnsupportedDeclarationsAndValues(t *testing.T) {
	for _, field := range []string{
		`{"type":"long","logicalType":"date"}`, `{"type":"int","logicalType":"time-millis"}`,
		`{"type":"long","logicalType":"timestamp-micros"}`, `{"type":"bytes","logicalType":"big-decimal"}`,
		`{"type":"string","logicalType":"uuid"}`, `{"type":"bytes","logicalType":null}`,
		`{"type":"bytes","logicalType":"decimal"}`, `{"type":"bytes","logicalType":"decimal","precision":0}`,
		`{"type":"bytes","logicalType":"decimal","precision":1001}`, `{"type":"bytes","logicalType":"decimal","precision":2.0}`,
		`{"type":"bytes","logicalType":"decimal","precision":"2"}`, `{"type":"bytes","logicalType":"decimal","precision":2,"scale":3}`,
		`{"type":"bytes","logicalType":"decimal","precision":2,"scale":-1}`, `{"type":"bytes","logicalType":"decimal","precision":2,"scale":0.5}`,
		`{"type":"bytes","logicalType":"decimal","precision":2,"scale":null}`, `{"type":"fixed","name":"F","size":1,"logicalType":"decimal","precision":3}`,
		`{"type":"fixed","name":"F","size":0,"logicalType":"decimal","precision":2}`, `{"type":"fixed","name":"F","size":16777217,"logicalType":"decimal","precision":2}`,
		`{"type":"fixed","size":2,"logicalType":"decimal","precision":2}`, `"Undeclared"`,
	} {
		schema := fmt.Sprintf(`{"type":"record","name":"R","fields":[{"name":"value","type":%s}]}`, field)
		if _, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(rawHeader(schema, "null")), nil); err == nil {
			t.Fatalf("unsupported declaration accepted: %s", field)
		}
	}
	for _, day := range []int64{-719163, 2932897, -2147483648, 2147483647} {
		var payload bytes.Buffer
		putLong(&payload, day)
		schema := `{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"int","logicalType":"date"}}]}`
		if _, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(rawBlock(rawHeader(schema, "null"), 1, payload.Bytes())), 0, 1, nil); err == nil {
			t.Fatalf("date out of supported calendar accepted: %d", day)
		}
	}
	for _, data := range [][]byte{nil, {0, 100}, {255, 156}, bytes.Repeat([]byte{1}, 500)} {
		var payload bytes.Buffer
		putBytes(&payload, data)
		schema := `{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"bytes","logicalType":"decimal","precision":2}}]}`
		r, err := NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(rawBlock(rawHeader(schema, "null"), 1, payload.Bytes())), nil)
		if err != nil {
			t.Fatal(err)
		}
		if rows, err := r.ReadRows(t.Context(), 1); err == nil || rows != nil {
			t.Fatalf("invalid decimal returned: %#v %v", rows, err)
		}
		if _, err := r.ReadRows(t.Context(), 1); err == nil {
			t.Fatal("reader recovered from rejected value")
		}
		r.Close(t.Context())
	}
}

func TestAvroLogicalDefaultScaleAndFixedReferenceNamespaces(t *testing.T) {
	schema := `{"type":"record","name":"r","namespace":"root","fields":[{"name":"a","type":{"type":"fixed","name":"other.F","namespace":"ignored","size":2,"logicalType":"decimal","precision":4}},{"name":"b","type":["null","other.F"]}]}`
	data := rawBlock(rawHeader(schema, "null"), 1, []byte{0, 42, 2, 255, 214})
	info, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Table.Fields[0].Scale != 0 || info.Table.Fields[1].Precision != 4 || info.Table.Fields[1].NativeType != "other.F" {
		t.Fatal("fixed definition was not inherited")
	}
	rows, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(data), 0, 1, nil)
	if err != nil || rows[0]["a"] != "42" || rows[0]["b"] != "-42" {
		t.Fatalf("default scale: %#v %v", rows, err)
	}
	for _, bad := range []string{strings.Replace(schema, `"other.F"]`, `"F"]`, 1), strings.Replace(schema, `"other.F"]`, `{"type":"fixed","name":"other.F","size":2,"logicalType":"decimal","precision":4}]`, 1)} {
		if _, err := NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(rawHeader(bad, "null")), nil); err == nil {
			t.Fatal("unresolved/duplicate fixed declaration accepted")
		}
	}
}

func TestAvroLogicalMaximumPortableDecimalPrecision(t *testing.T) {
	for _, scale := range []int{0, 1000} {
		digits := strings.Repeat("9", 1000)
		integer, _ := new(big.Int).SetString(digits, 10)
		data := integer.Bytes()
		if data[0]&0x80 != 0 {
			data = append([]byte{0}, data...)
		}
		var payload bytes.Buffer
		putBytes(&payload, data)
		schema := fmt.Sprintf(`{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"bytes","logicalType":"decimal","precision":1000,"scale":%d}}]}`, scale)
		rows, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(rawBlock(rawHeader(schema, "null"), 1, payload.Bytes())), 0, 1, nil)
		if scale > 0 {
			digits = "0." + digits
		}
		if err != nil || len(rows) != 1 || rows[0]["v"] != digits {
			t.Fatalf("precision/scale 1000 lost: %v", err)
		}
	}
}
