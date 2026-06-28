package scanprocessor

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/contentio"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

func TestFileSingleDetectedItemInputKeepsStorageFacts(t *testing.T) {
	t.Parallel()

	modifiedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	resource := &commonModels.Engine{ID: 9, EngineType: "static"}
	parent := &models.MetaNode{ID: 3, FullName: "tables"}
	file := metaitem.StorageFileRef{
		Name:        "sales.csv",
		Path:        "tables/sales.csv",
		CatalogPath: plugin.FileItemPath(resource.ID, "tables/sales.csv"),
		Size:        42,
		ModifiedAt:  modifiedAt,
		ContentType: "text/csv",
	}
	detected := metaitem.InferSingleResourceItem(file)
	fullName := "root/tables/sales.csv"

	input := FileSingleInput(resource, 1, parent, file, detected, "file", file.Name, fullName, nil, nil, models.ScannedDepthDeep)

	if input.ItemType != "file" || input.ItemName != "sales.csv" || input.FullName != fullName {
		t.Fatalf("item identity = %q/%q/%q", input.ItemType, input.ItemName, input.FullName)
	}
	if input.PhysicalPath != file.Path || input.IndexPath != file.Path || input.IndexRelativePath != file.Path {
		t.Fatalf("paths = physical:%q index:%q relative:%q", input.PhysicalPath, input.IndexPath, input.IndexRelativePath)
	}
	if input.CatalogPathFor(file.Path).StringPath() != "tables/sales.csv" {
		t.Fatalf("catalog path = %q", input.CatalogPathFor(file.Path).StringPath())
	}
	if input.DataUpdatedAt == nil || !input.DataUpdatedAt.Equal(modifiedAt) {
		t.Fatalf("DataUpdatedAt = %#v, want %v", input.DataUpdatedAt, modifiedAt)
	}
	if input.SizeBytes != file.Size || !input.IncludeAccessIndex || input.Attributes == nil {
		t.Fatalf("input flags/size/attrs = access:%v size:%d attrs:%#v", input.IncludeAccessIndex, input.SizeBytes, input.Attributes)
	}
}

func TestFileDetectedItemInputKeepsCatalogAndIndexPaths(t *testing.T) {
	t.Parallel()

	size := int64(42)
	resource := &commonModels.Engine{ID: 9, EngineType: "static"}
	parent := &models.MetaNode{ID: 3, FullName: "tables"}
	detected := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			DataType:           datatype.Table,
			Format:             string(format.FormatCSV),
			Layout:             format.LayoutSingle,
			PrimaryContentPath: "/tables/sales.csv",
			SizeBytes:          &size,
		},
	}
	plan, ok := metacatalog.PlanFileCatalogDetectedItem(resource.ID, "/tables", detected, "file")
	if !ok {
		t.Fatal("file item plan should be created")
	}

	input := FileDetectedInput(resource, 1, parent, plan, detected, nil, nil, models.ScannedDepthDeep)

	if input.EngineID != resource.ID || input.TenantID != 1 || input.ParentNode != parent {
		t.Fatalf("scope fields = engine:%d tenant:%d parent:%p", input.EngineID, input.TenantID, input.ParentNode)
	}
	if input.ItemType != "file" || input.ItemName != "sales.csv" || input.FullName != "tables/sales.csv" {
		t.Fatalf("item identity = %q/%q/%q", input.ItemType, input.ItemName, input.FullName)
	}
	if input.PhysicalPath != "tables/sales.csv" || input.IndexPath != "tables/sales.csv" || input.IndexRelativePath != "tables/sales.csv" {
		t.Fatalf("paths = physical:%q index:%q relative:%q", input.PhysicalPath, input.IndexPath, input.IndexRelativePath)
	}
	if input.CatalogPathFor(input.PhysicalPath).StringPath() != "tables/sales.csv" {
		t.Fatalf("catalog path = %q", input.CatalogPathFor(input.PhysicalPath).StringPath())
	}
	if !input.IncludeAccessIndex || input.SizeBytes != size || input.Attributes == nil {
		t.Fatalf("input flags/size/attrs = access:%v size:%d attrs:%#v", input.IncludeAccessIndex, input.SizeBytes, input.Attributes)
	}
}

