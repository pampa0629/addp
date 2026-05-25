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
		Title:         "Report",
		Language:      "zh-CN",
		Encoding:      "utf-8",
		PageCount:     12,
		WordCount:     4096,
		SizeBytes:     &size,
		TextExtracted: true,
	})

	document := commonJSON.Section(attrs, "type_info.document")
	if document["title"] != "Report" || document["page_count"] != 12 || document["size_bytes"] != size {
		t.Fatalf("type_info.document = %#v", document)
	}
	if document["text_extracted"] != true {
		t.Fatalf("type_info.document.text_extracted = %#v", document["text_extracted"])
	}
}
