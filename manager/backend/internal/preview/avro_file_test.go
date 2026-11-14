package preview

import (
	"encoding/json"
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

func TestAvroNestedPreviewUsesRegisteredReaderAndPublicValues(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/synthetic-nested.avro")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := format.GetTableInfoProvider(format.FormatAvro)
	sample, _ := format.GetTableSampleReader(format.FormatAvro)
	p := &FileTablePreviewProvider{}
	req := &PreviewRequest{Page: 3, PageSize: 1, Table: "nested.avro"}
	result, err := p.previewStreamable(t.Context(), staticContentReader{content: data}, "", "nested.avro", format.FormatAvro, info, sample, mustFileParseOptions(t, p, format.FormatAvro, req), req)
	if err != nil || *result.Total != 43 || len(result.Fields) != 5 || !result.HasMore {
		t.Fatalf("nested preview %#v %v", result, err)
	}
	public, err := datatype.RowsForJSON(result.Fields, result.Rows)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &public); err != nil {
		t.Fatal(err)
	}
	detail := public[0]["detail"].(map[string]interface{})
	if public[0]["id"] != "9007199254740995" || detail["uid"] != "9007199254740995" || detail["stamp_ns"] != "1970-01-01T00:00:00.000000001Z" || detail["payload"] != "AAL/" {
		t.Fatalf("nested precision or binary %#v", public)
	}
	if public[0]["items"].([]interface{})[1] != nil || public[0]["grid"].([]interface{})[2] != nil {
		t.Fatal("nested null elements lost")
	}
}
