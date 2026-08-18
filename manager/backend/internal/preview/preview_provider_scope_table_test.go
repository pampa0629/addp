package preview

import (
	"context"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/manager/internal/models"
)

type fakeRuntimeScopeTableProvider struct {
	reader  *fakeRuntimeScopeTableReader
	ref     contentio.Ref
	options *format.ParseOptions
}

func (p *fakeRuntimeScopeTableProvider) Format() format.FormatType { return format.FormatFileGDB }

func (p *fakeRuntimeScopeTableProvider) OpenTableScopeReader(_ context.Context, _ contentio.Reader, ref contentio.Ref, options *format.ParseOptions) (format.TableReader, error) {
	p.ref = ref
	p.options = options
	return p.reader, nil
}

type fakeRuntimeScopeTableReader struct {
	fields  []datatype.FieldInfo
	spatial *datatype.SpatialInfo
	rows    []map[string]interface{}
	offset  int
	closed  bool
}

func (r *fakeRuntimeScopeTableReader) Fields() []datatype.FieldInfo {
	return append([]datatype.FieldInfo(nil), r.fields...)
}

func (r *fakeRuntimeScopeTableReader) SpatialInfo() *datatype.SpatialInfo { return r.spatial.Clone() }

func (r *fakeRuntimeScopeTableReader) ReadRows(_ context.Context, limit int) ([]map[string]interface{}, error) {
	if r.offset >= len(r.rows) {
		return []map[string]interface{}{}, nil
	}
	end := r.offset + limit
	if end > len(r.rows) {
		end = len(r.rows)
	}
	rows := append([]map[string]interface{}(nil), r.rows[r.offset:end]...)
	r.offset = end
	return rows, nil
}

func (r *fakeRuntimeScopeTableReader) Close(context.Context) error {
	r.closed = true
	return nil
}

type recordingCatalogProvider struct {
	parent plugin.CatalogPath
}

func (p *recordingCatalogProvider) Type() string         { return "recording" }
func (p *recordingCatalogProvider) DisplayName() string  { return "recording" }
func (p *recordingCatalogProvider) Description() string  { return "" }
func (p *recordingCatalogProvider) Version() string      { return "" }
func (p *recordingCatalogProvider) EngineOrigin() string { return "general" }
func (p *recordingCatalogProvider) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingCatalogProvider) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *recordingCatalogProvider) DefaultPort() int          { return 0 }
func (p *recordingCatalogProvider) RequiredFields() []string  { return nil }
func (p *recordingCatalogProvider) SensitiveFields() []string { return nil }
func (p *recordingCatalogProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p *recordingCatalogProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p *recordingCatalogProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.CatalogPath, _ plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	p.parent = parent
	return nil, nil
}
func (p *recordingCatalogProvider) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return nil, nil
}

func TestScopeTableContentReaderUsesObjectCatalogReader(t *testing.T) {
	t.Parallel()

	reader, err := scopeTableContentReader(&PreviewRequest{
		Engine:   &models.Engine{EngineType: "minio", ID: 7},
		ItemType: "object",
		Schema:   "demo",
	}, nil, nil, plugin.ConnectionInfo{"bucket": "demo"})
	if err != nil {
		t.Fatalf("scopeTableContentReader() error = %v", err)
	}
	if _, ok := reader.(*objectCatalogContentReader); !ok {
		t.Fatalf("reader = %T, want *objectCatalogContentReader", reader)
	}
}

func TestScopeTableContentReaderUsesFileCatalogReader(t *testing.T) {
	t.Parallel()

	reader, err := scopeTableContentReader(&PreviewRequest{
		Engine:   &models.Engine{EngineType: "nfs", ID: 7},
		ItemType: "file",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("scopeTableContentReader() error = %v", err)
	}
	if _, ok := reader.(*fileCatalogContentReader); !ok {
		t.Fatalf("reader = %T, want *fileCatalogContentReader", reader)
	}
}

func TestResolveScopeTableFormatDoesNotInferFromPhysicalPath(t *testing.T) {
	t.Parallel()

	got := resolveScopeTableFormat(&PreviewRequest{
		PhysicalPath: "bucket/dataset/part-000.parquet",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"data_type": "table",
				"layout":    "whole",
			},
		},
	})
	if got != format.FormatUnknown {
		t.Fatalf("resolveScopeTableFormat() = %s, want unknown without standard item.format", got)
	}
}

func TestObjectCatalogContentReaderListTrimsBucketFromScope(t *testing.T) {
	t.Parallel()

	catalog := &recordingCatalogProvider{}
	reader := newObjectCatalogContentReader(nil, catalog, nil, 7, "demo")
	if _, err := reader.List(context.Background(), contentio.NewRef("demo/dataset", contentio.RoleScope)); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if got := catalog.parent.StringPath(); got != "demo/dataset" {
		t.Fatalf("catalog path = %q, want demo/dataset", got)
	}
}

