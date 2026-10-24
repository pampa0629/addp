package avro

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
	goavro "github.com/linkedin/goavro/v2"
)

const primitiveSchema = `{"type":"record","name":"sample","fields":[{"name":"i","type":"int"},{"name":"id","type":"long"},{"name":"label","type":["null","string"]},{"name":"flag","type":"boolean"},{"name":"f","type":"float"},{"name":"d","type":"double"},{"name":"blob","type":["bytes","null"]}]}`

func fixture(t *testing.T, codec string, count int) []byte {
	t.Helper()
	var output bytes.Buffer
	w, err := goavro.NewOCFWriter(goavro.OCFConfig{W: &output, Schema: primitiveSchema, CompressionName: codec, SyncMarker: [16]byte{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start < count; start += 7 {
		var rows []interface{}
		for i := start; i < min(start+7, count); i++ {
			var label, blob interface{}
			if i%2 == 0 {
				label = goavro.Union("string", fmt.Sprintf("设备-%d", i))
				blob = goavro.Union("bytes", []byte{0, 255, byte(i)})
			}
			rows = append(rows, map[string]interface{}{"i": int32(i), "id": int64(9007199254740993 + i), "label": label, "flag": i%2 == 0, "f": float32(1.5), "d": 2.25, "blob": blob})
		}
		if err = w.Append(rows); err != nil {
			t.Fatal(err)
		}
	}
	return output.Bytes()
}

func TestPrimitiveOCFDescriptionSampleAndContinuousReader(t *testing.T) {
	for _, codec := range []string{"null", "deflate", "snappy"} {
		t.Run(codec, func(t *testing.T) {
			data := fixture(t, codec, 25)
			p := NewPlugin()
			ctx := t.Context()
			info, err := p.DescribeTable(ctx, bytes.NewReader(data), nil)
			if err != nil {
				t.Fatal(err)
			}
			if info.Table.RowCount == nil || *info.Table.RowCount != 25 || info.FormatInfo["codec"] != codec || info.Spatial != nil || info.AccessIndex != nil {
				t.Fatalf("description: %#v", info)
			}
			wantTypes := []datatype.FieldType{datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeString, datatype.FieldTypeBool, datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeBytes}
			for i, f := range info.Table.Fields {
				if f.Type != wantTypes[i] || f.OrdinalPosition != i+1 || f.Nullable != (i == 2 || i == 6) {
					t.Fatalf("field: %#v", f)
				}
			}
			r, err := p.OpenTableReader(ctx, bytes.NewReader(data), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close(ctx)
			if !reflect.DeepEqual(r.Fields(), info.Table.Fields) {
				t.Fatal("reader and metadata schema differ")
			}
			var all []map[string]interface{}
			for {
				rows, err := r.ReadRows(ctx, 4)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) == 0 {
					break
				}
				all = append(all, rows...)
			}
			if len(all) != 25 || all[0]["id"] != int64(9007199254740993) || all[0]["label"] != "设备-0" || !reflect.DeepEqual(all[0]["blob"], []byte{0, 255, 0}) || all[1]["label"] != nil || all[1]["blob"] != nil {
				t.Fatalf("decoded rows: %#v", all)
			}
			for i, row := range all {
				if row["i"] != int32(i) || row["f"] != float32(1.5) || row["d"] != 2.25 || row["flag"] != (i%2 == 0) {
					t.Fatalf("row %d: %#v", i, row)
				}
			}
			rows, err := p.SampleTable(ctx, bytes.NewReader(data), 6, 12, nil)
			if err != nil || !reflect.DeepEqual(rows, all[6:18]) {
				t.Fatalf("cross-block pagination: %#v %v", rows, err)
			}
			rows, err = p.SampleTable(ctx, bytes.NewReader(data), 24, 5, nil)
			if err != nil || !reflect.DeepEqual(rows, all[24:]) {
				t.Fatalf("last page: %#v %v", rows, err)
			}
			rows, err = p.SampleTable(ctx, bytes.NewReader(data), 100, 5, nil)
			if err != nil || len(rows) != 0 {
				t.Fatalf("past EOF: %#v %v", rows, err)
			}
		})
	}
}

func TestEmptyOCFAndSelection(t *testing.T) {
	p := NewPlugin()
	data := fixture(t, "null", 0)
	info, err := p.DescribeTable(t.Context(), bytes.NewReader(data), nil)
	if err != nil || info.Table.RowCount == nil || *info.Table.RowCount != 0 || len(info.Table.Fields) != 7 {
		t.Fatalf("empty OCF: %#v %v", info, err)
	}
	opts := format.DefaultParseOptions()
	opts.FieldSelection = &format.FieldSelectionOptions{Include: []string{"label", "id"}}
	data = fixture(t, "snappy", 10)
	info, err = p.DescribeTable(t.Context(), bytes.NewReader(data), opts)
	if err != nil || len(info.Table.Fields) != 2 || info.Table.Fields[0].Name != "label" {
		t.Fatalf("selected schema: %#v %v", info, err)
	}
	rows, err := p.SampleTable(t.Context(), bytes.NewReader(data), 1, 1, opts)
	if err != nil || len(rows) != 1 || len(rows[0]) != 2 || rows[0]["label"] != nil || rows[0]["id"] != int64(9007199254740994) {
		t.Fatalf("selected sample: %#v %v", rows, err)
	}
	opts.FieldSelection.Include = []string{"missing"}
	if _, err = p.OpenTableReader(t.Context(), bytes.NewReader(data), opts); err == nil {
		t.Fatal("missing field accepted")
	}
	opts.FieldSelection.MissingFieldPolicy = format.MissingFieldIgnore
	r, err := p.OpenTableReader(t.Context(), bytes.NewReader(data), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(t.Context())
	rows, err = r.ReadRows(t.Context(), 2)
	if err != nil || len(r.Fields()) != 0 || len(rows) != 2 || len(rows[0]) != 0 {
		t.Fatalf("empty projection: %#v %v", rows, err)
	}
}

// Build OCF framing independently from the production reader and the codec.
func putLong(b *bytes.Buffer, n int64) {
	var data [10]byte
	size := binary.PutUvarint(data[:], uint64(n<<1)^uint64(n>>63))
	b.Write(data[:size])
}
func putBytes(b *bytes.Buffer, data []byte) { putLong(b, int64(len(data))); b.Write(data) }
func rawHeader(schema, codec string) []byte {
	var b bytes.Buffer
	b.Write([]byte{'O', 'b', 'j', 1})
	putLong(&b, 2)
	putBytes(&b, []byte("avro.schema"))
	putBytes(&b, []byte(schema))
	putBytes(&b, []byte("avro.codec"))
	putBytes(&b, []byte(codec))
	putLong(&b, 0)
	b.Write(make([]byte, 16))
	return b.Bytes()
}

const intSchema = `{"type":"record","name":"r","fields":[{"name":"v","type":"int"}]}`

func rawBlock(header []byte, count int64, data []byte) []byte {
	var b bytes.Buffer
	b.Write(header)
	putLong(&b, count)
	putLong(&b, int64(len(data)))
	b.Write(data)
	b.Write(make([]byte, 16))
	return b.Bytes()
}

func TestRejectMalformedOCFAndBudgets(t *testing.T) {
	good := rawBlock(rawHeader(intSchema, "null"), 1, []byte{2})
	badSync := append([]byte(nil), good...)
	badSync[len(badSync)-1] = 1
	var hugeSize bytes.Buffer
	hugeSize.Write(rawHeader(intSchema, "null"))
	putLong(&hugeSize, 1)
	putLong(&hugeSize, maxBlockBytes+1)
	var hugeCount bytes.Buffer
	hugeCount.Write(rawHeader(intSchema, "null"))
	putLong(&hugeCount, maxBlockRows+1)
	var overflow bytes.Buffer
	overflow.Write(rawHeader(intSchema, "null"))
	overflow.Write(bytes.Repeat([]byte{255}, 10))
	var intOverflow bytes.Buffer
	putLong(&intOverflow, 1<<32)
	var bomb bytes.Buffer
	fw, _ := flate.NewWriter(&bomb, flate.BestSpeed)
	_, _ = io.CopyN(fw, zeroReader{}, maxBlockBytes+1)
	_ = fw.Close()
	var compressed bytes.Buffer
	small, _ := flate.NewWriter(&compressed, flate.DefaultCompression)
	_, _ = small.Write([]byte{2})
	_ = small.Close()
	compressed.WriteByte(42)
	var snappyBomb bytes.Buffer
	var encoded [10]byte
	n := binary.PutUvarint(encoded[:], maxBlockBytes+1)
	snappyBomb.Write(encoded[:n])
	snappyBomb.Write(make([]byte, 4))
	for name, data := range map[string][]byte{
		"magic": []byte("ordinary"), "truncated-header": good[:8], "truncated-block": good[:len(good)-2], "sync": badSync,
		"trailing-record": rawBlock(rawHeader(intSchema, "null"), 1, []byte{2, 4}), "trailing-partial-varint": append(append([]byte(nil), good...), 128),
		"huge-size": hugeSize.Bytes(), "huge-count": hugeCount.Bytes(), "overflow": overflow.Bytes(), "zero-count": rawBlock(rawHeader(intSchema, "null"), 0, nil),
		"deflate-bomb": rawBlock(rawHeader(intSchema, "deflate"), 1, bomb.Bytes()), "snappy-bomb": rawBlock(rawHeader(intSchema, "snappy"), 1, snappyBomb.Bytes()),
		"snappy-crc":        rawBlock(rawHeader(intSchema, "snappy"), 1, []byte{1, 0, 2, 0, 0, 0, 0}),
		"unsupported-codec": rawHeader(intSchema, "zstandard"), "empty-codec": rawHeader(intSchema, ""), "missing-schema": rawHeader("", "null"),
		"deflate-trailing": rawBlock(rawHeader(intSchema, "deflate"), 1, compressed.Bytes()),
		"int-range":        rawBlock(rawHeader(intSchema, "null"), 1, intOverflow.Bytes()),
		"long-overflow":    rawBlock(rawHeader(strings.ReplaceAll(intSchema, `"int"`, `"long"`), "null"), 1, append(bytes.Repeat([]byte{255}, 10), 0)),
		"noncanonical-int": rawBlock(rawHeader(intSchema, "null"), 1, []byte{128, 0}),
		"invalid-bool":     rawBlock(rawHeader(strings.ReplaceAll(intSchema, `"int"`, `"boolean"`), "null"), 1, []byte{2}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(data), nil); err == nil {
				t.Fatal("invalid OCF accepted")
			}
		})
	}
	var header bytes.Buffer
	header.Write([]byte{'O', 'b', 'j', 1})
	putLong(&header, 1)
	putBytes(&header, []byte("avro.schema"))
	putLong(&header, maxHeaderBytes+1)
	if _, err := newReader(t.Context(), &header); err == nil {
		t.Fatal("oversized header accepted")
	}
	// A corrupt read must stay failed, never return the later records as success.
	r, err := newReader(t.Context(), bytes.NewReader(rawBlock(rawHeader(intSchema, "null"), 2, []byte{2})))
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := r.ReadRows(t.Context(), 2); err == nil || rows != nil {
		t.Fatalf("partial failed batch returned: %#v %v", rows, err)
	}
	if _, err := r.ReadRows(t.Context(), 2); err == nil {
		t.Fatal("failed reader resumed")
	}
}

// This fixture was independently encoded with Apache's Python avro 1.12.0,
// null compression and two blocks; the input rows are synthetic ADDP data.
func TestApacheAvroFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/apache-primitives.avro")
	if err != nil {
		t.Fatal(err)
	}
	p := NewPlugin()
	info, err := p.DescribeTable(t.Context(), bytes.NewReader(data), nil)
	if err != nil || info.Table.RowCount == nil || *info.Table.RowCount != 3 || len(info.Table.Fields) != 3 {
		t.Fatalf("Apache fixture description: %#v %v", info, err)
	}
	rows, err := p.SampleTable(t.Context(), bytes.NewReader(data), 1, 2, nil)
	if err != nil || len(rows) != 2 || rows[0]["id"] != int64(-9007199254740993) || rows[0]["label"] != nil || !reflect.DeepEqual(rows[0]["payload"], []byte{0, 255}) || rows[1]["label"] != "设备三" {
		t.Fatalf("Apache fixture rows: %#v %v", rows, err)
	}
}

func TestApachePythonInvalidDeflateTailIsRejected(t *testing.T) {
	// Apache Python 1.12.0 uses zlib.compress(data)[2:-1], retaining three
	// checksum bytes. Avro requires raw RFC1951 without a checksum; do not add
	// a compatibility path that silently discards those bytes.
	data, err := os.ReadFile("testdata/apache-1.12.0-deflate-trailing.avro")
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewPlugin().DescribeTable(t.Context(), bytes.NewReader(data), nil)
	if err == nil || !strings.Contains(err.Error(), "trailing data") || !format.IsDefinitiveParseError(err) {
		t.Fatalf("invalid producer output: %v", err)
	}
}

func TestPrimitiveObjectUnionAndAbsentCodec(t *testing.T) {
	schema := `{"type":"record","name":"r","fields":[{"name":"v","type":[{"type":"null"},{"type":"int"}]}]}`
	data := rawBlock(rawHeader(schema, "null"), 1, []byte{2, 84})
	rows, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(data), 0, 1, nil)
	if err != nil || len(rows) != 1 || rows[0]["v"] != int32(42) {
		t.Fatalf("primitive type objects: %#v %v", rows, err)
	}
	var header bytes.Buffer
	header.Write([]byte{'O', 'b', 'j', 1})
	putLong(&header, 1)
	putBytes(&header, []byte("avro.schema"))
	putBytes(&header, []byte(intSchema))
	putLong(&header, 0)
	header.Write(make([]byte, 16))
	rows, err = NewPlugin().SampleTable(t.Context(), bytes.NewReader(rawBlock(header.Bytes(), 1, []byte{84})), 0, 1, nil)
	if err != nil || len(rows) != 1 || rows[0]["v"] != int32(42) {
		t.Fatalf("default null codec: %#v %v", rows, err)
	}
}

