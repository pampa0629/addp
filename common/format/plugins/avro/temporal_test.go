package avro

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	parquetplugin "github.com/addp/common/format/plugins/parquet"
	"github.com/golang/snappy"
	parquetgo "github.com/parquet-go/parquet-go"
)

const utcSchema = `{"type":"record","name":"UTC","fields":[{"name":"millis","type":["null",{"type":"long","logicalType":"timestamp-millis","timezone":"America/New_York"}]},{"name":"micros","type":[{"type":"long","logicalType":"timestamp-micros"},"null"]},{"name":"nanos","type":["null",{"type":"long","logicalType":"timestamp-nanos"}]},{"name":"plain","type":"long"}]}`

var utcValues = [][]interface{}{
	{int64(-1), int64(-1), int64(-1)},
	{int64(0), int64(0), int64(0)},
	{int64(946720800123), int64(946720800123456), int64(946720800123456789)},
	{int64(-62135596800000), int64(-62135596800000000), int64(math.MinInt64)},
	{int64(253402300799999), int64(253402300799999999), int64(math.MaxInt64)},
	{int64(9007199254740), int64(9007199254740993), int64(9007199254740993)},
	{nil, int64(1), int64(1)},
	{int64(1), nil, nil},
}

var utcTexts = [][]interface{}{
	{"1969-12-31T23:59:59.999Z", "1969-12-31T23:59:59.999999Z", "1969-12-31T23:59:59.999999999Z"},
	{"1970-01-01T00:00:00.000Z", "1970-01-01T00:00:00.000000Z", "1970-01-01T00:00:00.000000000Z"},
	{"2000-01-01T10:00:00.123Z", "2000-01-01T10:00:00.123456Z", "2000-01-01T10:00:00.123456789Z"},
	{"0001-01-01T00:00:00.000Z", "0001-01-01T00:00:00.000000Z", "1677-09-21T00:12:43.145224192Z"},
	{"9999-12-31T23:59:59.999Z", "9999-12-31T23:59:59.999999Z", "2262-04-11T23:47:16.854775807Z"},
	{"2255-06-05T23:47:34.740Z", "2255-06-05T23:47:34.740993Z", "1970-04-15T05:59:59.254740993Z"},
	{nil, "1970-01-01T00:00:00.000001Z", "1970-01-01T00:00:00.000000001Z"},
	{"1970-01-01T00:00:00.001Z", nil, nil},
}

// Independent OCF encoding keeps signed integer ticks intact, including both
// nullable-union orders, negative fractions, and the valid minimum nanosecond.
func utcFixture(t *testing.T, compression string) ([]byte, []map[string]interface{}) {
	t.Helper()
	var output bytes.Buffer
	output.Write(rawHeader(utcSchema, compression))
	var expected []map[string]interface{}
	for start := 0; start < 25; start += 4 {
		var raw bytes.Buffer
		for i := start; i < min(start+4, 25); i++ {
			row := map[string]interface{}{"plain": int64(9007199254740993 + i)}
			for j, name := range []string{"millis", "micros", "nanos"} {
				value := utcValues[i%len(utcValues)][j]
				branch := int64(0)
				if value != nil {
					branch = 1
				}
				if j == 1 {
					branch = 1 - branch
				}
				putLong(&raw, branch)
				if value != nil {
					putLong(&raw, value.(int64))
				}
				row[name] = value
			}
			putLong(&raw, row["plain"].(int64))
			expected = append(expected, row)
		}
		data := raw.Bytes()
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
			encoded := snappy.Encode(nil, data)
			var crc [4]byte
			binary.BigEndian.PutUint32(crc[:], crc32.ChecksumIEEE(data))
			data = append(encoded, crc[:]...)
		}
		output.Write(rawBlock(nil, int64(min(4, 25-start)), data))
	}
	return output.Bytes(), expected
}

