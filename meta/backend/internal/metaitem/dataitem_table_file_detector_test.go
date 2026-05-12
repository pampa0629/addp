package metaitem

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/dataitem"
)

func TestTableFileDetectorDetectsPartitionedWholeScope(t *testing.T) {
	t.Parallel()

	d := &tableFileItemDetector{}
	files := []plugin.FileEntry{
		{Name: "part-000.parquet", Path: "dataset/dt=2026-05-05/part-000.parquet", Size: 10},
		{Name: "part-001.parquet", Path: "dataset/dt=2026-05-06/part-001.parquet", Size: 20},
	}
	subdirs := []plugin.DirEntry{
		{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"},
		{Name: "dt=2026-05-06", Path: "dataset/dt=2026-05-06/"},
	}

	if !d.Detect(context.Background(), files, subdirs) {
		t.Fatal("expected partitioned parquet directory to match")
	}
	info, err := extractTableFileWholeScopeInfo(context.Background(), nil, nil, 1, "dataset", files, subdirs)
	if err != nil {
		t.Fatalf("extractTableFileWholeScopeInfo() error = %v", err)
	}
	if info.Organization != dataitem.OrganizationWhole {
		t.Fatalf("Organization = %q, want %q", info.Organization, dataitem.OrganizationWhole)
	}
	if info.EntryPath != "dataset" {
		t.Fatalf("EntryPath = %q, want dataset", info.EntryPath)
	}
	if len(info.ComponentFiles) != 2 {
		t.Fatalf("ComponentFiles len = %d, want 2", len(info.ComponentFiles))
	}
	if mode := commonJSON.String(info.Attributes, "format_info.parquet", "mode"); mode != "whole" {
		t.Fatalf("mode = %v, want whole", mode)
	}
}

func TestTableFileDetectorRulesDeclareSingleFileAndWholeScope(t *testing.T) {
	t.Parallel()

	d := &tableFileItemDetector{}
	rules := d.Rules()
	if len(rules) != 2 {
		t.Fatalf("Rules len = %d, want 2", len(rules))
	}

	seenSingle := false
	seenWhole := false
	for _, rule := range rules {
		if err := dataitem.ValidateFormatRule(rule); err != nil {
			t.Fatalf("ValidateFormatRule(%s/%s) error = %v", rule.Format, rule.Organization, err)
		}
		switch rule.Organization {
		case dataitem.OrganizationSingle:
			seenSingle = true
		case dataitem.OrganizationWhole:
			seenWhole = true
			if rule.WholeScope == nil || !rule.WholeScope.ExclusiveOnStrongHit {
				t.Fatalf("whole scope rule = %#v, want explicit exclusive rule", rule.WholeScope)
			}
		}
	}
	if !seenSingle || !seenWhole {
		t.Fatalf("rules missing single=%v whole=%v", seenSingle, seenWhole)
	}
}

func TestTableFileDetectorAllowsAuxiliaryFiles(t *testing.T) {
	t.Parallel()

	d := &tableFileItemDetector{}
	files := []plugin.FileEntry{
		{Name: "part-000.parquet", Path: "dataset/part-000.parquet", Size: 10},
		{Name: "_SUCCESS", Path: "dataset/_SUCCESS", Size: 0},
		{Name: "._metadata.crc", Path: "dataset/._metadata.crc", Size: 4},
	}

	if !d.Detect(context.Background(), files, nil) {
		t.Fatal("expected parquet directory with auxiliary files to match")
	}
	info, err := extractTableFileWholeScopeInfo(context.Background(), nil, nil, 1, "dataset", files, nil)
	if err != nil {
		t.Fatalf("extractTableFileWholeScopeInfo() error = %v", err)
	}
	if len(info.ComponentFiles) != 1 || info.ComponentFiles[0] != "dataset/part-000.parquet" {
		t.Fatalf("ComponentFiles = %#v, want only parquet data file", info.ComponentFiles)
	}
	if info.SizeBytes == nil || *info.SizeBytes != 10 {
		t.Fatalf("SizeBytes = %v, want 10", info.SizeBytes)
	}
}

func TestTableFileDetectorRejectsMixedWholeScope(t *testing.T) {
	t.Parallel()

	d := &tableFileItemDetector{}
	files := []plugin.FileEntry{
		{Name: "part-000.parquet", Path: "dataset/dt=2026-05-05/part-000.parquet"},
		{Name: "README.txt", Path: "dataset/README.txt"},
	}
	subdirs := []plugin.DirEntry{{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"}}

	if d.Detect(context.Background(), files, subdirs) {
		t.Fatal("expected mixed whole scope to be rejected")
	}
}

func TestTableFileDetectorResolvesWholeScopeFromRecursiveScope(t *testing.T) {
	t.Parallel()

	d := &tableFileItemDetector{}
	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		DirPath: "dataset",
		Subdirs: []plugin.DirEntry{
			{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"},
		},
		RecursiveFiles: []plugin.FileEntry{
			{Name: "part-000.parquet", Path: "dataset/dt=2026-05-05/part-000.parquet", Size: 10},
		},
		RecursiveSubdirs: []plugin.DirEntry{
			{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if result == nil || len(result.Items) != 1 {
		t.Fatalf("ResolveItems() = %#v, want one item", result)
	}
	item := result.Items[0]
	if item.Organization != dataitem.OrganizationWhole {
		t.Fatalf("Organization = %q, want whole", item.Organization)
	}
	if item.EntryPath != "dataset" || item.PhysicalPath != "dataset" {
		t.Fatalf("paths = entry %q physical %q, want dataset", item.EntryPath, item.PhysicalPath)
	}
	if !result.Exclusive {
		t.Fatal("expected directory tree result to be exclusive")
	}
	if !result.Claims["dataset/dt=2026-05-05/part-000.parquet"] {
		t.Fatalf("claims = %#v, want recursive parquet component", result.Claims)
	}
}

func TestTableFileDetectorRejectsSiblingIndependentParquetFiles(t *testing.T) {
	t.Parallel()

	d := &tableFileItemDetector{}
	files := []plugin.FileEntry{
		{Name: "sales.parquet", Path: "dataset/sales.parquet"},
		{Name: "customers.parquet", Path: "dataset/customers.parquet"},
	}

	if d.Detect(context.Background(), files, nil) {
		t.Fatal("expected independent sibling parquet files to remain single-file items")
	}
}

func TestExtractJSONSingleFileInfoStrictRequiresRecordCollection(t *testing.T) {
	reader := staticContentReader{content: `{"name":"plain"}`}

	_, err := ExtractTableFileSingleFileInfoStrict(context.Background(), reader, nil, 1, "plain.json", 10, false)
	if err == nil {
		t.Fatal("expected plain JSON document to be rejected as table")
	}
}

func TestExtractJSONSingleFileInfoStrictWritesSpatialOnlyWhenGeometryExists(t *testing.T) {
	reader := staticContentReader{content: `{
		"type": "FeatureCollection",
		"bbox": [1, 2, 3, 4],
		"features": [
			{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"A"}}
		]
	}`}

	info, err := ExtractTableFileSingleFileInfoStrict(context.Background(), reader, nil, 1, "roads.geojson", 10, false)
	if err != nil {
		t.Fatalf("ExtractTableFileSingleFileInfoStrict() error = %v", err)
	}
	if info.DataType != dataitem.DataTypeTable || info.Format != string(format.FormatJSON) {
		t.Fatalf("info = %#v", info)
	}
	spatial := commonJSON.Section(info.Attributes, "capabilities.spatial")
	if spatial["primary_geometry_column"] != "geometry" {
		t.Fatalf("spatial = %#v", spatial)
	}
	if extent := commonJSON.InterfaceFloat64Slice(spatial["extent"]); len(extent) != 4 || extent[0] != 1 || extent[3] != 4 {
		t.Fatalf("extent = %#v", spatial["extent"])
	}
}

func TestExtractJSONSingleFileInfoStrictDoesNotWriteSpatialWithoutGeometry(t *testing.T) {
	reader := staticContentReader{content: `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","properties":{"name":"A"}}
		]
	}`}

	info, err := ExtractTableFileSingleFileInfoStrict(context.Background(), reader, nil, 1, "rows.geojson", 10, false)
	if err != nil {
		t.Fatalf("ExtractTableFileSingleFileInfoStrict() error = %v", err)
	}
	if spatial := commonJSON.Section(info.Attributes, "capabilities.spatial"); len(spatial) != 0 {
		t.Fatalf("spatial should be empty: %#v", spatial)
	}
}

type staticContentReader struct {
	content string
}

func (r staticContentReader) Type() string         { return "static" }
func (r staticContentReader) DisplayName() string  { return "static" }
func (r staticContentReader) EngineOrigin() string { return "general" }
func (r staticContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r staticContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r staticContentReader) DefaultPort() int                                   { return 0 }
func (r staticContentReader) RequiredFields() []string                           { return nil }
func (r staticContentReader) SensitiveFields() []string                          { return nil }
func (r staticContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r staticContentReader) StoreSemantics() plugin.StoreSemantics { return plugin.StoreSemantics{} }
func (r staticContentReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(r.content)), nil
}
