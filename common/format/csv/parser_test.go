package csv

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func TestCSVParser_ParseTableInfo(t *testing.T) {
	csvData := `name,age,score,active
Alice,25,95.5,true
Bob,30,87.3,false
Charlie,28,92.1,true`

	reader := strings.NewReader(csvData)
	parser := NewParser(nil)

	tableInfo, err := parser.ParseTableInfo(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("ParseTableInfo failed: %v", err)
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

func TestCSVParser_ReadPreview(t *testing.T) {
	csvData := `id,name,value
1,Test,100
2,Sample,200`

	reader := strings.NewReader(csvData)
	parser := NewParser(nil)

	records, err := parser.ReadPreview(context.Background(), reader, 0, -1, nil)
	if err != nil {
		t.Fatalf("ReadPreview failed: %v", err)
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

func TestCSVParser_ParseTableInfoCountsRecords(t *testing.T) {
	csvData := `id,name
1,Test
2,Sample
3,Demo`

	reader := strings.NewReader(csvData)
	parser := NewParser(nil)

	tableInfo, err := parser.ParseTableInfo(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("ParseTableInfo failed: %v", err)
	}

	if tableInfo.RowCount == nil || *tableInfo.RowCount != 3 {
		t.Errorf("Expected 3 records, got %v", tableInfo.RowCount)
	}
}

func TestDetectDelimiter(t *testing.T) {
	tests := []struct {
		filename string
		expected rune
	}{
		{"data.csv", ','},
		{"data.tsv", '\t'},
		{"data.psv", '|'},
		{"data.txt", ','},
	}

	for _, tt := range tests {
		result := DetectDelimiter(tt.filename)
		if result != tt.expected {
			t.Errorf("DetectDelimiter(%s): expected %q, got %q", tt.filename, tt.expected, result)
		}
	}
}