func TestFileDetectedMultiTIFFInputUsesPrimaryContentPath(t *testing.T) {
	t.Parallel()

	size := int64(162)
	resource := &commonModels.Engine{ID: 9, EngineType: "static"}
	parent := &models.MetaNode{ID: 3, FullName: "geotiff"}
	detected := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			DataType:           datatype.Media,
			Format:             string(format.FormatTIFF),
			Layout:             format.LayoutMulti,
			PrimaryContentPath: "geotiff/srtm_40_01.tif",
			RefList: []dataitem.ItemRef{
				{Path: "geotiff/srtm_40_01.tif", Role: "main", Required: true, Primary: true, Extension: ".tif"},
				{Path: "geotiff/srtm_40_01.tfw", Role: "world_file", Extension: ".tfw"},
				{Path: "geotiff/srtm_40_01.tif.aux.xml", Role: "auxiliary_metadata", Extension: ".aux.xml"},
			},
			SizeBytes: &size,
		},
	}
	plan, ok := metacatalog.PlanFileCatalogDetectedItem(resource.ID, "/geotiff", detected, "file")
	if !ok {
		t.Fatal("file item plan should be created")
	}

	input := FileDetectedInput(resource, 1, parent, plan, detected, nil, nil, models.ScannedDepthDeep)

	if input.ItemName != "srtm_40_01.tif" || input.FullName != "geotiff/srtm_40_01.tif" {
		t.Fatalf("item identity = %q/%q, want primary TIFF", input.ItemName, input.FullName)
	}
	if input.PhysicalPath != "geotiff/srtm_40_01.tif" {
		t.Fatalf("physical path = %q, want primary TIFF path", input.PhysicalPath)
	}
	if input.CatalogPathFor(input.PhysicalPath).StringPath() != "geotiff/srtm_40_01.tif" {
		t.Fatalf("catalog path = %q, want primary TIFF path", input.CatalogPathFor(input.PhysicalPath).StringPath())
	}
}

func TestFileDetectedWholeScopeInputUsesScopeRootPath(t *testing.T) {
	t.Parallel()

	size := int64(1024)
	resource := &commonModels.Engine{ID: 9, EngineType: "static"}
	parent := &models.MetaNode{ID: 3, FullName: "3d"}
	detected := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			DataType:           datatype.Model3D,
			Format:             string(format.FormatOSGBScene),
			Layout:             format.LayoutWhole,
			PrimaryContentPath: "3d/site/metadata.xml",
			ScopePath:          "3d/site",
			RefList: []dataitem.ItemRef{
				{Path: "3d/site/metadata.xml", Role: "manifest", Required: true, Primary: true, Extension: ".xml"},
			},
			SizeBytes: &size,
		},
		PhysicalPath: "3d/site",
	}
	plan, ok := metacatalog.PlanFileCatalogDetectedItem(resource.ID, "3d/site", detected, "file")
	if !ok {
		t.Fatal("file item plan should be created")
	}

	input := FileDetectedInput(resource, 1, parent, plan, detected, nil, nil, models.ScannedDepthDeep)

	if input.ItemName != "site" || input.FullName != "3d/site" {
		t.Fatalf("item identity = %q/%q, want whole scope root", input.ItemName, input.FullName)
	}
	if input.PhysicalPath != "3d/site" {
		t.Fatalf("physical path = %q, want whole scope root", input.PhysicalPath)
	}
	if input.CatalogPathFor(input.PhysicalPath).StringPath() != "3d/site" {
		t.Fatalf("catalog path = %q, want whole scope root", input.CatalogPathFor(input.PhysicalPath).StringPath())
	}
}