func TestScopeTableSampleOptionsFromMetaAttributesUsesParquetFileRowCounts(t *testing.T) {
	t.Parallel()

	opts := scopeTableSampleOptionsFromMetaAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"format": "parquet",
		},
		"format_info": map[string]interface{}{
			"parquet": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"path": "/dataset/part-000.parquet", "row_count": 2},
					map[string]interface{}{"path": "dataset/part-001.parquet", "row_count": int64(3)},
				},
			},
		},
	})
	if opts == nil || opts.ExtraParams == nil {
		t.Fatal("expected parse options with parquet file row counts")
	}
	counts := map[string]int64{}
	for _, value := range opts.ExtraParams {
		if typed, ok := value.(map[string]int64); ok {
			counts = typed
			break
		}
	}
	if counts["dataset/part-000.parquet"] != 2 || counts["dataset/part-001.parquet"] != 3 {
		t.Fatalf("counts = %#v", counts)
	}
}

func TestScopeTableSampleOptionsFromMetaAttributesIgnoresUnknownFormat(t *testing.T) {
	t.Parallel()

	opts := scopeTableSampleOptionsFromMetaAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"format": "yml",
		},
		"format_info": map[string]interface{}{
			"parquet": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"path": "dataset/part-000.parquet", "row_count": 2},
				},
			},
		},
	})
	if opts != nil {
		t.Fatalf("options = %#v, want nil for unknown legacy format", opts)
	}
}

func TestScopeTablePreviewReadsRuntimeContainerChildWithSpatialMetadata(t *testing.T) {
	srid := 4326
	dimension := 2
	extent := datatype.NewBoundingBox(100, 20, 104, 24)
	reader := &fakeRuntimeScopeTableReader{
		fields: []datatype.FieldInfo{
			{Name: "OBJECTID", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Name: "SHAPE", Type: datatype.FieldTypeGeometry, Nullable: true},
		},
		spatial: &datatype.SpatialInfo{
			SRID:                  &srid,
			CRSRef:                "EPSG:4326",
			PrimaryGeometryColumn: "SHAPE",
			GeometryColumns: []datatype.GeometryColumnInfo{{
				Name: "SHAPE", GeometryType: "Point", SRID: &srid, Dimension: &dimension,
			}},
			Extent: &extent,
		},
		rows: []map[string]interface{}{
			{"OBJECTID": int64(1), "SHAPE": []byte{1}},
			{"OBJECTID": int64(2), "SHAPE": []byte{2}},
			{"OBJECTID": int64(3), "SHAPE": []byte{3}},
			{"OBJECTID": int64(4), "SHAPE": []byte{4}},
			{"OBJECTID": int64(5), "SHAPE": []byte{5}},
		},
	}
	provider := &fakeRuntimeScopeTableProvider{reader: reader}
	preview, err := NewScopeTablePreviewProvider().Preview(context.Background(), &PreviewRequest{
		Engine:                   &models.Engine{ID: 14, EngineType: "nfs"},
		Schema:                   "arcgis",
		Table:                    "pgeo_roundtrip.gdb",
		ScopePath:                "arcgis/pgeo_roundtrip.gdb",
		ChildName:                "PGEO_WGS84_POINTS",
		Page:                     2,
		PageSize:                 2,
		ScopeTableReaderProvider: provider,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{"data_type": "container", "format": "filegdb", "layout": "whole"},
			"type_info": map[string]interface{}{
				"container": map[string]interface{}{
					"children": []interface{}{
						map[string]interface{}{"name": "PGEO_WGS84_POINTS", "table": "PGEO_WGS84_POINTS", "row_count": int64(265)},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if len(preview.Rows) != 2 || preview.Rows[0]["OBJECTID"] != int64(3) || preview.Rows[1]["OBJECTID"] != int64(4) {
		t.Fatalf("rows = %#v, want second page OBJECTID 3 and 4", preview.Rows)
	}
	if preview.Total != 265 || preview.GeometryColumn != "SHAPE" || preview.SourceSRID != 4326 || preview.SourceCRS != "EPSG:4326" {
		t.Fatalf("preview spatial/total = %#v", preview)
	}
	if len(preview.Extent) != 4 || preview.Extent[0] != 100 || preview.Extent[3] != 24 {
		t.Fatalf("extent = %#v, want [100 20 104 24]", preview.Extent)
	}
	if provider.ref.Role != contentio.RoleScope || provider.ref.Path != "arcgis/pgeo_roundtrip.gdb" {
		t.Fatalf("scope ref = %#v", provider.ref)
	}
	if provider.options == nil || provider.options.GeometryEncoding != format.GeometryEncodingEWKB || provider.options.ExtraParams[format.ChildTableParam] != "PGEO_WGS84_POINTS" {
		t.Fatalf("parse options = %#v, want selected child and EWKB", provider.options)
	}
	if !reader.closed {
		t.Fatal("runtime scope reader was not closed")
	}
}
