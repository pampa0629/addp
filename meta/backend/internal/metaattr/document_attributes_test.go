package metaattr

import (
	"testing"

	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
)

func TestDocumentInfoAttributesWritesTypeInfoDocument(t *testing.T) {
	t.Parallel()

	size := int64(2048)
	attrs := DocumentInfoAttributes(&datatype.DocumentInfo{
		Title:     "Report",
		Language:  "zh-CN",
		Encoding:  "utf-8",
		PageCount: 12,
		WordCount: 4096,
		SizeBytes: &size,
	})

	document := commonJSON.Section(attrs, "type_info.document")
	if document["title"] != "Report" || document["page_count"] != 12 || document["size_bytes"] != size {
		t.Fatalf("type_info.document = %#v", document)
	}
	if _, ok := document["text_extracted"]; ok {
		t.Fatalf("type_info.document must not carry extraction status: %#v", document)
	}
}

func TestDocumentInfoAttributesSkipsEmptyDocumentInfo(t *testing.T) {
	t.Parallel()

	attrs := DocumentInfoAttributes(&datatype.DocumentInfo{})

	if section := commonJSON.Section(attrs, "type_info.document"); len(section) != 0 {
		t.Fatalf("type_info.document = %#v, want absent", section)
	}
	if section := commonJSON.Section(attrs, "type_info"); len(section) != 0 {
		t.Fatalf("type_info = %#v, want absent", section)
	}
}
