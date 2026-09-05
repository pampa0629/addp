package builtin

import (
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func TestDocumentFormatSnapshotsReflectBackendParsingBoundary(t *testing.T) {
	tests := []struct {
		formatType     format.FormatType
		wantInfo       bool
		wantTextReader bool
	}{
		{formatType: format.FormatPDF, wantInfo: true},
		{formatType: format.FormatDOC},
		{formatType: format.FormatDOCX, wantInfo: true, wantTextReader: true},
		{formatType: format.FormatRTF},
		{formatType: format.FormatPPTX, wantInfo: true, wantTextReader: true},
		{formatType: format.FormatXML, wantInfo: true, wantTextReader: true},
		{formatType: format.FormatWPS},
	}

	for _, tt := range tests {
		t.Run(string(tt.formatType), func(t *testing.T) {
			snapshot, ok := format.GetFormatCapabilitySnapshot(tt.formatType)
			if !ok {
				t.Fatalf("expected capability snapshot for %s", tt.formatType)
			}
			if !snapshot.Implementations.FormatPlugin {
				t.Fatalf("%s implementations = %#v, want format plugin", tt.formatType, snapshot.Implementations)
			}
			if snapshot.Implementations.DocumentInfoProvider != tt.wantInfo {
				t.Fatalf("%s document info provider = %v, want %v", tt.formatType, snapshot.Implementations.DocumentInfoProvider, tt.wantInfo)
			}
			if snapshot.Implementations.DocumentTextReader != tt.wantTextReader {
				t.Fatalf("%s document text reader = %v, want %v", tt.formatType, snapshot.Implementations.DocumentTextReader, tt.wantTextReader)
			}
		})
	}
}

func TestUnknownFormatSnapshotRegistersBinaryReader(t *testing.T) {
	reader, err := format.GetBinaryContentReader(format.FormatUnknown)
	if err != nil {
		t.Fatalf("GetBinaryContentReader(unknown) error = %v", err)
	}
	content, err := reader.ReadBinaryContent(t.Context(), strings.NewReader("abc"), 2, nil)
	if err != nil {
		t.Fatalf("ReadBinaryContent() error = %v", err)
	}
	if string(content.Bytes) != "ab" || !content.Truncated {
		t.Fatalf("content = %#v, want truncated ab", content)
	}

	snapshot, ok := format.GetFormatCapabilitySnapshot(format.FormatUnknown)
	if !ok {
		t.Fatal("expected unknown capability snapshot")
	}
	if !snapshot.Implementations.FormatPlugin {
		t.Fatalf("unknown implementations = %#v, want format plugin", snapshot.Implementations)
	}
	if !snapshot.Implementations.BinaryContentReader {
		t.Fatalf("unknown implementations = %#v, want binary content reader", snapshot.Implementations)
	}
}

func TestDescriptorOnlyTableFormatsExposeMissingProviders(t *testing.T) {
	for _, formatType := range []format.FormatType{format.FormatAvro, format.FormatORC} {
		t.Run(string(formatType), func(t *testing.T) {
			snapshot, ok := format.GetFormatCapabilitySnapshot(formatType)
			if !ok {
				t.Fatalf("expected capability snapshot for %s", formatType)
			}
			if !snapshot.Implementations.FormatPlugin {
				t.Fatalf("%s implementations = %#v, want descriptor plugin", formatType, snapshot.Implementations)
			}
			if snapshot.Implementations.TableInfoProvider ||
				snapshot.Implementations.TableSampleReader ||
				snapshot.Implementations.TableReaderProvider ||
				snapshot.Implementations.TableWriterProvider ||
				snapshot.Implementations.ScopeTableReader {
				t.Fatalf("%s should expose missing table providers until implemented: %#v", formatType, snapshot.Implementations)
			}
		})
	}
}

func TestMDBWeakIdentityBelongsOnlyToAccess(t *testing.T) {
	if detected := format.DetectFormat("source.mdb", nil); detected != format.FormatAccess {
		t.Fatalf("DetectFormat(source.mdb) = %q, want access", detected)
	}
	access, ok := format.GetFormatCapabilitySnapshot(format.FormatAccess)
	if !ok || !access.Implementations.RuntimeFormatDetector {
		t.Fatalf("access capability snapshot = %#v, want runtime format detector", access)
	}
	pgeo, ok := format.GetFormatCapabilitySnapshot(format.FormatPGeo)
	if !ok {
		t.Fatal("expected PGeo capability snapshot")
	}
	if len(pgeo.Descriptor.Identification.Extensions) != 0 || len(pgeo.Descriptor.Identification.MimeTypes) != 0 {
		t.Fatalf("PGeo weak identification = %#v, want none", pgeo.Descriptor.Identification)
	}
}

func TestPointCloudFormatsExposeExpectedProviders(t *testing.T) {
	tests := []struct {
		formatType format.FormatType
		wantInfo   bool
	}{
		{formatType: format.FormatLAS, wantInfo: true},
		{formatType: format.FormatLAZ, wantInfo: true},
		{formatType: format.FormatCOPC, wantInfo: true},
		{formatType: format.FormatE57, wantInfo: true},
		{formatType: format.FormatPCD, wantInfo: true},
		{formatType: format.FormatXYZ, wantInfo: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.formatType), func(t *testing.T) {
			snapshot, ok := format.GetFormatCapabilitySnapshot(tt.formatType)
			if !ok {
				t.Fatalf("expected capability snapshot for %s", tt.formatType)
			}
			if !snapshot.Implementations.FormatPlugin {
				t.Fatalf("%s implementations = %#v, want format plugin", tt.formatType, snapshot.Implementations)
			}
			if snapshot.Descriptor.DataType != "point_cloud" {
				t.Fatalf("%s data_type = %q, want point_cloud", tt.formatType, snapshot.Descriptor.DataType)
			}
			if snapshot.Implementations.PointCloudInfoProvider != tt.wantInfo {
				t.Fatalf("%s point cloud info provider = %v, want %v", tt.formatType, snapshot.Implementations.PointCloudInfoProvider, tt.wantInfo)
			}
		})
	}
}

func TestWholeScopePointCloudFormatsExposeScopeProviders(t *testing.T) {
	snapshot, ok := format.GetFormatCapabilitySnapshot(format.FormatEPT)
	if !ok {
		t.Fatal("expected capability snapshot for ept")
	}
	if !snapshot.Implementations.FormatPlugin {
		t.Fatalf("ept implementations = %#v, want format plugin", snapshot.Implementations)
	}
	if snapshot.Descriptor.DataType != "point_cloud" {
		t.Fatalf("ept data_type = %q, want point_cloud", snapshot.Descriptor.DataType)
	}
	if !snapshot.Implementations.ScopePointCloudInfoProvider {
		t.Fatalf("ept implementations = %#v, want scope point cloud provider", snapshot.Implementations)
	}
	if snapshot.Implementations.PointCloudInfoProvider {
		t.Fatalf("ept should not expose single-resource point cloud provider: %#v", snapshot.Implementations)
	}
}
