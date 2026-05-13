package service

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/models"
)

func TestFileTablePreviewProviderResolveFormatUsesMetaFormat(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Table: "fallback.bin",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format": "xlsx",
			},
		},
	}

	got := provider.resolveFormat(req)
	if got != format.FormatExcel {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatExcel)
	}
}

func TestFileTablePreviewProviderResolveFormatUsesContentType(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Table: "fallback.bin",
		Attributes: map[string]interface{}{
			"storage": map[string]interface{}{
				"content_type": "application/geo+json",
			},
		},
	}

	got := provider.resolveFormat(req)
	if got != format.FormatJSON {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatJSON)
	}
}

func TestFileTablePreviewProviderResolveFormatDoesNotFallbackToFilename(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Table: "roads.geojson",
	}

	got := provider.resolveFormat(req)
	if got != format.FormatUnknown {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatUnknown)
	}
}

func TestFileTablePreviewProviderBuildParseOptionsUsesResolvedFormat(t *testing.T) {
	provider := &FileTablePreviewProvider{}

	if got := provider.buildParseOptions(format.FormatTSV).Delimiter; got != 0 {
		t.Fatalf("delimiter = %q, want zero value so format plugin owns delimiter semantics", got)
	}
}

func TestFileTablePreviewProviderBuildParseOptionsUsesExcelChildSheet(t *testing.T) {
	provider := &FileTablePreviewProvider{}

	opts := provider.buildParseOptions(format.FormatExcel, &PreviewRequest{ChildName: " Cities "})
	if opts.SheetName != "Cities" {
		t.Fatalf("SheetName = %q, want Cities", opts.SheetName)
	}
}

func TestFileTablePreviewProviderResourceContextUsesFileSystemReader(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	enginePlugin := &recordingContentPlugin{engineType: "nfs"}
	plugin.Register(enginePlugin)
	defer func() {
		if previousErr == nil {
			plugin.Register(previous)
			return
		}
		plugin.Unregister(enginePlugin.Type())
	}()

	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Engine:       &models.Engine{EngineType: enginePlugin.Type(), ID: 7},
		Schema:       "gis-data",
		Table:        "sample.csv",
		PhysicalPath: "/gis-data/sample.csv",
	}

	resourceCtx, err := provider.resourceContextForPreview(req)
	if err != nil {
		t.Fatalf("resourceContextForPreview() error = %v", err)
	}
	if _, ok := resourceCtx.reader.(*fileSystemResourceReader); !ok {
		t.Fatalf("reader = %T, want *fileSystemResourceReader", resourceCtx.reader)
	}
	if resourceCtx.path != "gis-data/sample.csv" {
		t.Fatalf("path = %q, want gis-data/sample.csv", resourceCtx.path)
	}

	rc, err := resourceCtx.reader.Open(context.Background(), resource.NewResourceRef(resourceCtx.path, resource.ResourceRoleMain))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	rc.Close()
	if got := enginePlugin.openedPath.StringPath(); got != "gis-data/sample.csv" {
		t.Fatalf("opened path = %q, want gis-data/sample.csv", got)
	}
}

func TestObjectStorageResourceReaderStripsBucketPrefixFromComponentPath(t *testing.T) {
	t.Parallel()

	enginePlugin := &recordingContentPlugin{engineType: "minio-preview-component"}
	reader := newObjectStorageResourceReader(enginePlugin, nil, nil, 9, "addp")

	rc, err := reader.Open(context.Background(), resource.NewResourceRef("addp/gis/规划用地.dbf", resource.ResourceRoleComponent))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	rc.Close()
	if got := enginePlugin.openedPath.StringPath(); got != "addp/gis/规划用地.dbf" {
		t.Fatalf("opened path = %q, want catalog path with bucket plus object key", got)
	}
	if len(enginePlugin.openedPath.Segments) != 3 {
		t.Fatalf("segments = %#v, want bucket + prefix + object", enginePlugin.openedPath.Segments)
	}
	objectSegment := enginePlugin.openedPath.Segments[2]
	if objectSegment.Name != "规划用地.dbf" {
		t.Fatalf("object segment = %#v, want key without duplicated bucket", objectSegment)
	}
}

func TestFileTablePreviewProviderPreviewTSVWithRegisteredProvider(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Page:     1,
		PageSize: 1,
		Table:    "manager/test.tsv",
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"row_count": int64(2),
					"fields": []interface{}{
						map[string]interface{}{"name": "name", "type": string(format.FieldTypeString), "nullable": true},
						map[string]interface{}{"name": "age", "type": string(format.FieldTypeInt), "nullable": true},
					},
				},
			},
		},
	}
	tableProvider, err := format.GetTableProvider(format.FormatTSV)
	if err != nil {
		t.Fatalf("GetTableProvider(tsv) failed: %v", err)
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticResourceReader{content: []byte("name\tage\nAlice\t25\nBob\t30\n")},
		"manager",
		"manager/test.tsv",
		format.FormatTSV,
		tableProvider,
		provider.buildParseOptions(format.FormatTSV),
		req,
	)
	if err != nil {
		t.Fatalf("previewStreamable() error = %v", err)
	}
	if preview.Rows[0]["name"] != "Alice" {
		t.Fatalf("rows = %#v, want Alice", preview.Rows)
	}
}

