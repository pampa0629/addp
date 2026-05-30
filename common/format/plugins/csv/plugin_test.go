package csv

import (
	"bytes"
	"context"
	"github.com/addp/common/datatype"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/common/resume"
)

func TestCSVPlugin_DescribeTable(t *testing.T) {
	csvData := `name,age,score,active
Alice,25,95.5,true
Bob,30,87.3,false
Charlie,28,92.1,true`

	reader := strings.NewReader(csvData)
	plugin := NewPlugin(nil)

	tableInfo, err := plugin.DescribeTable(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}

	if len(tableInfo.Table.Fields) != 4 {
		t.Errorf("Expected 4 fields, got %d", len(tableInfo.Table.Fields))
	}

	// 检查字段类型推断
	expectedTypes := map[string]datatype.FieldType{
		"name":   datatype.FieldTypeString,
		"age":    datatype.FieldTypeInt,
		"score":  datatype.FieldTypeDouble, // CSV 浮点数默认为双精度
		"active": datatype.FieldTypeBool,
	}

	for _, field := range tableInfo.Table.Fields {
		if expectedType, ok := expectedTypes[field.Name]; ok {
			if field.Type != expectedType {
				t.Errorf("Field %s: expected type %s, got %s", field.Name, expectedType, field.Type)
			}
		}
	}
}

func TestCSVPlugin_SampleTable(t *testing.T) {
	csvData := `id,name,value
1,Test,100
2,Sample,200`

	reader := strings.NewReader(csvData)
	plugin := NewPlugin(nil)

	records, err := plugin.SampleTable(context.Background(), reader, 0, -1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}

	if len(records) != 2 {
		t.Errorf("Expected 2 records, got %d", len(records))
	}

	// 验证第一条记录
	if records[0]["name"] != "Test" {
		t.Errorf("Expected name='Test', got %v", records[0]["name"])
	}

	// 验证类型转换
	if v, ok := records[0]["id"].(int64); !ok || v != 1 {
		t.Errorf("Expected id=1 (int64), got %v (%T)", records[0]["id"], records[0]["id"])
	}
}

func TestCSVPlugin_FieldSelection(t *testing.T) {
	csvData := `id,name,value
1,Test,100
2,Sample,200`
	opts := format.DefaultParseOptions()
	opts.FieldSelection = &format.FieldSelectionOptions{Include: []string{"name", "id"}}
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(csvData), opts)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if len(info.Table.Fields) != 2 || info.Table.Fields[0].Name != "name" || info.Table.Fields[1].Name != "id" {
		t.Fatalf("fields = %#v, want name,id", info.Table.Fields)
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(csvData), 0, 1, opts)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || len(rows[0]) != 2 || rows[0]["name"] != "Test" {
		t.Fatalf("rows = %#v, want selected name/id", rows)
	}
	if _, ok := rows[0]["value"]; ok {
		t.Fatalf("rows = %#v, value should be pruned", rows)
	}

	reader, err := plugin.OpenTableReader(context.Background(), strings.NewReader(csvData), opts)
	if err != nil {
		t.Fatalf("OpenTableReader failed: %v", err)
	}
	defer reader.Close(context.Background())
	if fields := reader.Fields(); len(fields) != 2 || fields[0].Name != "name" {
		t.Fatalf("fields = %#v, want selected fields", fields)
	}
	readRows, err := reader.ReadRows(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadRows failed: %v", err)
	}
	if len(readRows) != 2 || len(readRows[0]) != 2 || readRows[0]["name"] != "Test" {
		t.Fatalf("read rows = %#v, want selected rows", readRows)
	}
}

func TestCSVPlugin_FieldSelectionMissingFieldErrors(t *testing.T) {
	opts := format.DefaultParseOptions()
	opts.FieldSelection = &format.FieldSelectionOptions{Include: []string{"missing"}}
	_, err := NewPlugin(nil).DescribeTable(context.Background(), strings.NewReader("id,name\n1,A\n"), opts)
	if err == nil {
		t.Fatal("DescribeTable succeeded, want missing field error")
	}
}

func TestCSVPlugin_DescribeFormat(t *testing.T) {
	plugin := NewPlugin(nil)
	reader := strings.NewReader("id,name\n1,Alice\n")

	info, err := plugin.DescribeFormat(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("DescribeFormat failed: %v", err)
	}
	if info["delimiter"] != nil || info["has_header"] != nil {
		t.Fatalf("table native facts should not be in format info: %#v", info)
	}
	if info["column_count"] != 2 {
		t.Fatalf("column_count = %#v, want 2", info["column_count"])
	}
}

