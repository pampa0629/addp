package parquet

import (
	"bytes"
	"context"
	"testing"

	"github.com/addp/common/format"
	parquetgo "github.com/parquet-go/parquet-go"
)

type testParquetRow struct {
	ID   int64  `parquet:"id"`
	Name string `parquet:"name"`
}

func TestParquetPluginImplementsTargetInterfaces(t *testing.T) {
	plugin := NewPlugin()
	var _ format.FormatPlugin = plugin
	var _ format.TableProvider = plugin
	var _ format.ScopeTableProvider = plugin
}

func TestParquetPluginDescribeAndSampleTable(t *testing.T) {
	data := buildTestParquetData(t)
	plugin := NewPlugin()

	info, err := plugin.DescribeTable(context.Background(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info.RowCount == nil || *info.RowCount != 2 {
		t.Fatalf("row count = %v, want 2", info.RowCount)
	}
	if len(info.Fields) != 2 {
		t.Fatalf("fields = %#v, want 2 fields", info.Fields)
	}

	rows, err := plugin.SampleTable(context.Background(), bytes.NewReader(data), 1, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "Bob" {
		t.Fatalf("rows = %#v, want Bob", rows)
	}
}

func buildTestParquetData(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := parquetgo.NewGenericWriter[testParquetRow](&buf)
	if _, err := writer.Write([]testParquetRow{
		{ID: 1, Name: "Alice"},
		{ID: 2, Name: "Bob"},
	}); err != nil {
		t.Fatalf("write parquet rows: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close parquet writer: %v", err)
	}
	return buf.Bytes()
}