func TestFileTablePreviewProviderPreviewStreamableReturnsTableModeAndFirstPage(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	tableProvider := &recordingTableProvider{}
	req := &PreviewRequest{
		Page:     1,
		PageSize: 2,
		Table:    "manager/test.parquet",
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticResourceReader{content: []byte("mock")},
		"manager",
		"manager/test.parquet",
		format.FormatParquet,
		tableProvider,
		nil,
		req,
	)
	if err != nil {
		t.Fatalf("previewStreamable() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("Mode = %q, want %q", preview.Mode, PreviewModeTable)
	}
	if tableProvider.sampleOffset != 0 {
		t.Fatalf("sample offset = %d, want 0 for first page", tableProvider.sampleOffset)
	}
	if preview.Page != 1 {
		t.Fatalf("Page = %d, want 1", preview.Page)
	}
	if len(preview.Rows) != 1 || preview.Rows[0]["name"] != "first" {
		t.Fatalf("Rows = %#v, want first row", preview.Rows)
	}
}

func TestFileTablePreviewProviderUsesAttributesTableInfo(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	tableProvider := &recordingTableProvider{}
	req := &PreviewRequest{
		Page:     2,
		PageSize: 2,
		Table:    "manager/test.csv",
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"row_count": int64(5),
					"fields": []interface{}{
						map[string]interface{}{"name": "name", "type": string(format.FieldTypeString), "nullable": true},
					},
				},
			},
		},
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticResourceReader{content: []byte("mock")},
		"manager",
		"manager/test.csv",
		format.FormatCSV,
		tableProvider,
		nil,
		req,
	)
	if err != nil {
		t.Fatalf("previewStreamable() error = %v", err)
	}
	if tableProvider.describeCalls != 0 {
		t.Fatalf("DescribeTable calls = %d, want 0", tableProvider.describeCalls)
	}
	if preview.Total != 5 {
		t.Fatalf("Total = %d, want 5", preview.Total)
	}
	if tableProvider.sampleOffset != 2 {
		t.Fatalf("sample offset = %d, want second page offset 2", tableProvider.sampleOffset)
	}
}

func TestFileTablePreviewProviderUsesExcelChildAttributes(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	tableProvider := &recordingTableProvider{}
	req := &PreviewRequest{
		Page:      1,
		PageSize:  2,
		Table:     "manager/book.xlsx",
		ChildName: "Cities",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format": "excel",
			},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{
							"name":       "Cities",
							"kind":       "sheet",
							"row_count":  int64(7),
							"has_header": true,
							"fields": []interface{}{
								map[string]interface{}{"name": "city", "type": string(format.FieldTypeString), "nullable": true},
								map[string]interface{}{"name": "population", "type": string(format.FieldTypeInt), "nullable": true},
							},
						},
					},
				},
			},
		},
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticResourceReader{content: []byte("mock")},
		"manager",
		"manager/book.xlsx",
		format.FormatExcel,
		tableProvider,
		provider.buildParseOptions(format.FormatExcel, req),
		req,
	)
	if err != nil {
		t.Fatalf("previewStreamable() error = %v", err)
	}
	if tableProvider.describeCalls != 0 {
		t.Fatalf("DescribeTable calls = %d, want 0 when child attributes have table info", tableProvider.describeCalls)
	}
	if preview.Total != 7 {
		t.Fatalf("Total = %d, want 7", preview.Total)
	}
	if len(preview.Columns) != 2 || preview.Columns[0] != "city" || preview.Columns[1] != "population" {
		t.Fatalf("Columns = %#v, want child sheet columns", preview.Columns)
	}
}