func TestObjectCompositeDetectedItemInputKeepsBucketAndObjectPaths(t *testing.T) {
	t.Parallel()

	size := int64(256)
	resource := &commonModels.Engine{ID: 7, EngineType: "static"}
	parent := &models.MetaNode{ID: 5, FullName: "addp/datasets/roads"}
	composite := metacatalog.ObjectCatalogCompositeItem{
		Bucket: "addp",
		Prefix: "datasets/roads",
		Item: &metaitem.DetectedItem{
			ResolvedItem: dataitem.ResolvedItem{
				DataType:           datatype.Table,
				Format:             string(format.FormatShapefile),
				Layout:             format.LayoutMulti,
				PrimaryContentPath: "addp/datasets/roads/roads.shp",
				SizeBytes:          &size,
			},
		},
	}
	plan, ok := metacatalog.PlanObjectCatalogCompositeItem(resource.ID, composite, "object")
	if !ok {
		t.Fatal("object composite item plan should be created")
	}

	input := ObjectCompositeInput(resource, 1, resource.ID, parent, plan, composite, nil, nil, models.ScannedDepthDeep)

	if input.EngineID != resource.ID || input.TenantID != 1 || input.ParentNode != parent {
		t.Fatalf("scope fields = engine:%d tenant:%d parent:%p", input.EngineID, input.TenantID, input.ParentNode)
	}
	if input.ItemType != "object" || input.ItemName != "roads.shp" || input.FullName != "addp/datasets/roads/roads.shp" {
		t.Fatalf("item identity = %q/%q/%q", input.ItemType, input.ItemName, input.FullName)
	}
	if input.PhysicalPath != "addp/datasets/roads/roads.shp" {
		t.Fatalf("physical path = %q", input.PhysicalPath)
	}
	if input.IndexRootName != "addp" || input.IndexPath != "datasets/roads/roads.shp" || input.IndexRelativePath != "datasets/roads/roads.shp" {
		t.Fatalf("index fields = root:%q path:%q relative:%q", input.IndexRootName, input.IndexPath, input.IndexRelativePath)
	}
	if input.CatalogPathFor(input.IndexPath).StringPath() != "addp/datasets/roads/roads.shp" {
		t.Fatalf("catalog path = %q", input.CatalogPathFor(input.IndexPath).StringPath())
	}
	if !input.IncludeAccessIndex || input.SizeBytes != size || input.Attributes == nil {
		t.Fatalf("input flags/size/attrs = access:%v size:%d attrs:%#v", input.IncludeAccessIndex, input.SizeBytes, input.Attributes)
	}
}

func TestObjectCompositeMultiTIFFInputUsesPrimaryObject(t *testing.T) {
	t.Parallel()

	size := int64(162)
	resource := &commonModels.Engine{ID: 7, EngineType: "static"}
	parent := &models.MetaNode{ID: 5, FullName: "addp/image"}
	composite := metacatalog.ObjectCatalogCompositeItem{
		Bucket: "addp",
		Prefix: "image",
		Item: &metaitem.DetectedItem{
			ResolvedItem: dataitem.ResolvedItem{
				DataType:           datatype.Media,
				Format:             string(format.FormatTIFF),
				Layout:             format.LayoutMulti,
				PrimaryContentPath: "addp/image/srtm_40_01.tif",
				RefList: []dataitem.ItemRef{
					{Path: "addp/image/srtm_40_01.tif", Role: "main", Required: true, Primary: true, Extension: ".tif"},
					{Path: "addp/image/srtm_40_01.tfw", Role: "world_file", Extension: ".tfw"},
					{Path: "addp/image/srtm_40_01.hdr", Role: "header", Extension: ".hdr"},
					{Path: "addp/image/srtm_40_01.tif.aux.xml", Role: "auxiliary_metadata", Extension: ".aux.xml"},
				},
				SizeBytes: &size,
			},
		},
	}
	plan, ok := metacatalog.PlanObjectCatalogCompositeItem(resource.ID, composite, "object")
	if !ok {
		t.Fatal("object composite item plan should be created")
	}

	input := ObjectCompositeInput(resource, 1, resource.ID, parent, plan, composite, nil, nil, models.ScannedDepthDeep)

	if input.ItemName != "srtm_40_01.tif" || input.FullName != "addp/image/srtm_40_01.tif" {
		t.Fatalf("item identity = %q/%q, want primary TIFF object", input.ItemName, input.FullName)
	}
	if input.PhysicalPath != "addp/image/srtm_40_01.tif" {
		t.Fatalf("physical path = %q, want primary TIFF object", input.PhysicalPath)
	}
	if input.IndexRootName != "addp" || input.IndexPath != "image/srtm_40_01.tif" {
		t.Fatalf("index fields = root:%q path:%q, want primary object", input.IndexRootName, input.IndexPath)
	}
	if input.CatalogPathFor(input.IndexPath).StringPath() != "addp/image/srtm_40_01.tif" {
		t.Fatalf("catalog path = %q, want bucket-qualified primary object", input.CatalogPathFor(input.IndexPath).StringPath())
	}
}

