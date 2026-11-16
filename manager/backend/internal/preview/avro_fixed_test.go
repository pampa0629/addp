package preview

import (
	"encoding/base64"
	"encoding/json"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"os"
	"testing"
)

func TestAvroFixedPreviewRetainsExactBytesAndWidths(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/synthetic-fixed.avro")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := format.GetTableInfoProvider(format.FormatAvro)
	sample, _ := format.GetTableSampleReader(format.FormatAvro)
	p := &FileTablePreviewProvider{}
	req := &PreviewRequest{Page: 2, PageSize: 20, Table: "fixed.avro"}
	result, err := p.previewStreamable(t.Context(), staticContentReader{content: data}, "", "fixed.avro", format.FormatAvro, info, sample, mustFileParseOptions(t, p, format.FormatAvro, req), req)
	if err != nil || result.Total == nil || *result.Total != 43 || len(result.Rows) != 20 || !result.HasMore {
		t.Fatalf("fixed page: %#v %v", result, err)
	}
	if result.Fields[1].Fixed.Name != "demo.Token4" || result.Fields[6].Fixed.Name != "other.Token4" || result.Fields[3].Fields[0].Fixed.Size != 16 {
		t.Fatal("fixed declaration lost")
	}
	public, err := datatype.RowsForJSON(result.Fields, result.Rows)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for n, row := range rows {
		for name, size := range map[string]int{"token": 4, "other": 4, "single": 1, "wide": 32} {
			v, err := base64.StdEncoding.DecodeString(row[name].(string))
			original := result.Rows[n][name].([]byte)
			if err != nil || len(v) != size || string(v) != string(original) {
				t.Fatalf("fixed bytes changed: %s", name)
			}
		}
	}
	if rows[1]["optional"] != nil || rows[14]["token"] != "AAAAAA==" {
		t.Fatal("NULL or all-zero bytes changed")
	}
}
