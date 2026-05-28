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
			if descriptor.TransferRead || descriptor.TransferWrite {
				t.Fatalf("%s should not claim transfer capability yet: read=%v write=%v", tt.formatType, descriptor.TransferRead, descriptor.TransferWrite)
			}
		})
	}
}

func TestFormatDescriptorMatchesCapabilityCoreFields(t *testing.T) {
	for _, descriptor := range ListFormatDescriptors() {
		capability, ok := GetFormatCapability(descriptor.Format)
		if !ok {
			t.Fatalf("descriptor %s has no matching capability", descriptor.Format)
		}
		assertCapabilityEqual(t, descriptor.Format, FormatCapabilityFromDescriptor(descriptor), capability)
	}
}

func TestFormatCapabilityFromDescriptorReturnsDetachedSlices(t *testing.T) {
	descriptor, ok := GetFormatDescriptor(FormatMarkdown)
	if !ok {
		t.Fatal("markdown descriptor not found")
	}

	capability := FormatCapabilityFromDescriptor(descriptor)
	capability.Extensions[0] = ".changed"
	capability.Layouts[0] = "changed"
	capability.ProviderHints[0] = "changed"
	capability.ContentReaders[0] = "changed"
	capability.EngineFamilies[0] = "changed"

	next := FormatCapabilityFromDescriptor(descriptor)
	if next.Extensions[0] == ".changed" || next.Layouts[0] == "changed" || next.ProviderHints[0] == "changed" || next.ContentReaders[0] == "changed" || next.EngineFamilies[0] == "changed" {
		t.Fatal("FormatCapabilityFromDescriptor returned mutable descriptor slices")
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

func TestRegisterFormatDescriptorUpdatesCapability(t *testing.T) {
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

	capability, ok := GetFormatCapability(formatType)
	if !ok {
		t.Fatal("registered descriptor did not update capability registry")
	}
	if capability.DataType != datatype.DataTypeDocument {
		t.Fatalf("capability data type = %q, want document", capability.DataType)
	}
	if !sameStrings(capability.Extensions, []string{".ptd"}) {
		t.Fatalf("capability extensions = %#v, want .ptd", capability.Extensions)
	}
	if !sameStrings(capability.ContentReaders, []string{string(ContentReaderDocumentText)}) {
		t.Fatalf("capability content readers = %#v, want document_text", capability.ContentReaders)
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

func assertCapabilityEqual(t *testing.T, formatType FormatType, left, right FormatCapability) {
	t.Helper()

	if left.Format != right.Format {
		t.Fatalf("%s format = %q, capability = %q", formatType, left.Format, right.Format)
	}
	if left.I18nKey != right.I18nKey {
		t.Fatalf("%s i18n key = %q, capability = %q", formatType, left.I18nKey, right.I18nKey)
	}
	if left.DataType != right.DataType {
		t.Fatalf("%s data type = %q, capability = %q", formatType, left.DataType, right.DataType)
	}
	if !sameStrings(left.Layouts, right.Layouts) {
		t.Fatalf("%s layouts = %#v, capability = %#v", formatType, left.Layouts, right.Layouts)
	}
	if !sameStrings(left.ProviderHints, right.ProviderHints) {
		t.Fatalf("%s provider hints = %#v, capability = %#v", formatType, left.ProviderHints, right.ProviderHints)
	}
	if !sameStrings(left.ContentReaders, right.ContentReaders) {
		t.Fatalf("%s content readers = %#v, capability = %#v", formatType, left.ContentReaders, right.ContentReaders)
	}
	if !sameStrings(left.Extensions, right.Extensions) {
		t.Fatalf("%s extensions = %#v, capability = %#v", formatType, left.Extensions, right.Extensions)
	}
	if left.Spatial != right.Spatial {
		t.Fatalf("%s spatial = %v, capability = %v", formatType, left.Spatial, right.Spatial)
	}
	if left.TransferRead != right.TransferRead {
		t.Fatalf("%s transfer read = %v, capability = %v", formatType, left.TransferRead, right.TransferRead)
	}
	if left.TransferWrite != right.TransferWrite {
		t.Fatalf("%s transfer write = %v, capability = %v", formatType, left.TransferWrite, right.TransferWrite)
	}
	if left.Parse != right.Parse {
		t.Fatalf("%s parse = %v, capability = %v", formatType, left.Parse, right.Parse)
	}
	if !sameStrings(left.EngineFamilies, right.EngineFamilies) {
		t.Fatalf("%s engine families = %#v, capability = %#v", formatType, left.EngineFamilies, right.EngineFamilies)
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