func TestObjectSingleDetectedItemInputKeepsEnhancedAttrsAndStorageFacts(t *testing.T) {
	t.Parallel()

	modifiedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	resource := &commonModels.Engine{ID: 7, EngineType: "static"}
	parent := &models.MetaNode{ID: 5, FullName: "addp/datasets"}
	catalogResource := metacatalog.StorageResource{
		RootName:     "addp",
		Path:         "datasets/profile.json",
		FullPath:     "addp/datasets/profile.json",
		NodeType:     "object",
		Format:       string(format.FormatJSON),
		SizeBytes:    128,
		LastModified: &modifiedAt,
		CatalogPath:  plugin.ObjectItemPath(resource.ID, "addp", "datasets/profile.json"),
	}
	plan := metacatalog.PlanObjectCatalogSingleItem(resource.ID, catalogResource, "datasets/profile.json", "object")
	attrs := models.JSONMap{
		"storage": map[string]interface{}{"path": "kept/from/existing"},
	}

	input := ObjectSingleInput(resource, 1, resource.ID, parent, plan, catalogResource, attrs, "datasets/profile.json", nil, nil, models.ScannedDepthDeep)

	if input.ItemType != "object" || input.ItemName != "profile.json" || input.FullName != "addp/datasets/profile.json" {
		t.Fatalf("item identity = %q/%q/%q", input.ItemType, input.ItemName, input.FullName)
	}
	if input.Attributes["storage"].(map[string]interface{})["path"] != "kept/from/existing" {
		t.Fatalf("attrs not preserved: %#v", input.Attributes)
	}
	if input.PhysicalPath != catalogResource.FullPath || input.IndexRootName != "addp" || input.IndexPath != catalogResource.Path || input.IndexRelativePath != "datasets/profile.json" {
		t.Fatalf("paths = physical:%q root:%q index:%q relative:%q", input.PhysicalPath, input.IndexRootName, input.IndexPath, input.IndexRelativePath)
	}
	if input.CatalogPathFor(input.IndexPath).StringPath() != "addp/datasets/profile.json" {
		t.Fatalf("catalog path = %q", input.CatalogPathFor(input.IndexPath).StringPath())
	}
	if input.DataUpdatedAt == nil || !input.DataUpdatedAt.Equal(modifiedAt) {
		t.Fatalf("DataUpdatedAt = %#v, want %v", input.DataUpdatedAt, modifiedAt)
	}
	if input.SizeBytes != catalogResource.SizeBytes || !input.IncludeAccessIndex {
		t.Fatalf("input flags/size = access:%v size:%d", input.IncludeAccessIndex, input.SizeBytes)
	}
}

func TestKnownMultiTableRefreshClearsStaleAccessIndex(t *testing.T) {
	t.Parallel()

	multiAttrs := map[string]interface{}{
		"access_index": map[string]interface{}{
			"table": map[string]interface{}{"kind": "stale"},
		},
	}
	multiItemAttrs := map[string]interface{}{
		"access_index": map[string]interface{}{
			"table": map[string]interface{}{"kind": "stale-item"},
		},
	}
	clearStaleKnownMultiTableAccessIndex(multiAttrs, &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:   format.LayoutMulti,
			DataType: datatype.Table,
			Format:   "custom_multi_table",
		},
		Attributes: multiItemAttrs,
	})
	if accessIndex, ok := multiAttrs["access_index"].(map[string]interface{}); ok {
		if tableIndex, exists := accessIndex["table"]; exists {
			t.Fatalf("multi access_index.table = %#v, want removed", tableIndex)
		}
	}
	if accessIndex, ok := multiItemAttrs["access_index"].(map[string]interface{}); ok {
		if tableIndex, exists := accessIndex["table"]; exists {
			t.Fatalf("multi item access_index.table = %#v, want removed", tableIndex)
		}
	}

	singleAttrs := map[string]interface{}{
		"access_index": map[string]interface{}{
			"table": map[string]interface{}{"kind": "keep"},
		},
	}
	clearStaleKnownMultiTableAccessIndex(singleAttrs, &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:   format.LayoutSingle,
			DataType: datatype.Table,
			Format:   "csv",
		},
		Attributes: map[string]interface{}{},
	})
	tableIndex, ok := singleAttrs["access_index"].(map[string]interface{})["table"].(map[string]interface{})
	if !ok {
		t.Fatalf("single access_index.table missing: %#v", singleAttrs)
	}
	if got := tableIndex["kind"]; got != "keep" {
		t.Fatalf("single access_index.table.kind = %q, want keep", got)
	}
}

