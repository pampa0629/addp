package excel

import (
	"bytes"
	"context"
	"testing"

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

	var cities *format.ContainerChildInfo
	for i := range info.Children {
		if info.Children[i].Name == "Cities" {
			cities = &info.Children[i]
			break
		}
	}
	if cities == nil {
		t.Fatalf("Cities child missing: %#v", info.Children)
	}
	if cities.Kind != "sheet" || cities.DataType != format.FormatDataTypeTable {
		t.Fatalf("Cities child = %#v", cities)
	}
	if cities.RowCount == nil {
		t.Fatalf("Cities RowCount is nil")
	}
	if cities.ColumnCount == nil || *cities.ColumnCount <= 0 {
		t.Fatalf("Cities ColumnCount = %#v, want positive column count", cities.ColumnCount)
	}
	if len(cities.Fields) < 2 || cities.Fields[0].Name != "id" {
		t.Fatalf("Cities Fields = %#v", cities.Fields)
	}
}
