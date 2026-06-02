package format_test

import (
	"sort"
	"testing"

	"github.com/addp/common/datatype"
	. "github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestListFormatDescriptorsIncludesBuiltinTextAndMarkdown(t *testing.T) {
	descriptors := ListFormatDescriptors()
	if len(descriptors) == 0 {
		t.Fatal("ListFormatDescriptors returned no descriptors")
	}

	seen := map[FormatType]bool{}
	for _, descriptor := range descriptors {
		seen[descriptor.Format] = true
	}
	for _, formatType := range []FormatType{FormatText, FormatMarkdown} {
		if !seen[formatType] {
			t.Fatalf("descriptor for %s not found", formatType)
		}
	}
}

func TestDescriptorsDeclareContentReadersForManagerHandledFormats(t *testing.T) {
	tests := []struct {
		formatType FormatType
		dataType   datatype.DataType
		reader     string
	}{
		{FormatPDF, datatype.DataTypeDocument, string(ContentReaderRawContent)},
		{FormatDOCX, datatype.DataTypeDocument, string(ContentReaderRawContent)},
		{FormatPPTX, datatype.DataTypeDocument, string(ContentReaderRawContent)},
		{FormatWPS, datatype.DataTypeDocument, string(ContentReaderRawContent)},
		{FormatJPEG, datatype.DataTypeMedia, string(ContentReaderRawContent)},
		{FormatPNG, datatype.DataTypeMedia, string(ContentReaderRawContent)},
		{FormatExcel, datatype.DataTypeContainer, string(ContentReaderTableSample)},
		{FormatSQLite, datatype.DataTypeContainer, string(ContentReaderTableSample)},
	}

	for _, tt := range tests {
		t.Run(string(tt.formatType), func(t *testing.T) {
			descriptor, ok := GetFormatDescriptor(tt.formatType)
			if !ok {
				t.Fatalf("descriptor for %s not found", tt.formatType)
			}
			if descriptor.DataType != tt.dataType {
				t.Fatalf("data type = %q, want %q", descriptor.DataType, tt.dataType)
			}
			if !containsStringForTest(descriptor.ContentReaders, tt.reader) {
				t.Fatalf("content readers = %#v, want %q", descriptor.ContentReaders, tt.reader)
			}
		})
	}
}

func TestGetFormatDescriptorReturnsDetachedContentReaders(t *testing.T) {
	descriptor, ok := GetFormatDescriptor(FormatCSV)
	if !ok {
		t.Fatal("csv descriptor not found")
	}
	if len(descriptor.ContentReaders) == 0 {
		t.Fatal("csv descriptor should declare content readers")
	}
	descriptor.ContentReaders[0] = "changed"

	next, ok := GetFormatDescriptor(FormatCSV)
	if !ok {
		t.Fatal("csv descriptor not found on second read")
	}
	if next.ContentReaders[0] == "changed" {
		t.Fatal("GetFormatDescriptor returned mutable content readers")
	}
}

func TestGetFormatDescriptorReturnsCopy(t *testing.T) {
	descriptor, ok := GetFormatDescriptor(FormatMarkdown)
	if !ok {
		t.Fatal("markdown descriptor not found")
	}
	descriptor.Identification.Extensions[0] = ".changed"

	next, ok := GetFormatDescriptor(FormatMarkdown)
	if !ok {
		t.Fatal("markdown descriptor not found on second read")
	}
	if next.Identification.Extensions[0] == ".changed" {
		t.Fatal("GetFormatDescriptor returned mutable internal descriptor")
	}
}

func TestRegisterFormatDescriptorStoresDescriptor(t *testing.T) {
	formatType := FormatType("plugin_test_doc")
	err := RegisterFormatDescriptor(FormatDescriptor{
		ID:       "plugin-test-doc",
		Version:  "v1",
		Priority: 10,
		Format:   formatType,
		DataType: datatype.DataTypeDocument,
		Layouts:  []string{LayoutSingle},
		Identification: FormatIdentification{
			Extensions: []string{".ptd"},
		},
		ContentReaders: []string{string(ContentReaderDocumentText)},
	})
	if err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}

	descriptor, ok := GetFormatDescriptor(formatType)
	if !ok {
		t.Fatal("registered descriptor not found")
	}
	if descriptor.ID != "plugin-test-doc" {
		t.Fatalf("descriptor ID = %q, want plugin-test-doc", descriptor.ID)
	}
	if descriptor.DataType != datatype.DataTypeDocument {
		t.Fatalf("descriptor data type = %q, want document", descriptor.DataType)
	}
	if !sameStrings(descriptor.Identification.Extensions, []string{".ptd"}) {
		t.Fatalf("descriptor extensions = %#v, want .ptd", descriptor.Identification.Extensions)
	}
	if !sameStrings(descriptor.ContentReaders, []string{string(ContentReaderDocumentText)}) {
		t.Fatalf("descriptor content readers = %#v, want document_text", descriptor.ContentReaders)
	}
}

func TestSupportsAccessIndexUsesDescriptorProviderCapability(t *testing.T) {
	if !SupportsAccessIndex(FormatCSV) {
		t.Fatalf("SupportsAccessIndex(csv) = false, want true")
	}
	if !SupportsAccessIndex(FormatTSV) {
		t.Fatalf("SupportsAccessIndex(tsv) = false, want true")
	}
	if !SupportsAccessIndex(FormatJSON) {
		t.Fatalf("SupportsAccessIndex(json) = false, want true")
	}
	if SupportsAccessIndex(FormatParquet) {
		t.Fatalf("SupportsAccessIndex(parquet) = true, want false")
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsStringForTest(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