func TestAvroUTCTimestampsReadAndPreview(t *testing.T) {
	for _, compression := range []string{"null", "deflate", "snappy"} {
		t.Run(compression, func(t *testing.T) {
			data, want := utcFixture(t, compression)
			plugin := NewPlugin()
			info, err := plugin.DescribeTable(t.Context(), bytes.NewReader(data), nil)
			if err != nil {
				t.Fatal(err)
			}
			for i, unit := range []string{"ms", "us", "ns"} {
				field := info.Table.Fields[i]
				if field.Type != datatype.FieldTypeTimestamp || field.NativeType != "long" || !field.Nullable || field.Temporal == nil || *field.Temporal != (datatype.TemporalInfo{Unit: unit, Timezone: "UTC"}) {
					t.Fatalf("lost timestamp declaration: %#v", field)
				}
			}
			if *info.Table.RowCount != 25 || info.Table.Fields[3].Type != datatype.FieldTypeBigInt || info.Table.Fields[3].Temporal != nil {
				t.Fatal("unannotated long or exact count changed")
			}
			original := info.FormatInfo["schema"].(map[string]interface{})["fields"].([]interface{})[0].(map[string]interface{})["type"].([]interface{})[1].(map[string]interface{})
			if original["timezone"] != "America/New_York" || original["logicalType"] != "timestamp-millis" {
				t.Fatal("source metadata was rewritten")
			}
			reader, err := plugin.OpenTableReader(t.Context(), bytes.NewReader(data), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close(t.Context())
			var rows []map[string]interface{}
			for {
				batch, err := reader.ReadRows(t.Context(), 3)
				if err != nil {
					t.Fatal(err)
				}
				if len(batch) == 0 {
					break
				}
				rows = append(rows, batch...)
			}
			if !reflect.DeepEqual(rows, want) {
				t.Fatalf("native tick values changed: %#v", rows)
			}
			wire, err := datatype.RowsForJSON(info.Table.Fields, rows)
			if err != nil {
				t.Fatal(err)
			}
			for i, row := range wire {
				for j, name := range []string{"millis", "micros", "nanos"} {
					if row[name] != utcTexts[i%len(utcTexts)][j] {
						t.Fatalf("UTC precision lost row %d %s: %#v", i, name, row[name])
					}
				}
			}
			page, err := plugin.SampleTable(t.Context(), bytes.NewReader(data), 3, 6, nil)
			if err != nil || !reflect.DeepEqual(page, want[3:9]) {
				t.Fatalf("cross-block page changed ticks: %#v %v", page, err)
			}
			if !reflect.DeepEqual(rows, want) {
				t.Fatal("JSON rendering mutated native source rows")
			}
		})
	}
}

func TestAvroUTCTimestampsParquetPreservesAllInt64Ticks(t *testing.T) {
	schema := `{"type":"record","name":"R","fields":[{"name":"ms","type":{"type":"long","logicalType":"timestamp-millis"}},{"name":"us","type":{"type":"long","logicalType":"timestamp-micros"}},{"name":"ns","type":{"type":"long","logicalType":"timestamp-nanos"}}]}`
	var payload bytes.Buffer
	counts := []int64{math.MinInt64, -9007199254740993, -1, 0, 1, 9007199254740993, math.MaxInt64}
	for _, v := range counts {
		for range 3 {
			putLong(&payload, v)
		}
	}
	data := rawBlock(rawHeader(schema, "null"), int64(len(counts)), payload.Bytes())
	plugin := NewPlugin()
	info, err := plugin.DescribeTable(t.Context(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := plugin.SampleTable(t.Context(), bytes.NewReader(data), 0, int64(len(counts)), nil)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer, err := parquetplugin.NewPlugin().OpenTableWriter(t.Context(), &output, info.Table, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.WriteRows(t.Context(), rows); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	file, err := parquetgo.OpenFile(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	physical := parquetgo.NewReader(bytes.NewReader(output.Bytes()))
	defer physical.Close()
	for i, count := range counts {
		batch := make([]parquetgo.Row, 1)
		n, err := physical.ReadRows(batch)
		if n != 1 || (err != nil && err != io.EOF) {
			t.Fatalf("physical row %d: %d %v", i, n, err)
		}
		for _, field := range info.Table.Fields {
			leaf, _ := file.Schema().Lookup(field.Name)
			logical := leaf.Node.Type().LogicalType()
			if leaf.Node.Type().Kind() != parquetgo.Int64 || logical.Timestamp == nil || !logical.Timestamp.IsAdjustedToUTC {
				t.Fatal("timestamp physical type or UTC annotation changed")
			}
			unit := logical.Timestamp.Unit
			if (field.Name == "ms" && unit.Millis == nil) || (field.Name == "us" && unit.Micros == nil) || (field.Name == "ns" && unit.Nanos == nil) {
				t.Fatal("timestamp unit changed")
			}
			for _, value := range batch[0] {
				if value.Column() == leaf.ColumnIndex && (value.IsNull() || value.Int64() != count) {
					t.Fatal("physical int64 ticks changed")
				}
			}
		}
	}
	readback, err := parquetplugin.NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(output.Bytes()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readback.Close(t.Context())
	for _, f := range readback.Fields() {
		if f.Temporal == nil || f.Temporal.Timezone != "UTC" || f.Temporal.Unit != f.Name {
			t.Fatalf("Parquet metadata changed: %#v", f)
		}
	}
	back, err := readback.ReadRows(t.Context(), 100)
	if err != nil || !reflect.DeepEqual(back, rows) {
		t.Fatalf("native Parquet round trip: %#v %v", back, err)
	}
	if _, err := datatype.RowsForJSON(info.Table.Fields, rows[:1]); err == nil {
		t.Fatal("out-of-calendar milliseconds/microseconds silently rendered")
	}
}

func TestAvroUTCTimestampsRejectUnsupportedAnnotationsAndBadLongs(t *testing.T) {
	for _, kind := range []string{"timestamp-millis", "timestamp-micros", "timestamp-nanos"} {
		for _, physical := range []string{"int", "double", "bytes", "string"} {
			schema := fmt.Sprintf(`{"type":"record","name":"R","fields":[{"name":"v","type":{"type":%q,"logicalType":%q}}]}`, physical, kind)
			if _, err := NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(rawHeader(schema, "null")), nil); err == nil {
				t.Fatal("wrong physical type admitted")
			}
		}
	}
	for _, kind := range []string{"local-timestamp-millis", "local-timestamp-micros", "local-timestamp-nanos", "timestamp-seconds", "time-micros"} {
		schema := fmt.Sprintf(`{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"long","logicalType":%q}},{"name":"id","type":"long"}]}`, kind)
		options := &format.ParseOptions{FieldSelection: &format.FieldSelectionOptions{Include: []string{"id"}}}
		if _, err := NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(rawHeader(schema, "null")), options); err == nil {
			t.Fatal("projection bypassed unsupported logical type")
		}
	}
	schema := `{"type":"record","name":"R","fields":[{"name":"v","type":{"type":"long","logicalType":"timestamp-nanos"}}]}`
	for _, bad := range [][]byte{{128, 0}, append(bytes.Repeat([]byte{255}, 10), 0)} {
		r, err := NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(rawBlock(rawHeader(schema, "null"), 1, bad)), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close(t.Context())
		if rows, err := r.ReadRows(t.Context(), 1); err == nil || rows != nil {
			t.Fatal("bad long encoding returned timestamp data")
		}
		if _, err := r.ReadRows(t.Context(), 1); err == nil {
			t.Fatal("failed reader resumed as success")
		}
	}
	// The source JSON still carries the annotation; physical-codec adaptation
	// does not change the metadata declaration consumed by Meta.
	info, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(rawHeader(schema, "null")), nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(info.FormatInfo["schema"])
	if !bytes.Contains(raw, []byte(`"logicalType":"timestamp-nanos"`)) {
		t.Fatal("logical annotation lost")
	}
}
