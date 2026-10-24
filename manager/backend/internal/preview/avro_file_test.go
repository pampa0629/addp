package preview

import (
	"os"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestAvroFilePreviewUsesRegisteredReaderAndExactPages(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/apache-primitives.avro")
	if err != nil {
		t.Fatal(err)
	}
	info, err := format.GetTableInfoProvider(format.FormatAvro)
	if err != nil {
		t.Fatal(err)
	}
	sample, err := format.GetTableSampleReader(format.FormatAvro)
	if err != nil {
		t.Fatal(err)
	}
	p := &FileTablePreviewProvider{}
	for page := 1; page <= 3; page++ {
		req := &PreviewRequest{Page: page, PageSize: 1, Table: "synthetic.avro"}
		result, err := p.previewStreamable(t.Context(), staticContentReader{content: data}, "", "synthetic.avro", format.FormatAvro, info, sample, mustFileParseOptions(t, p, format.FormatAvro, req), req)
		if err != nil {
			t.Fatal(err)
		}
		if result.Total == nil || *result.Total != 3 || len(result.Rows) != 1 || result.HasMore != (page < 3) || len(result.Fields) != 3 || result.Fields[0].Type != datatype.FieldTypeBigInt || !result.Fields[1].Nullable {
			t.Fatalf("page %d: %#v", page, result)
		}
		if page == 2 && (result.Rows[0]["id"] != int64(-9007199254740993) || result.Rows[0]["label"] != nil) {
			t.Fatalf("precision/null lost: %#v", result.Rows)
		}
	}
}
