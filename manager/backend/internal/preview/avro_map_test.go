package preview

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"os"
	"testing"
)

func TestAvroMapPreviewRetainsDynamicKeysAndValueDeclaration(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/synthetic-map.avro")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := format.GetTableInfoProvider(format.FormatAvro)
	sample, _ := format.GetTableSampleReader(format.FormatAvro)
	p := &FileTablePreviewProvider{}
	req := &PreviewRequest{Page: 2, PageSize: 20, Table: "map.avro"}
	result, err := p.previewStreamable(t.Context(), staticContentReader{content: data}, "", "map.avro", format.FormatAvro, info, sample, mustFileParseOptions(t, p, format.FormatAvro, req), req)
	if err != nil || result.Total == nil || *result.Total != 43 || len(result.Rows) != 20 || !result.HasMore {
		t.Fatalf("map page %#v %v", result, err)
	}
	if result.Fields[1].MapValue.Type != datatype.FieldTypeBigInt || result.Fields[3].MapValue.Enum.Name != "demo.MapState" {
		t.Fatal("map declaration lost")
	}
	public, err := datatype.RowsForJSON(result.Fields, result.Rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(public[0]["metrics"].(map[string]interface{})) != 0 {
		t.Fatal("empty map became NULL")
	}
	m := public[2]["metrics"].(map[string]interface{})
	if m[""] != "9007199254741015" || m["sensor.A"] != nil || m["中文 "] != "9007199254741018" {
		t.Fatal(m)
	}
}
