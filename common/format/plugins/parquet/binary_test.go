package parquet

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	parquetgo "github.com/parquet-go/parquet-go"
)

func TestUnannotatedBinaryPreservesBytesAndTextRemainsText(t *testing.T) {
	type record struct {
		Payload []byte  `parquet:"payload"`
		Fixed   [4]byte `parquet:"fixed"`
		Text    string  `parquet:"text"`
	}
	var output bytes.Buffer
	w := parquetgo.NewGenericWriter[record](&output)
	_, err := w.Write([]record{{Payload: []byte{0, 255, 128}, Fixed: [4]byte{0, 255, 128, 7}, Text: "中文"}, {Payload: []byte{}, Text: ""}, {Payload: nil, Text: "null bytes"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	native := parquetgo.NewGenericReader[record](bytes.NewReader(output.Bytes()))
	var first [1]record
	_, err = native.Read(first[:])
	_ = native.Close()
	if err != nil || !bytes.Equal(first[0].Payload, []byte{0, 255, 128}) {
		t.Fatalf("independent fixture read: %#v %v", first, err)
	}
	p := NewPlugin()
	info, err := p.DescribeTable(t.Context(), bytes.NewReader(output.Bytes()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Table.GetField("payload").Type != datatype.FieldTypeBytes || info.Table.GetField("fixed").Type != datatype.FieldTypeBytes || info.Table.GetField("text").Type != datatype.FieldTypeString {
		t.Fatalf("schema: %#v", info.Table.Fields)
	}
	rows, err := p.SampleTable(t.Context(), bytes.NewReader(output.Bytes()), 0, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows[0]["payload"], []byte{0, 255, 128}) || !reflect.DeepEqual(rows[0]["fixed"], []byte{0, 255, 128, 7}) || rows[0]["text"] != "中文" {
		t.Fatalf("binary/text conflated: %#v", rows)
	}
	r, err := p.OpenTableReader(t.Context(), bytes.NewReader(output.Bytes()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(t.Context())
	for i := 0; i < 3; i++ {
		got, err := r.ReadRows(t.Context(), 1)
		if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], rows[i]) {
			t.Fatalf("continuous binary row %d: %#v %v", i, got, err)
		}
	}
}

func TestBinaryWriterRoundTripKeepsEmptyDistinctFromNull(t *testing.T) {
	p := NewPlugin()
	var output bytes.Buffer
	w, err := p.OpenTableWriter(t.Context(), &output, &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "payload", Type: datatype.FieldTypeBytes, Nullable: true}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []interface{}{[]byte{0, 255}, []byte{}, nil}
	for _, v := range want {
		if err = w.WriteRows(t.Context(), []map[string]interface{}{{"payload": v}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err := p.SampleTable(t.Context(), bytes.NewReader(output.Bytes()), 0, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range want {
		if !reflect.DeepEqual(rows[i]["payload"], v) {
			t.Fatalf("row %d: %#v want %#v", i, rows[i]["payload"], v)
		}
	}
}
