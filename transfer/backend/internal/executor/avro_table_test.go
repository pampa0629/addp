package executor

import (
	"bytes"
	"os"
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	avroformat "github.com/addp/common/format/plugins/avro"
	parquetformat "github.com/addp/common/format/plugins/parquet"
)

func TestAvroToParquetTransferKeepsBigIntsBytesAndNulls(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/apache-primitives.avro")
	if err != nil {
		t.Fatal(err)
	}
	avro := avroformat.NewPlugin()
	parquet := parquetformat.NewPlugin()
	info, err := avro.DescribeTable(t.Context(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	target := &fakeContentWriter{}
	executor := &TableTransferExecutor{SourceContentReader: &fakeContentReader{content: string(data)}, TargetContentWriter: target, SourceTableReadProvider: avro, SourceInfoProvider: avro, TargetTableWriterProvider: parquet}
	metrics, err := executor.Execute(t.Context(), TableTransferPlan{
		Source: TableSourcePlan{Kind: TableEndpointEncoded, Path: engineplugin.FileItemPath(7, "synthetic.avro"), Format: format.FormatAvro, Layout: format.LayoutSingle, TableInfo: info.Table},
		Target: TableTargetPlan{Kind: TableEndpointEncoded, Path: engineplugin.FileItemPath(8, "synthetic.parquet"), Format: format.FormatParquet}, BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.RecordsRead != 3 || metrics.RecordsWritten != 3 || metrics.Batches != 3 || !target.closed {
		t.Fatalf("metrics: %#v closed=%v", metrics, target.closed)
	}
	result, err := parquet.DescribeTable(t.Context(), bytes.NewReader(target.buf.Bytes()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Table.Fields[0].Type != datatype.FieldTypeBigInt || !result.Table.Fields[1].Nullable {
		t.Fatalf("target schema: %#v", result.Table.Fields)
	}
	rows, err := parquet.SampleTable(t.Context(), bytes.NewReader(target.buf.Bytes()), 0, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0]["id"] != int64(9007199254740993) || rows[1]["id"] != int64(-9007199254740993) || rows[1]["label"] != nil || !bytes.Equal(rows[1]["payload"].([]byte), []byte{0, 255}) || rows[2]["label"] != "设备三" {
		t.Fatalf("target rows: %#v", rows)
	}
}
