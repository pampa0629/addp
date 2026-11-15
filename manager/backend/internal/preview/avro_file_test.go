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

func TestAvroEnumPreviewPreservesDeclaredCandidates(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/synthetic-enum.avro")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := format.GetTableInfoProvider(format.FormatAvro)
	sample, _ := format.GetTableSampleReader(format.FormatAvro)
	p := &FileTablePreviewProvider{}
	req := &PreviewRequest{Page: 2, PageSize: 20, Table: "enum.avro"}
	result, err := p.previewStreamable(t.Context(), staticContentReader{content: data}, "", "enum.avro", format.FormatAvro, info, sample, mustFileParseOptions(t, p, format.FormatAvro, req), req)
	if err != nil || result.Total == nil || *result.Total != 43 || len(result.Rows) != 20 || !result.HasMore {
		t.Fatalf("enum preview %#v %v", result, err)
	}
	for _, f := range []datatype.FieldInfo{result.Fields[1], result.Fields[2], result.Fields[3].Fields[0], *result.Fields[4].Element} {
		if f.Enum == nil || f.Enum.Name != "demo.State" || len(f.Enum.Symbols) != 3 || f.Enum.Symbols[0] != "z" || f.Enum.Symbols[1] != "A" {
			t.Fatalf("enum field lost: %#v", f)
		}
	}
	public, err := datatype.RowsForJSON(result.Fields, result.Rows)
	if err != nil {
		t.Fatal(err)
	}
	if public[0]["id"] != "9007199254741013" || public[0]["state"] != "a" || public[1]["optional"] != nil || public[0]["detail"].(map[string]interface{})["state"] != "A" {
		t.Fatalf("enum values changed: %#v", public[:2])
	}
}