func TestCSVPlugin_DescribeTableWritesTableNative(t *testing.T) {
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader("id,name\n1,Alice\n"), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info == nil || info.Table == nil {
		t.Fatalf("DescribeTable() = %#v", info)
	}
	if info.Table.Native["delimiter"] != "," || info.Table.Native["has_header"] != true {
		t.Fatalf("table native = %#v, want delimiter and has_header", info.Table.Native)
	}
	if info.FormatInfo["delimiter"] != nil || info.FormatInfo["has_header"] != nil {
		t.Fatalf("format info should not contain table native facts: %#v", info.FormatInfo)
	}
}

func TestCSVPlugin_ImplementsTargetInterfaces(t *testing.T) {
	plugin := NewPlugin(nil)
	var _ format.FormatPlugin = plugin
	var _ format.FormatInfoProvider = plugin
	var _ format.TableInfoProvider = plugin
	var _ format.TableSampleReader = plugin
	var _ format.TableReaderProvider = plugin
	var _ format.TableWriterProvider = plugin
	if !format.SupportsAccessIndex(plugin.Format()) {
		t.Fatalf("SupportsAccessIndex(%q) = false, want true", plugin.Format())
	}
}

func TestCSVPlugin_OpenTableReader(t *testing.T) {
	plugin := NewPlugin(nil)
	reader, err := plugin.OpenTableReader(context.Background(), strings.NewReader("id,name,active\n1,Alice,true\n2,Bob,false\n"), nil)
	if err != nil {
		t.Fatalf("OpenTableReader failed: %v", err)
	}
	fields := reader.Fields()
	if len(fields) != 3 || fields[1].Name != "name" {
		t.Fatalf("fields = %#v", fields)
	}

	rows, err := reader.ReadRows(context.Background(), 1)
	if err != nil {
		t.Fatalf("ReadRows first batch failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "Alice" {
		t.Fatalf("first rows = %#v", rows)
	}

	rows, err = reader.ReadRows(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReadRows second batch failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["active"] != false {
		t.Fatalf("second rows = %#v", rows)
	}

	rows, err = reader.ReadRows(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReadRows EOF batch failed: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("EOF rows = %#v, want empty", rows)
	}
	if err := reader.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestCSVPluginRejectsResumeMarker(t *testing.T) {
	plugin := NewPlugin(nil)
	parseOpts := format.DefaultParseOptions()
	parseOpts.ResumeMarker = &resume.Marker{Version: resume.MarkerVersionV1}
	if _, err := plugin.OpenTableReader(context.Background(), strings.NewReader("id\n1\n"), parseOpts); err == nil {
		t.Fatal("OpenTableReader succeeded with resume marker, want explicit unsupported error")
	}

	writeOpts := format.DefaultWriteOptions()
	writeOpts.ResumeMarker = &resume.Marker{Version: resume.MarkerVersionV1}
	if _, err := plugin.OpenTableWriter(context.Background(), &bytes.Buffer{}, &datatype.TableInfo{
		Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}},
	}, writeOpts); err == nil {
		t.Fatal("OpenTableWriter succeeded with resume marker, want explicit unsupported error")
	}
}

func TestCSVPlugin_OpenTableWriter(t *testing.T) {
	plugin := NewPlugin(nil)
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "name", Type: datatype.FieldTypeString},
			{Name: "note", Type: datatype.FieldTypeString},
		},
	}
	var buf bytes.Buffer
	writer, err := plugin.OpenTableWriter(context.Background(), &buf, tableInfo, nil)
	if err != nil {
		t.Fatalf("OpenTableWriter failed: %v", err)
	}
	err = writer.WriteRows(context.Background(), []map[string]interface{}{
		{"id": int64(1), "name": "Alice", "note": "hello, csv"},
		{"id": int64(2), "name": "Bob", "note": nil},
	})
	if err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	want := "id,name,note\n1,Alice,\"hello, csv\"\n2,Bob,\n"
	if buf.String() != want {
		t.Fatalf("csv output = %q, want %q", buf.String(), want)
	}
}

func TestCSVPlugin_OpenTableWriterWithoutHeader(t *testing.T) {
	plugin := NewPlugin(nil)
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{{Name: "id"}, {Name: "name"}},
	}
	opts := format.DefaultWriteOptions()
	opts.OmitHeader = true
	var buf bytes.Buffer
	writer, err := plugin.OpenTableWriter(context.Background(), &buf, tableInfo, opts)
	if err != nil {
		t.Fatalf("OpenTableWriter failed: %v", err)
	}
	if err := writer.WriteRows(context.Background(), []map[string]interface{}{{"id": 1, "name": "Alice"}}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if got, want := buf.String(), "1,Alice\n"; got != want {
		t.Fatalf("csv output = %q, want %q", got, want)
	}
}