func TestEnrichKnownMultiTablePreservesBaseItemAndStorageAttributes(t *testing.T) {
	formatType := format.FormatType("scanprocessor_multi_table_preserve")
	if err := format.RegisterFormatPlugin(scanProcessorMultiTableProvider{formatType: formatType}); err != nil {
		t.Fatalf("RegisterFormatPlugin() error = %v", err)
	}

	size := int64(24)
	detected := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutMulti,
			DataType:           datatype.Table,
			Format:             string(formatType),
			PrimaryContentPath: "roads/roads.main",
			RefList: []dataitem.ItemRef{
				{Path: "roads/roads.main", Role: "main", Required: true, Primary: true, Extension: ".main"},
				{Path: "roads/roads.attr", Role: "attributes", Required: true, Extension: ".attr"},
			},
			SizeBytes: &size,
		},
		PhysicalPath: "roads",
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(detected)))
	metaattr.SetStorage(attrs, "physical_path", "roads/roads.main")

	got, err := (Processor{}).enrichKnownMultiTable(context.Background(), &input{
		Detected:       detected,
		ContentReader:  scanProcessorContentReader{},
		EngineID:       1,
		CatalogPathFor: plugin.FileItemPathForEngine(1),
	}, attrs)
	if err != nil {
		t.Fatalf("enrichKnownMultiTable() error = %v", err)
	}

	if gotLayout := commonJSON.String(got, "item", "layout"); gotLayout != string(format.LayoutMulti) {
		t.Fatalf("item.layout = %q, want multi", gotLayout)
	}
	if gotFormat := commonJSON.String(got, "item", "format"); gotFormat != string(formatType) {
		t.Fatalf("item.format = %q, want %s", gotFormat, formatType)
	}
	if refs := commonJSON.InterfaceSlice(commonJSON.Section(got, "item")["refs"]); len(refs) != 2 {
		t.Fatalf("item.refs = %#v, want 2 refs", refs)
	}
	if gotPath := commonJSON.String(got, "storage", "physical_path"); gotPath != "roads/roads.main" {
		t.Fatalf("storage.physical_path = %q, want roads/roads.main", gotPath)
	}
	if table := datatype.TableInfoFromPayload(commonJSON.Section(got, "type_info.table"), ""); table == nil || table.GetField("name") == nil {
		t.Fatalf("type_info.table = %#v, want enriched table field", commonJSON.Section(got, "type_info.table"))
	}
	if formatInfo := commonJSON.Section(got, "format_info."+string(formatType)); formatInfo["source"] != "test-provider" {
		t.Fatalf("format_info.%s = %#v, want merged provider facts", formatType, formatInfo)
	}
}

type scanProcessorMultiTableProvider struct {
	formatType format.FormatType
}

func (p scanProcessorMultiTableProvider) Format() format.FormatType {
	return p.formatType
}

func (p scanProcessorMultiTableProvider) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "scanprocessor-multi-table-preserve",
		Format:   p.formatType,
		DataType: datatype.Table,
		Layouts:  []string{format.LayoutMulti},
		Identification: format.FormatIdentification{
			Extensions: []string{".main"},
		},
	}
}

func (p scanProcessorMultiTableProvider) RelatedRefSpecs() []format.RelatedRefSpec {
	return []format.RelatedRefSpec{
		{Extension: ".main", Role: "main", Required: true, Primary: true},
		{Extension: ".attr", Role: "attributes", Required: true},
	}
}

func (p scanProcessorMultiTableProvider) DescribeMultiTable(context.Context, contentio.Reader, []format.RelatedRef, *format.ParseOptions) (*format.TableDescribeResult, error) {
	return &format.TableDescribeResult{
		Table: &datatype.TableInfo{
			Name:   "roads",
			Fields: []datatype.FieldInfo{{Name: "name", Type: datatype.FieldTypeString}},
		},
		FormatInfo: map[string]interface{}{"source": "test-provider"},
	}, nil
}

type scanProcessorContentReader struct{}

func (scanProcessorContentReader) Type() string         { return "scanprocessor_content_reader" }
func (scanProcessorContentReader) DisplayName() string  { return "scanprocessor content reader" }
func (scanProcessorContentReader) EngineOrigin() string { return "general" }
func (scanProcessorContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (scanProcessorContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (scanProcessorContentReader) DefaultPort() int                                   { return 0 }
func (scanProcessorContentReader) RequiredFields() []string                           { return nil }
func (scanProcessorContentReader) SensitiveFields() []string                          { return nil }
func (scanProcessorContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (scanProcessorContentReader) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (scanProcessorContentReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
