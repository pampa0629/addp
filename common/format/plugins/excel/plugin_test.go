package excel

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/xuri/excelize/v2"
)

func TestPluginDescribeContainer(t *testing.T) {
	t.Parallel()

	workbook := excelize.NewFile()
	defer workbook.Close()
	index, err := workbook.NewSheet("Cities")
	if err != nil {
		t.Fatalf("new sheet: %v", err)
	}
	workbook.SetActiveSheet(index)
	if err := workbook.SetSheetRow("Cities", "A1", &[]interface{}{"id", "name"}); err != nil {
		t.Fatalf("set header: %v", err)
	}
	if err := workbook.SetSheetRow("Cities", "A2", &[]interface{}{1, "Hangzhou"}); err != nil {
		t.Fatalf("set row: %v", err)
	}

	var buf bytes.Buffer
	if err := workbook.Write(&buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}

	opts := format.DefaultParseOptions()
	opts.SampleSize = 20
	opts.ExtraParams = map[string]interface{}{
		format.ContainerChildLimitParam: 10,
		format.ContainerRowLimitParam:   20,
	}

	info, err := NewPlugin(nil).DescribeContainer(context.Background(), bytes.NewReader(buf.Bytes()), opts)
	if err != nil {
		t.Fatalf("DescribeContainer() error = %v", err)
	}
	if info.ChildCount != 2 {
		t.Fatalf("ChildCount = %d, want 2", info.ChildCount)
	}
	if info.DefaultChild != "Cities" {
		t.Fatalf("DefaultChild = %q, want Cities", info.DefaultChild)
	}
	if len(info.Children) != 2 {
		t.Fatalf("len(Children) = %d, want 2", len(info.Children))
	}

	var cities *datatype.ContainerChildInfo
	for i := range info.Children {
		if info.Children[i].Name == "Cities" {
			cities = &info.Children[i]
			break
		}
	}
	if cities == nil {
		t.Fatalf("Cities child missing: %#v", info.Children)
	}
	if cities.ChildKind != "sheet" || cities.DataType != datatype.Table {
		t.Fatalf("Cities child = %#v", cities)
	}
	if cities.RowCount != nil {
		t.Fatalf("Cities RowCount = %#v, want nil for dimension-based estimate", cities.RowCount)
	}
	if cities.EstimatedRowCount == nil {
		t.Fatal("Cities EstimatedRowCount is nil")
	}
	if *cities.EstimatedRowCount != 1 {
		t.Fatalf("Cities EstimatedRowCount = %d, want 1", *cities.EstimatedRowCount)
	}
	if cities.ColumnCount == nil || *cities.ColumnCount <= 0 {
		t.Fatalf("Cities ColumnCount = %#v, want positive column count", cities.ColumnCount)
	}
	formatInfo, err := NewPlugin(nil).DescribeFormat(context.Background(), bytes.NewReader(buf.Bytes()), opts)
	if err != nil {
		t.Fatalf("DescribeFormat() error = %v", err)
	}
	if formatInfo["sheet_count"] != 2 {
		t.Fatalf("format_info.sheet_count = %#v, want 2", formatInfo["sheet_count"])
	}
	if formatInfo["default_sheet"] != "Cities" {
		t.Fatalf("format_info.default_sheet = %#v, want Cities", formatInfo["default_sheet"])
	}
}

func TestPluginDescribeTableWritesSheetFactsToNative(t *testing.T) {
	t.Parallel()

	workbook := excelize.NewFile()
	defer workbook.Close()
	index, err := workbook.NewSheet("Cities")
	if err != nil {
		t.Fatalf("new sheet: %v", err)
	}
	workbook.SetActiveSheet(index)
	if err := workbook.SetSheetRow("Cities", "A1", &[]interface{}{"id", "name"}); err != nil {
		t.Fatalf("set header: %v", err)
	}
	if err := workbook.SetSheetRow("Cities", "A2", &[]interface{}{1, "Hangzhou"}); err != nil {
		t.Fatalf("set row: %v", err)
	}

	var buf bytes.Buffer
	if err := workbook.Write(&buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}

	opts := format.DefaultParseOptions()
	opts.SheetName = "Cities"
	info, err := NewPlugin(nil).DescribeTable(context.Background(), bytes.NewReader(buf.Bytes()), opts)
	if err != nil {
		t.Fatalf("DescribeTable() error = %v", err)
	}
	if info.Table == nil {
		t.Fatalf("Table is nil")
	}
	if info.Table.RowCount != nil {
		t.Fatalf("RowCount = %#v, want nil for dimension-based estimate", info.Table.RowCount)
	}
	if info.Table.EstimatedRowCount == nil {
		t.Fatal("EstimatedRowCount is nil")
	}
	if *info.Table.EstimatedRowCount != 1 {
		t.Fatalf("EstimatedRowCount = %d, want 1", *info.Table.EstimatedRowCount)
	}
	if info.Table.Native["sheet_name"] != "Cities" {
		t.Fatalf("native.sheet_name = %#v, want Cities", info.Table.Native["sheet_name"])
	}
	if info.Table.Native["sheet_index"] != index {
		t.Fatalf("native.sheet_index = %#v, want %d", info.Table.Native["sheet_index"], index)
	}
	if len(info.Table.Fields) != 2 || info.Table.Fields[0].Name != "id" || info.Table.Fields[1].Name != "name" {
		t.Fatalf("fields = %#v, want id/name only", info.Table.Fields)
	}
	if info.FormatInfo["sheet_name"] != nil || info.FormatInfo["sheet_index"] != nil {
		t.Fatalf("format info should not contain table native facts: %#v", info.FormatInfo)
	}
	if info.FormatInfo["sheet_count"] != 2 {
		t.Fatalf("format_info.sheet_count = %#v, want 2", info.FormatInfo["sheet_count"])
	}
}