func TestTSVPlugin_DescribeAndSampleTable(t *testing.T) {
	tsvData := "name\tage\nAlice\t25\nBob\t30\n"
	plugin := NewTSVPlugin(nil)

	tableInfo, err := plugin.DescribeTable(context.Background(), strings.NewReader(tsvData), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if plugin.Format() != format.FormatTSV {
		t.Fatalf("Format = %q, want tsv", plugin.Format())
	}
	if len(tableInfo.Table.Fields) != 2 || tableInfo.Table.Fields[0].Name != "name" || tableInfo.Table.Fields[1].Name != "age" {
		t.Fatalf("fields = %#v", tableInfo.Table.Fields)
	}

	records, err := plugin.SampleTable(context.Background(), strings.NewReader(tsvData), 1, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(records) != 1 || records[0]["name"] != "Bob" {
		t.Fatalf("records = %#v, want Bob", records)
	}
}

func TestTSVPluginRegisteredAsTableInfoAndSampleProviders(t *testing.T) {
	infoProvider, err := format.GetTableInfoProvider(format.FormatTSV)
	if err != nil {
		t.Fatalf("GetTableInfoProvider(tsv) failed: %v", err)
	}
	if infoProvider.Format() != format.FormatTSV {
		t.Fatalf("info provider format = %q, want tsv", infoProvider.Format())
	}
	sampleReader, err := format.GetTableSampleReader(format.FormatTSV)
	if err != nil {
		t.Fatalf("GetTableSampleReader(tsv) failed: %v", err)
	}
	if sampleReader.Format() != format.FormatTSV {
		t.Fatalf("sample reader format = %q, want tsv", sampleReader.Format())
	}
	writerProvider, err := format.GetTableWriterProvider(format.FormatTSV)
	if err != nil {
		t.Fatalf("GetTableWriterProvider(tsv) failed: %v", err)
	}
	if writerProvider.Format() != format.FormatTSV {
		t.Fatalf("writer provider format = %q, want tsv", writerProvider.Format())
	}
}

func TestTSVPlugin_OpenTableWriter(t *testing.T) {
	plugin := NewTSVPlugin(nil)
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{{Name: "id"}, {Name: "name"}},
	}
	var buf bytes.Buffer
	writer, err := plugin.OpenTableWriter(context.Background(), &buf, tableInfo, nil)
	if err != nil {
		t.Fatalf("OpenTableWriter failed: %v", err)
	}
	if err := writer.WriteRows(context.Background(), []map[string]interface{}{{"id": 1, "name": "Alice"}}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if got, want := buf.String(), "id\tname\n1\tAlice\n"; got != want {
		t.Fatalf("tsv output = %q, want %q", got, want)
	}
}

func TestCSVPlugin_DescribeTableCountsRecords(t *testing.T) {
	csvData := `id,name
1,Test
2,Sample
3,Demo`

	reader := strings.NewReader(csvData)
	plugin := NewPlugin(nil)

	tableInfo, err := plugin.DescribeTable(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}

	if tableInfo.Table.RowCount == nil || *tableInfo.Table.RowCount != 3 {
		t.Errorf("Expected 3 records, got %v", tableInfo.Table.RowCount)
	}
}

func TestCSVPlugin_DescribeTableBuildsSparseRowIndex(t *testing.T) {
	csvData := `id,name
1,A
2,B
3,C
4,D`

	reader := strings.NewReader(csvData)
	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.AccessIndexStep = 2

	tableInfo, err := plugin.DescribeTable(context.Background(), reader, opts)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}

	indexInfo := tableInfo.AccessIndex
	if indexInfo == nil {
		t.Fatalf("access index extension missing")
	}
	index := indexInfo
	if index.Kind != datatype.AccessIndexKindSparseRow {
		t.Fatalf("index kind = %q, want sparse row", index.Kind)
	}
	if index.RowCount != 4 {
		t.Fatalf("row count = %d, want 4", index.RowCount)
	}
	if len(index.Anchors) != 3 {
		t.Fatalf("anchors = %#v, want row 0/2/4 anchors", index.Anchors)
	}
	if index.Anchors[0].Row != 0 || index.Anchors[1].Row != 2 || index.Anchors[2].Row != 4 {
		t.Fatalf("anchor rows = %#v", index.Anchors)
	}
}

func TestCSVPlugin_SampleTableFromPositionedReader(t *testing.T) {
	csvData := "id,name\n1,A\n2,B\n3,C\n4,D\n"
	anchor := int64(strings.Index(csvData, "3,C"))
	if anchor <= 0 {
		t.Fatalf("test fixture anchor not found")
	}

	reader := strings.NewReader(csvData)
	if _, err := reader.Seek(anchor, io.SeekStart); err != nil {
		t.Fatalf("seek failed: %v", err)
	}

	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.TableSample = &format.TableSampleOptions{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "name", Type: datatype.FieldTypeString},
		},
		InputStartsAtRow:  2,
		InputIsPositioned: true,
	}

	records, err := plugin.SampleTable(context.Background(), reader, 3, 1, opts)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want 1 row", records)
	}
	if records[0]["name"] != "D" {
		t.Fatalf("name = %#v, want D", records[0]["name"])
	}
}