func TestFileTablePreviewProviderRestoresSpatialInfoFromAttributes(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	tableProvider := &recordingTableProvider{}
	req := &PreviewRequest{
		Page:     1,
		PageSize: 2,
		Table:    "manager/roads.json",
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"row_count": int64(1),
					"fields": []interface{}{
						map[string]interface{}{"name": "geometry", "type": string(format.FieldTypeGeometry), "nullable": false},
						map[string]interface{}{"name": "name", "type": string(format.FieldTypeString), "nullable": true},
					},
				},
			},
			"capabilities": map[string]interface{}{
				"spatial": map[string]interface{}{
					"primary_geometry_column": "geometry",
					"geometry_columns": []interface{}{
						map[string]interface{}{"name": "geometry", "geometry_type": "Point", "srid": int64(4326)},
					},
					"extent": []interface{}{1.0, 2.0, 3.0, 4.0},
				},
			},
		},
	}

	preview, err := provider.previewStreamable(
		context.Background(),
		staticResourceReader{content: []byte("mock")},
		"manager",
		"manager/roads.json",
		format.FormatJSON,
		tableProvider,
		nil,
		req,
	)
	if err != nil {
		t.Fatalf("previewStreamable() error = %v", err)
	}
	if tableProvider.describeCalls != 0 {
		t.Fatalf("DescribeTable calls = %d, want 0", tableProvider.describeCalls)
	}
	if preview.SRID != 4326 {
		t.Fatalf("SRID = %d, want 4326", preview.SRID)
	}
	if len(preview.GeometryColumns) != 1 || preview.GeometryColumns[0] != "geometry" {
		t.Fatalf("GeometryColumns = %#v", preview.GeometryColumns)
	}
}

func TestFileTablePreviewProviderPreviewShapefileReturnsTableModeAndFirstPage(t *testing.T) {
	t.Parallel()

	provider := &FileTablePreviewProvider{}
	componentProvider := &recordingComponentTableProvider{}
	req := &PreviewRequest{
		Page:     1,
		PageSize: 2,
		Table:    "gis/roads.shp",
	}

	preview, err := provider.previewComponents(
		context.Background(),
		emptyComponentReader{},
		"bucket",
		format.FormatShapefile,
		componentProvider,
		nil,
		req,
	)
	if err != nil {
		t.Fatalf("previewComponents() error = %v", err)
	}
	if preview.Mode != PreviewModeTable {
		t.Fatalf("Mode = %q, want %q", preview.Mode, PreviewModeTable)
	}
	if componentProvider.sampleOffset != 0 {
		t.Fatalf("sample offset = %d, want 0 for first page", componentProvider.sampleOffset)
	}
	if preview.Page != 1 {
		t.Fatalf("Page = %d, want 1", preview.Page)
	}
}

func TestFileTablePreviewProviderPreviewComponentsUsesAttributesTableInfo(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	componentProvider := &recordingComponentTableProvider{}
	req := &PreviewRequest{
		Page:     2,
		PageSize: 3,
		Table:    "gis/roads.shp",
		Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"table": map[string]interface{}{
					"row_count": float64(9),
					"fields": []interface{}{
						map[string]interface{}{"name": "name", "type": "string", "nullable": true},
					},
				},
			},
		},
	}

	preview, err := provider.previewComponents(
		context.Background(),
		emptyComponentReader{},
		"bucket",
		format.FormatShapefile,
		componentProvider,
		nil,
		req,
	)
	if err != nil {
		t.Fatalf("previewComponents() error = %v", err)
	}
	if componentProvider.describeCalls != 0 {
		t.Fatalf("DescribeTableComponents calls = %d, want 0 when attributes have table info", componentProvider.describeCalls)
	}
	if componentProvider.sampleOffset != 3 {
		t.Fatalf("sample offset = %d, want 3", componentProvider.sampleOffset)
	}
	if preview.Total != 9 {
		t.Fatalf("Total = %d, want 9", preview.Total)
	}
}

func TestComponentReaderForPreviewUsesMetaComponentFiles(t *testing.T) {
	reader := componentReaderForPreview(staticResourceReader{}, "bucket/roads/roads.shp", format.FormatShapefile, map[string]interface{}{
		"item": map[string]interface{}{
			"component_files": []interface{}{
				"bucket/roads/roads.dbf",
				"bucket/roads/roads.prj",
				"bucket/roads/roads.shp",
				"bucket/roads/roads.shx",
			},
		},
	})

	components := reader.Components()
	got := make(map[string]resource.ComponentRef, len(components))
	for _, component := range components {
		got[component.ComponentRole] = component
	}

	for _, role := range []string{"main", "index", "attributes", "projection"} {
		if _, ok := got[role]; !ok {
			t.Fatalf("missing component role %q in %#v", role, components)
		}
	}
	if !got["main"].Required || !got["index"].Required || !got["attributes"].Required {
		t.Fatalf("required flags = main:%v index:%v attributes:%v", got["main"].Required, got["index"].Required, got["attributes"].Required)
	}
	if got["projection"].Required {
		t.Fatalf("projection component should be optional")
	}
	if got["main"].Path != "bucket/roads/roads.shp" {
		t.Fatalf("main path = %q", got["main"].Path)
	}
}