func TestExcelTableNativeFiltersUnknownKeys(t *testing.T) {
	t.Parallel()

	native := datatype.FilterTableNative(map[string]interface{}{
		"sheet_name": "Cities",
		"unknown":    "ignored",
	}, excelTableNativeKeys)
	if native["sheet_name"] != "Cities" {
		t.Fatalf("sheet_name = %#v, want Cities", native["sheet_name"])
	}
	if _, ok := native["unknown"]; ok {
		t.Fatalf("unknown native key should be filtered: %#v", native)
	}
}

func TestWorkbookReadsSelectedSheetSchemaAndEmptySheets(t *testing.T) {
	t.Parallel()
	workbook := excelize.NewFile()
	defer workbook.Close()
	if err := workbook.SetSheetName("Sheet1", "Cities"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Readings", "Empty", "HeaderOnly"} {
		if _, err := workbook.NewSheet(name); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		sheet, cell string
		values      []interface{}
	}{
		{"Cities", "A1", []interface{}{"city_id", "city_name"}},
		{"Cities", "A2", []interface{}{1, "Hangzhou"}},
		{"Cities", "A3", []interface{}{2, "Shanghai"}},
		{"Cities", "A4", []interface{}{3, "Beijing"}},
		{"Readings", "A1", []interface{}{"sensor", "temperature", "status"}},
		{"Readings", "A2", []interface{}{"Sensor-A", 18.5, "ready"}},
		{"HeaderOnly", "A1", []interface{}{"code", "label"}},
	} {
		if err := workbook.SetSheetRow(row.sheet, row.cell, &row.values); err != nil {
			t.Fatal(err)
		}
	}
	var data bytes.Buffer
	if err := workbook.Write(&data); err != nil {
		t.Fatal(err)
	}
	plugin := NewPlugin(nil)
	container, err := plugin.DescribeContainer(context.Background(), bytes.NewReader(data.Bytes()), format.DefaultParseOptions())
	if err != nil {
		t.Fatal(err)
	}
	if container.ChildCount != 4 || len(container.Children) != 4 {
		t.Fatalf("container = %#v, want all four sheets including empty ones", container)
	}
	for _, tc := range []struct {
		name    string
		fields  []string
		offset  int64
		wantRow map[string]interface{}
	}{
		{"Cities", []string{"city_id", "city_name"}, 1, map[string]interface{}{"city_id": "2", "city_name": "Shanghai"}},
		{"Readings", []string{"sensor", "temperature", "status"}, 0, map[string]interface{}{"sensor": "Sensor-A", "temperature": "18.5", "status": "ready"}},
		{"Empty", []string{}, 0, nil},
		{"HeaderOnly", []string{"code", "label"}, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := format.DefaultParseOptions()
			opts.SheetName = tc.name
			description, err := plugin.DescribeTable(context.Background(), bytes.NewReader(data.Bytes()), opts)
			if err != nil {
				t.Fatal(err)
			}
			fields := make([]string, len(description.Table.Fields))
			for i, field := range description.Table.Fields {
				fields[i] = field.Name
			}
			if description.Table.Name != tc.name || !reflect.DeepEqual(fields, tc.fields) {
				t.Fatalf("selected table = %s/%v, want %s/%v", description.Table.Name, fields, tc.name, tc.fields)
			}
			rows, err := plugin.SampleTable(context.Background(), bytes.NewReader(data.Bytes()), tc.offset, 1, opts)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantRow == nil {
				if len(rows) != 0 {
					t.Fatalf("empty sheet rows = %#v", rows)
				}
			} else if len(rows) != 1 || !reflect.DeepEqual(rows[0], tc.wantRow) {
				t.Fatalf("selected sheet rows = %#v, want %#v", rows, tc.wantRow)
			}
		})
	}
}
