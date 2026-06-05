package scanprocessor

import (
	"testing"
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
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

func TestClearStaleKnownMultiTableAccessIndexUsesLayoutSemantics(t *testing.T) {
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
	ClearStaleKnownMultiTableAccessIndex(multiAttrs, &metaitem.DetectedItem{
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
	ClearStaleKnownMultiTableAccessIndex(singleAttrs, &metaitem.DetectedItem{
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
