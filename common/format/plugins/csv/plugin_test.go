package csv

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/format"
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

	if len(tableInfo.Fields) != 4 {
		t.Errorf("Expected 4 fields, got %d", len(tableInfo.Fields))
	}

	// 检查字段类型推断
	expectedTypes := map[string]format.FieldType{
		"name":   format.FieldTypeString,
		"age":    format.FieldTypeInt,
		"score":  format.FieldTypeDouble, // CSV 浮点数默认为双精度
		"active": format.FieldTypeBool,
	}

	for _, field := range tableInfo.Fields {
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

func TestCSVPlugin_DescribeFormat(t *testing.T) {
	plugin := NewPlugin(nil)
	reader := strings.NewReader("id,name\n1,Alice\n")

	info, err := plugin.DescribeFormat(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("DescribeFormat failed: %v", err)
	}
	if info["delimiter"] != "," {
		t.Fatalf("delimiter = %#v, want comma", info["delimiter"])
	}
	if info["has_header"] != true {
		t.Fatalf("has_header = %#v, want true", info["has_header"])
	}
	if info["column_count"] != 2 {
		t.Fatalf("column_count = %#v, want 2", info["column_count"])
	}
}

func TestCSVPlugin_ImplementsTargetInterfaces(t *testing.T) {
	plugin := NewPlugin(nil)
	var _ format.FormatPlugin = plugin
	var _ format.FormatInfoProvider = plugin
	var _ format.TableInfoProvider = plugin
	var _ format.TableSampleReader = plugin
	if !format.SupportsContentIndex(plugin.Format()) {
		t.Fatalf("SupportsContentIndex(%q) = false, want true", plugin.Format())
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
	if len(tableInfo.Fields) != 2 || tableInfo.Fields[0].Name != "name" || tableInfo.Fields[1].Name != "age" {
		t.Fatalf("fields = %#v", tableInfo.Fields)
	}

	records, err := plugin.SampleTable(context.Background(), strings.NewReader(tsvData), 1, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(records) != 1 || records[0]["name"] != "Bob" {
		t.Fatalf("records = %#v, want Bob", records)
	}
}

func TestTSVPluginRegisteredAsTableProvider(t *testing.T) {
	provider, err := format.GetTableProvider(format.FormatTSV)
	if err != nil {
		t.Fatalf("GetTableProvider(tsv) failed: %v", err)
	}
	if provider.Format() != format.FormatTSV {
		t.Fatalf("provider format = %q, want tsv", provider.Format())
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

	if tableInfo.RowCount == nil || *tableInfo.RowCount != 3 {
		t.Errorf("Expected 3 records, got %v", tableInfo.RowCount)
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
	opts.ContentIndexStep = 2

	tableInfo, err := plugin.DescribeTable(context.Background(), reader, opts)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}

	indexInfo := tableInfo.GetContentIndexInfo()
	if indexInfo == nil || indexInfo.Table == nil {
		t.Fatalf("content index extension missing")
	}
	index := indexInfo.Table
	if index.Kind != format.ContentIndexKindSparseRow {
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
		Fields: []format.FieldInfo{
			{Name: "id", Type: format.FieldTypeInt},
			{Name: "name", Type: format.FieldTypeString},
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
