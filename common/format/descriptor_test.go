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

func TestRasterMosaicDescriptorUsesManifestFileName(t *testing.T) {
	descriptor, ok := GetFormatDescriptor(FormatRasterMosaic)
	if !ok {
		t.Fatal("descriptor for raster_mosaic not found")
	}
	if descriptor.DataType != datatype.Media {
		t.Fatalf("data_type = %s, want media", descriptor.DataType)
	}
	if !HasLayout(descriptor.Layouts, LayoutWhole) || HasLayout(descriptor.Layouts, LayoutSingle) {
		t.Fatalf("layouts = %#v, want only whole", descriptor.Layouts)
	}
	if len(descriptor.Identification.FileNames) != 1 || descriptor.Identification.FileNames[0] != "mosaic.addp.json" {
		t.Fatalf("file_names = %#v, want mosaic.addp.json", descriptor.Identification.FileNames)
	}
	if len(descriptor.Identification.Extensions) != 0 {
		t.Fatalf("extensions = %#v, want none to avoid JSON extension conflict", descriptor.Identification.Extensions)
	}
}

func TestPMTilesDescriptorUsesSingleFileIdentity(t *testing.T) {
	descriptor, ok := GetFormatDescriptor(FormatPMTiles)
	if !ok {
		t.Fatal("descriptor for pmtiles not found")
	}
	if descriptor.DataType != datatype.Media || !HasLayout(descriptor.Layouts, LayoutSingle) {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if len(descriptor.Identification.Extensions) != 1 || descriptor.Identification.Extensions[0] != ".pmtiles" {
		t.Fatalf("extensions = %#v", descriptor.Identification.Extensions)
	}
}

func TestDescriptorsKeepOnlyStaticFactsForBuiltinFormats(t *testing.T) {
	tests := []struct {
		formatType FormatType
		dataType   datatype.DataType
		layout     string
	}{
		{FormatPDF, datatype.Document, LayoutSingle},
		{FormatDOC, datatype.Document, LayoutSingle},
		{FormatDOCX, datatype.Document, LayoutSingle},
		{FormatRTF, datatype.Document, LayoutSingle},
		{FormatPPTX, datatype.Document, LayoutSingle},
		{FormatWPS, datatype.Document, LayoutSingle},
		{FormatJPEG, datatype.Media, LayoutSingle},
		{FormatPNG, datatype.Media, LayoutSingle},
		{FormatExcel, datatype.Container, LayoutSingle},
		{FormatSQLite, datatype.Container, LayoutSingle},
		{FormatOBJ, datatype.Model3D, LayoutSingle},
		{FormatSTL, datatype.Model3D, LayoutSingle},
		{FormatFBX, datatype.Model3D, LayoutSingle},
		{FormatIFC, datatype.Model3D, LayoutSingle},
		{FormatPLY, datatype.Model3D, LayoutSingle},
		{FormatSplat, datatype.GaussianSplat, LayoutSingle},
		{FormatKSplat, datatype.GaussianSplat, LayoutSingle},
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
			if !HasLayout(descriptor.Layouts, tt.layout) {
				t.Fatalf("layouts = %#v, want %q", descriptor.Layouts, tt.layout)
			}
		})
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
		DataType: datatype.Document,
		Layouts:  []string{LayoutSingle},
		Identification: FormatIdentification{
			Extensions: []string{".ptd"},
		},
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
	if descriptor.DataType != datatype.Document {
		t.Fatalf("descriptor data type = %q, want document", descriptor.DataType)
	}
	if !sameStrings(descriptor.Identification.Extensions, []string{".ptd"}) {
		t.Fatalf("descriptor extensions = %#v, want .ptd", descriptor.Identification.Extensions)
	}
}

func TestSupportsAccessIndexUsesDynamicProviderCapability(t *testing.T) {
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