func TestComponentReaderForPreviewFallsBackToSameBasenameComponents(t *testing.T) {
	reader := componentReaderForPreview(staticResourceReader{}, "bucket/roads/roads.shp", format.FormatShapefile, nil)
	components := reader.Components()
	required := map[string]bool{}
	for _, component := range components {
		if component.Required {
			required[component.Path] = true
		}
	}
	want := map[string]bool{
		"bucket/roads/roads.shp": true,
		"bucket/roads/roads.shx": true,
		"bucket/roads/roads.dbf": true,
	}
	if !reflect.DeepEqual(required, want) {
		t.Fatalf("required fallback components = %#v, want %#v", required, want)
	}
}

type staticResourceReader struct {
	content []byte
}

func (r staticResourceReader) Open(context.Context, resource.ResourceRef) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.content)), nil
}

func (r staticResourceReader) Stat(context.Context, resource.ResourceRef) (*resource.ResourceMetadata, error) {
	return nil, nil
}

func (r staticResourceReader) List(context.Context, resource.ResourceRef) ([]resource.ResourceRef, error) {
	return nil, nil
}

type recordingTableProvider struct {
	sampleOffset  int64
	describeCalls int
}

func (p *recordingTableProvider) Format() format.FormatType {
	return format.FormatParquet
}

func (p *recordingTableProvider) Capabilities() format.FormatCapability {
	return format.FormatCapability{}
}

func (p *recordingTableProvider) DescribeTable(context.Context, io.Reader, *format.ParseOptions) (*format.TableInfo, error) {
	p.describeCalls++
	rowCount := int64(1)
	return &format.TableInfo{
		Fields:   []format.FieldInfo{{Name: "name", Type: format.FieldTypeString}},
		RowCount: &rowCount,
	}, nil
}

func (p *recordingTableProvider) SampleTable(_ context.Context, _ io.Reader, offset, _ int64, _ *format.ParseOptions) ([]map[string]interface{}, error) {
	p.sampleOffset = offset
	return []map[string]interface{}{{"name": "first"}}, nil
}

type recordingComponentTableProvider struct {
	recordingTableProvider
	sampleOffset  int64
	describeCalls int
}

func (p *recordingComponentTableProvider) Format() format.FormatType {
	return format.FormatShapefile
}

func (p *recordingComponentTableProvider) DescribeTableComponents(context.Context, resource.ComponentReader, *format.ParseOptions) (*format.TableInfo, error) {
	p.describeCalls++
	rowCount := int64(1)
	return &format.TableInfo{
		Fields:   []format.FieldInfo{{Name: "name", Type: format.FieldTypeString}},
		RowCount: &rowCount,
	}, nil
}

func (p *recordingComponentTableProvider) SampleTableComponents(_ context.Context, _ resource.ComponentReader, offset, _ int64, _ *format.ParseOptions) ([]map[string]interface{}, error) {
	p.sampleOffset = offset
	return []map[string]interface{}{{"name": "first"}}, nil
}

type emptyComponentReader struct{}

func (emptyComponentReader) Components() []resource.ComponentRef {
	return nil
}

func (emptyComponentReader) OpenComponent(context.Context, resource.ComponentRef) (io.ReadCloser, error) {
	return nil, resource.ErrResourceNotFound
}

func (emptyComponentReader) OpenComponentRole(context.Context, string) (io.ReadCloser, error) {
	return nil, resource.ErrResourceNotFound
}

var _ format.TableProvider = (*recordingTableProvider)(nil)
var _ format.ComponentTableProvider = (*recordingComponentTableProvider)(nil)
var _ resource.ResourceReader = staticResourceReader{}
var _ resource.ComponentReader = emptyComponentReader{}

type recordingContentPlugin struct {
	engineType string
	openedPath plugin.CatalogPath
}

func (p *recordingContentPlugin) Type() string         { return p.engineType }
func (p *recordingContentPlugin) DisplayName() string  { return p.engineType }
func (p *recordingContentPlugin) EngineOrigin() string { return "general" }
func (p *recordingContentPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingContentPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingContentPlugin) DefaultPort() int          { return 0 }
func (p *recordingContentPlugin) RequiredFields() []string  { return nil }
func (p *recordingContentPlugin) SensitiveFields() []string { return nil }
func (p *recordingContentPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p *recordingContentPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p *recordingContentPlugin) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	p.openedPath = path
	return io.NopCloser(strings.NewReader("name\nAlice\n")), nil
}

func TestContentIndexObjectKeyIncludesBucketForObjectStorage(t *testing.T) {
	req := &PreviewRequest{Engine: &models.Engine{EngineType: "minio"}}

	if got := contentIndexObjectKey(req, "bucket", "dir/sample.csv"); got != "bucket/dir/sample.csv" {
		t.Fatalf("object key = %q, want bucket/dir/sample.csv", got)
	}
	if got := contentIndexObjectKey(req, "bucket", "bucket/dir/sample.csv"); got != "bucket/dir/sample.csv" {
		t.Fatalf("object key = %q, want bucket/dir/sample.csv", got)
	}
}
