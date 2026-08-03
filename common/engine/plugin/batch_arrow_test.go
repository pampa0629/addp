package plugin

import (
	"bytes"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
)

func TestBatchArrowStreamWriterWritesMultipleBatches(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
	}
	var payload bytes.Buffer
	writer, err := NewBatchArrowStreamWriter(&payload, fields)
	if err != nil {
		t.Fatalf("NewBatchArrowStreamWriter: %v", err)
	}
	for _, rows := range [][]map[string]any{
		{{"id": int64(1), "name": "first"}},
		{{"id": int64(2), "name": nil}},
	} {
		if err := writer.WriteBatch(&BatchData{Fields: fields, Rows: rows}); err != nil {
			t.Fatalf("WriteBatch: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reader, err := ipc.NewReader(bytes.NewReader(payload.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer reader.Release()
	var ids []int64
	for reader.Next() {
		column := reader.Record().Column(0).(*array.Int64)
		for index := 0; index < column.Len(); index++ {
			ids = append(ids, column.Value(index))
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("read Arrow stream: %v", err)
	}
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("decoded ids = %#v", ids)
	}
}
