package format_test

import (
	"testing"

	"github.com/addp/common/datatype"
	. "github.com/addp/common/format"
)

func TestDescriptorHasContentReader(t *testing.T) {
	descriptor := FormatDescriptor{
		Format:         FormatType("reader_test"),
		DataType:       datatype.DataTypeDocument,
		ContentReaders: []string{" RAW_CONTENT ", "document_text"},
	}

	if !DescriptorHasContentReader(descriptor, ContentReaderRawContent) {
		t.Fatalf("DescriptorHasContentReader(raw_content) = false, want true")
	}
	if DescriptorHasContentReader(descriptor, ContentReaderRangeContent) {
		t.Fatalf("DescriptorHasContentReader(range_content) = true, want false")
	}
}

func TestSupportsContentReaderUsesDescriptorAndCapability(t *testing.T) {
	formatType := FormatType("reader_capability_test")
	if err := RegisterFormatDescriptor(FormatDescriptor{
		ID:             "reader-capability-test",
		Format:         formatType,
		DataType:       datatype.DataTypeDocument,
		Layouts:        []string{LayoutSingle},
		ContentReaders: []string{string(ContentReaderRawContent)},
	}); err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}

	if !SupportsContentReader(formatType, ContentReaderRawContent) {
		t.Fatalf("SupportsContentReader(raw_content) = false, want true")
	}
	if SupportsContentReader(formatType, ContentReaderRangeContent) {
		t.Fatalf("SupportsContentReader(range_content) = true, want false")
	}
}
