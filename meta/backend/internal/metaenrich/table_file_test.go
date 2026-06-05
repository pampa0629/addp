package metaenrich

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	parquetformat "github.com/addp/common/format/plugins/parquet"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaitem"
	parquetgo "github.com/parquet-go/parquet-go"
)

func TestTableFileResolverDetectsPartitionedWholeScope(t *testing.T) {
	t.Parallel()

	d := &tableFileItemResolver{}
	files := []metaitem.StorageFileRef{
		{Name: "part-000.parquet", Path: "dataset/dt=2026-05-05/part-000.parquet", Size: 10},
		{Name: "part-001.parquet", Path: "dataset/dt=2026-05-06/part-001.parquet", Size: 20},
	}
	subdirs := []metaitem.StorageDirectoryRef{
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
	if info.Layout != format.LayoutWhole {
		t.Fatalf("Layout = %q, want %q", info.Layout, format.LayoutWhole)
	}
	if info.ScopePath != "dataset" {
		t.Fatalf("ScopePath = %q, want dataset", info.ScopePath)
	}
	if len(info.RefFiles) != 2 {
		t.Fatalf("RefFiles len = %d, want 2", len(info.RefFiles))
	}
	if mode := commonJSON.String(info.Attributes, "format_info.parquet", "mode"); mode != "whole" {
		t.Fatalf("mode = %v, want whole", mode)
	}
}

func TestTableFileResolverWritesParquetPartRowCounts(t *testing.T) {
	reader := mapContentReader{content: map[string][]byte{
		"dataset/dt=2026-05-05/part-000.parquet": buildMetaitemParquetRows(t, testMetaitemParquetRow{ID: 1, Name: "Alice"}),
		"dataset/dt=2026-05-06/part-001.parquet": buildMetaitemParquetRows(t, testMetaitemParquetRow{ID: 2, Name: "Bob"}, testMetaitemParquetRow{ID: 3, Name: "Carol"}),
	}}
	files := []metaitem.StorageFileRef{
		{Name: "part-000.parquet", Path: "dataset/dt=2026-05-05/part-000.parquet", Size: 10},
		{Name: "part-001.parquet", Path: "dataset/dt=2026-05-06/part-001.parquet", Size: 20},
	}
	subdirs := []metaitem.StorageDirectoryRef{
		{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"},
		{Name: "dt=2026-05-06", Path: "dataset/dt=2026-05-06/"},
	}

	info, err := extractTableFileWholeScopeInfo(context.Background(), reader, nil, 1, "dataset", files, subdirs)
	if err != nil {
		t.Fatalf("extractTableFileWholeScopeInfo() error = %v", err)
	}
	if rowCount := commonJSON.Int64(info.Attributes, "type_info.table", "row_count"); rowCount != 3 {
		t.Fatalf("row_count = %d, want 3", rowCount)
	}
	counts := parquetformat.FileRowCountsFromAttributes(info.Attributes)
	if len(counts) != 2 {
		t.Fatalf("parquet file row counts = %#v, want two files", counts)
	}
	if counts["dataset/dt=2026-05-05/part-000.parquet"] != 1 || counts["dataset/dt=2026-05-06/part-001.parquet"] != 2 {
		t.Fatalf("parquet file row counts = %#v", counts)
	}
}

func TestTableFileResolverRulesDeclareSingleFileAndWholeScope(t *testing.T) {
	t.Parallel()

	d := &tableFileItemResolver{}
	rules := d.Rules()

	seen := map[string]bool{}
	for _, rule := range rules {
		if err := dataitem.ValidateFormatRule(rule); err != nil {
			t.Fatalf("ValidateFormatRule(%s/%s) error = %v", rule.Format, rule.Layout, err)
		}
		if rule.DataType != datatype.Table {
			t.Fatalf("rule = %#v, want table data type", rule)
		}
		seen[rule.Format+"/"+string(rule.Layout)] = true
		switch rule.Layout {
		case format.LayoutSingle:
			if !hasSingleTableProvider(rule.Format) {
				t.Fatalf("single rule = %#v, want single table provider", rule)
			}
		case format.LayoutWhole:
			if !hasScopeTableProvider(rule.Format) {
				t.Fatalf("whole scope rule = %#v, want scope table provider", rule)
			}
			if rule.WholeScope == nil || !rule.WholeScope.ExclusiveOnStrongHit {
				t.Fatalf("whole scope rule = %#v, want explicit exclusive rule", rule.WholeScope)
			}
		default:
			t.Fatalf("rule = %#v, want single or whole layout", rule)
		}
	}
	for _, key := range []string{"csv/single", "tsv/single", "parquet/single", "parquet/whole"} {
		if !seen[key] {
			t.Fatalf("rules = %#v, missing %s", rules, key)
		}
	}
}

func TestTableFileRuleCanReadUsesLayoutCapability(t *testing.T) {
	testFormat := format.FormatType("metaenrich_scope_only_test")
	if err := format.RegisterFormatPlugin(scopeOnlyTestFormatPlugin{formatType: testFormat}); err != nil {
		t.Fatalf("RegisterFormatPlugin() error = %v", err)
	}

	if hasSingleTableProvider(string(testFormat)) {
		t.Fatal("scope-only test format should not expose single table provider")
	}
	if !hasScopeTableProvider(string(testFormat)) {
		t.Fatal("scope-only test format should expose scope table provider")
	}
	if tableFileRuleCanRead(dataitem.FormatRule{
		Format:   string(testFormat),
		DataType: datatype.Table,
		Layout:   format.LayoutSingle,
	}) {
		t.Fatal("single rule should require single table provider")
	}
	if !tableFileRuleCanRead(dataitem.FormatRule{
		Format:   string(testFormat),
		DataType: datatype.Table,
		Layout:   format.LayoutWhole,
	}) {
		t.Fatal("whole scope rule should use scope table provider")
	}

	filtered := tableFiles([]metaitem.StorageFileRef{
		{Name: "dataset.scopeonlytable", Path: "dataset/dataset.scopeonlytable"},
	})
	if len(filtered) != 0 {
		t.Fatalf("tableFiles() = %#v, want scope-only format excluded from single-file candidates", filtered)
	}
}

func TestTableFilesUseProviderCapabilities(t *testing.T) {
	t.Parallel()

	files := []metaitem.StorageFileRef{
		{Name: "sales.csv", Path: "dataset/sales.csv"},
		{Name: "sales.tsv", Path: "dataset/sales.tsv"},
		{Name: "sales.json", Path: "dataset/sales.json"},
		{Name: "part-000.parquet", Path: "dataset/part-000.parquet"},
		{Name: "README.txt", Path: "dataset/README.txt"},
		{Name: "blob.unknown", Path: "dataset/blob.unknown"},
	}

	filtered := tableFiles(files)
	paths := map[string]bool{}
	for _, file := range filtered {
		paths[file.Path] = true
		if !hasSingleTableProvider(fileFormatName(file.Name)) {
			t.Fatalf("tableFiles included unreadable table file: %#v", file)
		}
	}

	for _, path := range []string{
		"dataset/sales.csv",
		"dataset/sales.tsv",
		"dataset/sales.json",
		"dataset/part-000.parquet",
	} {
		if !paths[path] {
			t.Fatalf("tableFiles = %#v, missing %s", filtered, path)
		}
	}
	for _, path := range []string{"dataset/README.txt", "dataset/blob.unknown"} {
		if paths[path] {
			t.Fatalf("tableFiles = %#v, should not include %s", filtered, path)
		}
	}
}

func TestDetectFormatOnlyCountsReadableTableFormats(t *testing.T) {
	t.Parallel()

	if got := detectFormat([]metaitem.StorageFileRef{
		{Name: "README.txt", Path: "dataset/README.txt"},
		{Name: "blob.unknown", Path: "dataset/blob.unknown"},
	}); got != string(format.FormatUnknown) {
		t.Fatalf("detectFormat() = %q, want unknown", got)
	}

	if got := detectFormat([]metaitem.StorageFileRef{
		{Name: "README.txt", Path: "dataset/README.txt"},
		{Name: "rows.json", Path: "dataset/rows.json"},
	}); got != string(format.FormatJSON) {
		t.Fatalf("detectFormat() = %q, want json", got)
	}
}

func TestTableFileResolverAllowsAuxiliaryFiles(t *testing.T) {
	t.Parallel()

	d := &tableFileItemResolver{}
	files := []metaitem.StorageFileRef{
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
	if len(info.RefFiles) != 1 || info.RefFiles[0] != "dataset/part-000.parquet" {
		t.Fatalf("RefFiles = %#v, want only parquet data file", info.RefFiles)
	}
	if info.SizeBytes == nil || *info.SizeBytes != 10 {
		t.Fatalf("SizeBytes = %v, want 10", info.SizeBytes)
	}
}

func TestTableFileResolverRejectsMixedWholeScope(t *testing.T) {
	t.Parallel()

	d := &tableFileItemResolver{}
	files := []metaitem.StorageFileRef{
		{Name: "part-000.parquet", Path: "dataset/dt=2026-05-05/part-000.parquet"},
		{Name: "README.txt", Path: "dataset/README.txt"},
	}
	subdirs := []metaitem.StorageDirectoryRef{{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"}}

	if d.Detect(context.Background(), files, subdirs) {
		t.Fatal("expected mixed whole scope to be rejected")
	}
}

func TestTableFileResolverResolvesWholeScopeFromRecursiveScope(t *testing.T) {
	t.Parallel()

	d := &tableFileItemResolver{}
	result, err := d.ResolveItems(context.Background(), metaitem.DirectoryResolveInput{
		DirPath: "dataset",
		Subdirs: []metaitem.StorageDirectoryRef{
			{Name: "dt=2026-05-05", Path: "dataset/dt=2026-05-05/"},
		},
		RecursiveFiles: []metaitem.StorageFileRef{
			{Name: "part-000.parquet", Path: "dataset/dt=2026-05-05/part-000.parquet", Size: 10},
		},
		RecursiveSubdirs: []metaitem.StorageDirectoryRef{
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
	if item.Layout != format.LayoutWhole {
		t.Fatalf("Layout = %q, want whole", item.Layout)
	}
	if item.ScopePath != "dataset" || item.PhysicalPath != "dataset" {
		t.Fatalf("paths = scope %q physical %q, want dataset", item.ScopePath, item.PhysicalPath)
	}
	if !result.Exclusive {
		t.Fatal("expected directory tree result to be exclusive")
	}
	if !result.Claims["dataset/dt=2026-05-05/part-000.parquet"] {
		t.Fatalf("claims = %#v, want recursive parquet ref", result.Claims)
	}
}

func TestTableFileResolverRejectsSiblingIndependentParquetFiles(t *testing.T) {
	t.Parallel()

	d := &tableFileItemResolver{}
	files := []metaitem.StorageFileRef{
		{Name: "sales.parquet", Path: "dataset/sales.parquet"},
		{Name: "customers.parquet", Path: "dataset/customers.parquet"},
	}

	if d.Detect(context.Background(), files, nil) {
		t.Fatal("expected independent sibling parquet files to remain single-file items")
	}
}

func TestExtractJSONSingleTableFileItemStrictRequiresRecordCollection(t *testing.T) {
	reader := staticContentReader{content: `{"name":"plain"}`}

	_, err := ExtractSingleTableFileItemStrict(context.Background(), reader, nil, 1, "plain.json", 10, false)
	if err == nil {
		t.Fatal("expected plain JSON document to be rejected as table")
	}
}

func TestEnrichSingleTableFileItemDetectsFormatFromContent(t *testing.T) {
	content := buildMetaitemParquetRows(t, testMetaitemParquetRow{ID: 1, Name: "Alice"})
	size := int64(len(content))
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Unknown,
			Format:             string(format.FormatUnknown),
			PrimaryContentPath: "lake3",
			SizeBytes:          &size,
		},
		PhysicalPath: "lake3",
		Attributes:   map[string]interface{}{},
	}

	enriched, ok, err := EnrichSingleTableFileItem(
		context.Background(),
		staticContentReader{content: string(content)},
		nil,
		1,
		item,
		"lake3",
		size,
		false,
		func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	)
	if err != nil {
		t.Fatalf("EnrichSingleTableFileItem() error = %v", err)
	}
	if !ok {
		t.Fatal("expected content-detected parquet file to be enriched")
	}
	if enriched.Format != string(format.FormatParquet) {
		t.Fatalf("Format = %q, want parquet", enriched.Format)
	}
	if enriched.DataType != datatype.Table {
		t.Fatalf("DataType = %q, want table", enriched.DataType)
	}
	if len(enriched.Fields) == 0 {
		t.Fatal("expected parquet fields")
	}
}

func TestExtractJSONSingleTableFileItemStrictAcceptsObjectArray(t *testing.T) {
	reader := staticContentReader{content: `[
		{"id":"1","name":"A","area":"356.16704388138885"},
		{"id":"2","name":"B","area":"129.1114944814742"}
	]`}

	info, err := ExtractSingleTableFileItemStrict(context.Background(), reader, nil, 1, "converted-data.json", 10, false)
	if err != nil {
		t.Fatalf("ExtractSingleTableFileItemStrict() error = %v", err)
	}
	if info.DataType != datatype.Table || info.Format != string(format.FormatJSON) {
		t.Fatalf("info = %#v", info)
	}
	table := commonJSON.Section(info.Attributes, "type_info.table")
	if commonJSON.InterfaceInt64(table["row_count"]) != 2 {
		t.Fatalf("table attrs = %#v", table)
	}
	if spatial := commonJSON.Section(info.Attributes, "capabilities.spatial"); len(spatial) != 0 {
		t.Fatalf("spatial should be empty: %#v", spatial)
	}
}

func TestExtractJSONSingleTableFileItemStrictWritesSpatialOnlyWhenGeometryExists(t *testing.T) {
	reader := staticContentReader{content: `{
		"type": "FeatureCollection",
		"bbox": [1, 2, 3, 4],
		"features": [
			{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"A"}}
		]
	}`}

	info, err := ExtractSingleTableFileItemStrict(context.Background(), reader, nil, 1, "roads.geojson", 10, false)
	if err != nil {
		t.Fatalf("ExtractSingleTableFileItemStrict() error = %v", err)
	}
	if info.DataType != datatype.Table || info.Format != string(format.FormatGeoJSON) {
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

func TestExtractJSONSingleTableFileItemStrictPromotesJSONSuffixGeoJSONContent(t *testing.T) {
	reader := staticContentReader{content: `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"A"}}
		]
	}`}

	info, err := ExtractSingleTableFileItemStrict(context.Background(), reader, nil, 1, "roads.json", 10, false)
	if err != nil {
		t.Fatalf("ExtractSingleTableFileItemStrict() error = %v", err)
	}
	if info.DataType != datatype.Table || info.Format != string(format.FormatGeoJSON) {
		t.Fatalf("info = %#v", info)
	}
	if geojsonInfo := commonJSON.Section(info.Attributes, "format_info.geojson"); geojsonInfo["structure"] != "geojson_feature_collection" {
		t.Fatalf("format_info.geojson = %#v", geojsonInfo)
	}
	if jsonInfo := commonJSON.Section(info.Attributes, "format_info.json"); len(jsonInfo) != 0 {
		t.Fatalf("format_info.json should be empty for promoted GeoJSON: %#v", jsonInfo)
	}
}

func TestExtractJSONSingleTableFileItemStrictDoesNotWriteSpatialWithoutGeometry(t *testing.T) {
	reader := staticContentReader{content: `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","properties":{"name":"A"}}
		]
	}`}

	info, err := ExtractSingleTableFileItemStrict(context.Background(), reader, nil, 1, "rows.geojson", 10, false)
	if err != nil {
		t.Fatalf("ExtractSingleTableFileItemStrict() error = %v", err)
	}
	if info.DataType != datatype.Table || info.Format != string(format.FormatGeoJSON) {
		t.Fatalf("info = %#v", info)
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

type mapContentReader struct {
	content map[string][]byte
}

func (r mapContentReader) Type() string         { return "map" }
func (r mapContentReader) DisplayName() string  { return "map" }
func (r mapContentReader) EngineOrigin() string { return "general" }
func (r mapContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r mapContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r mapContentReader) DefaultPort() int                                   { return 0 }
func (r mapContentReader) RequiredFields() []string                           { return nil }
func (r mapContentReader) SensitiveFields() []string                          { return nil }
func (r mapContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r mapContentReader) StoreSemantics() plugin.StoreSemantics { return plugin.StoreSemantics{} }
func (r mapContentReader) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	data, ok := r.content[path.StringPath()]
	if !ok {
		return nil, fmt.Errorf("content not found: %s", path.StringPath())
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

type scopeOnlyTestFormatPlugin struct {
	formatType format.FormatType
}

func (p scopeOnlyTestFormatPlugin) Format() format.FormatType {
	return p.formatType
}

func (p scopeOnlyTestFormatPlugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       string(p.formatType),
		Format:   p.formatType,
		DataType: datatype.Table,
		Layouts:  []string{format.LayoutWhole},
		Identification: format.FormatIdentification{
			Extensions: []string{".scopeonlytable"},
		},
	}
}

func (p scopeOnlyTestFormatPlugin) DescribeTableScope(context.Context, contentio.Reader, contentio.Ref, *format.ParseOptions) (*format.TableDescribeResult, error) {
	return &format.TableDescribeResult{
		Table: &datatype.TableInfo{
			Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeString}},
		},
	}, nil
}

type testMetaitemParquetRow struct {
	ID   int64  `parquet:"id"`
	Name string `parquet:"name"`
}

func buildMetaitemParquetRows(t *testing.T, rows ...testMetaitemParquetRow) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := parquetgo.NewGenericWriter[testMetaitemParquetRow](&buf)
	if _, err := writer.Write(rows); err != nil {
		t.Fatalf("write parquet rows: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close parquet writer: %v", err)
	}
	return buf.Bytes()
}
