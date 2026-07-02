package format_test

import (
	"testing"

	"github.com/addp/common/datatype"
	. "github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestListFormatCapabilitySnapshotsIncludesMarkdown(t *testing.T) {
	snapshots := ListFormatCapabilitySnapshots()
	if len(snapshots) == 0 {
		t.Fatal("expected builtin format capability snapshots")
	}

	for _, snapshot := range snapshots {
		if snapshot.Descriptor.Format != FormatMarkdown {
			continue
		}
		if snapshot.Descriptor.ID != "builtin-markdown" {
			t.Fatalf("descriptor ID = %q, want builtin-markdown", snapshot.Descriptor.ID)
		}
		if snapshot.Descriptor.DataType != datatype.Document {
			t.Fatalf("DataType = %q, want %q", snapshot.Descriptor.DataType, datatype.Document)
		}
		if !snapshot.Implementations.DocumentInfoProvider || !snapshot.Implementations.DocumentTextReader {
			t.Fatalf("implementations = %#v, want document info/text reader", snapshot.Implementations)
		}
		return
	}
	t.Fatal("markdown capability snapshot not found")
}

func TestGetFormatCapabilitySnapshot(t *testing.T) {
	snapshot, ok := GetFormatCapabilitySnapshot(FormatShapefile)
	if !ok {
		t.Fatal("expected shapefile capability snapshot")
	}
	if snapshot.Descriptor.DataType != datatype.Table {
		t.Fatalf("DataType = %q, want table", snapshot.Descriptor.DataType)
	}
	if !snapshot.Implementations.MultiTableInfoProvider || !snapshot.Implementations.MultiTableReader {
		t.Fatalf("implementations = %#v, want multi table implementations", snapshot.Implementations)
	}
	if snapshot.Implementations.AccessIndexProvider {
		t.Fatal("shapefile should not expose access index provider; .shx is native format indexing")
	}
}

func TestListFormatConflictDiagnosticsIsAvailable(t *testing.T) {
	diagnostics := ListFormatConflictDiagnostics()
	if diagnostics == nil {
		t.Fatal("ListFormatConflictDiagnostics should return an empty slice or diagnostics, not nil")
	}
}

func TestFormatCapabilitySnapshotSeparatesDescriptorAndImplementations(t *testing.T) {
	formatType := FormatType("discovery_descriptor_only_document")
	if err := RegisterFormatDescriptor(FormatDescriptor{
		ID:       "discovery-descriptor-only-document",
		Format:   formatType,
		DataType: datatype.Document,
		Layouts:  []string{LayoutSingle},
		Identification: FormatIdentification{
			Extensions: []string{".ddoc"},
			MimeTypes:  []string{"application/x-discovery-descriptor-only-document"},
		},
	}); err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}

	snapshot, ok := GetFormatCapabilitySnapshot(formatType)
	if !ok {
		t.Fatal("expected capability snapshot")
	}
	if snapshot.Descriptor.DataType != datatype.Document {
		t.Fatalf("descriptor = %#v, want document", snapshot.Descriptor)
	}
	if snapshot.Implementations.FormatPlugin || snapshot.Implementations.DocumentInfoProvider || snapshot.Implementations.DocumentTextReader {
		t.Fatalf("implementations = %#v, want none before plugin is registered", snapshot.Implementations)
	}
}

func TestFormatCapabilitySnapshotReportsAccessIndexProvider(t *testing.T) {
	snapshot, ok := GetFormatCapabilitySnapshot(FormatCSV)
	if !ok {
		t.Fatal("expected csv capability snapshot")
	}
	if !snapshot.Implementations.AccessIndexProvider {
		t.Fatalf("implementations = %#v, want access index provider", snapshot.Implementations)
	}
}

func TestFormatCapabilitySnapshotReportsTableWriterProvider(t *testing.T) {
	snapshot, ok := GetFormatCapabilitySnapshot(FormatCSV)
	if !ok {
		t.Fatal("expected csv capability snapshot")
	}
	if !snapshot.Implementations.TableWriterProvider {
		t.Fatalf("implementations = %#v, want table writer provider", snapshot.Implementations)
	}
}

func TestFormatCapabilitySnapshotReportsPLYDynamic3DProviders(t *testing.T) {
	snapshot, ok := GetFormatCapabilitySnapshot(FormatPLY)
	if !ok {
		t.Fatal("expected PLY capability snapshot")
	}
	if !snapshot.Implementations.Model3DInfoProvider ||
		!snapshot.Implementations.PointCloudInfoProvider ||
		!snapshot.Implementations.GaussianSplatInfoProvider {
		t.Fatalf("implementations = %#v, want model_3d, point_cloud and gaussian_splat providers", snapshot.Implementations)
	}
}

func TestFormatCapabilitySnapshotReportsKSplatGaussianSplatProvider(t *testing.T) {
	snapshot, ok := GetFormatCapabilitySnapshot(FormatKSplat)
	if !ok {
		t.Fatal("expected KSPLAT capability snapshot")
	}
	if !snapshot.Implementations.GaussianSplatInfoProvider {
		t.Fatalf("implementations = %#v, want gaussian_splat provider", snapshot.Implementations)
	}
}

func TestFormatCapabilitySnapshotReportsEPTScopePointCloudProvider(t *testing.T) {
	snapshot, ok := GetFormatCapabilitySnapshot(FormatEPT)
	if !ok {
		t.Fatal("expected EPT capability snapshot")
	}
	if !snapshot.Implementations.ScopePointCloudInfoProvider {
		t.Fatalf("implementations = %#v, want scope point cloud provider", snapshot.Implementations)
	}
}