func TestSchemaMetadataKeepsIntegerDefaultsExact(t *testing.T) {
	schema := `{"type":"record","name":"r","fields":[{"name":"v","type":"long","default":9007199254740993}]}`
	info, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(rawHeader(schema, "null")), nil)
	if err != nil {
		t.Fatal(err)
	}
	fields := info.FormatInfo["schema"].(map[string]interface{})["fields"].([]interface{})
	if fields[0].(map[string]interface{})["default"] != json.Number("9007199254740993") {
		t.Fatalf("schema source fact rounded: %#v", fields)
	}
}

type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) { clear(b); return len(b), nil }

func TestRejectUnsupportedSchemaWithoutLosingSemantics(t *testing.T) {
	for _, kind := range []string{`{"type":"long","logicalType":"timestamp-micros"}`, `{"type":"array","items":"string"}`, `{"type":"map","values":"string"}`, `{"type":"record","name":"nested","fields":[]}`, `{"type":"enum","name":"e","symbols":["a"]}`, `{"type":"fixed","name":"f","size":4}`, `["int","string"]`, `["null","int","string"]`, `"null"`} {
		schema := `{"type":"record","name":"r","fields":[{"name":"v","type":` + kind + `}]}`
		if _, err := newReader(t.Context(), bytes.NewReader(rawHeader(schema, "null"))); err == nil {
			t.Fatalf("unsupported schema accepted: %s", kind)
		}
	}
	// Strings cannot silently carry malformed UTF-8.
	bad := rawBlock(rawHeader(strings.ReplaceAll(intSchema, `"int"`, `"string"`), "null"), 1, []byte{2, 255})
	if _, err := NewPlugin().DescribeTable(t.Context(), bytes.NewReader(bad), nil); err == nil {
		t.Fatal("invalid string accepted")
	}
}

func TestCancellationIOFailureCloseAndUnsupportedResume(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewPlugin().OpenTableReader(ctx, bytes.NewReader(fixture(t, "null", 1)), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	r, err := NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(fixture(t, "deflate", 2)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ReadRows(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("read cancellation: %v", err)
	}
	if _, err = r.ReadRows(t.Context(), 0); err == nil {
		t.Fatal("zero limit accepted")
	}
	_ = r.Close(t.Context())
	rows, err := r.ReadRows(t.Context(), 1)
	if err != nil || len(rows) != 0 {
		t.Fatalf("closed reader: %#v %v", rows, err)
	}
	opts := format.DefaultParseOptions()
	opts.ResumeMarker = &resume.Marker{Provider: "avro"}
	if _, err = NewPlugin().OpenTableReader(t.Context(), bytes.NewReader(fixture(t, "null", 1)), opts); err == nil {
		t.Fatal("resume accepted")
	}
	if _, err = NewPlugin().OpenTableReader(t.Context(), errReader{}, nil); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("I/O failure lost: %v", err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }
