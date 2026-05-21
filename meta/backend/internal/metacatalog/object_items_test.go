package metacatalog

import (
	"testing"
	"time"

	commondataitem "github.com/addp/common/dataitem"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaitem"
)

func TestObjectResourcesByParentPrefixDoesNotAddCrossLayerCompositeCandidates(t *testing.T) {
	t.Parallel()

	groups := objectResourcesByParentPrefix([]StorageResource{
		{RootName: "addp", Path: "datasets/roads/roads.shp", NodeType: "object"},
		{RootName: "addp", Path: "datasets/roads/roads.shx", NodeType: "object"},
		{RootName: "addp", Path: "datasets/roads/attributes/roads.dbf", NodeType: "object"},
	})

	if got := len(groups["addp\x00datasets/roads"]); got != 2 {
		t.Fatalf("datasets/roads candidate size = %d, want 2", got)
	}
	if got := len(groups["addp\x00datasets/roads/attributes"]); got != 1 {
		t.Fatalf("direct child candidate size = %d, want 1", got)
	}
}

func TestObjectResourcesByCompositePrefixAddsPartitionedWholeScopeCandidate(t *testing.T) {
	t.Parallel()

	groups := objectResourcesByCompositePrefix([]StorageResource{
		{RootName: "addp", Path: "datasets/orders/dt=2026-05-20/part-000.parquet", NodeType: "object"},
		{RootName: "addp", Path: "datasets/orders/dt=2026-05-21/part-001.parquet", NodeType: "object"},
	})

	if got := len(groups["addp\x00datasets/orders"]); got != 2 {
		t.Fatalf("partitioned dataset candidate size = %d, want 2", got)
	}
	if got := len(groups["addp\x00datasets/orders/dt=2026-05-20"]); got != 1 {
		t.Fatalf("direct partition candidate size = %d, want 1", got)
	}
}

func TestUnclaimedObjectResourcesFiltersAlreadyClaimedComponents(t *testing.T) {
	t.Parallel()

	group := []StorageResource{
		{Path: "datasets/roads/roads.shp"},
		{Path: "datasets/roads/roads.shx"},
		{Path: "datasets/roads/roads.dbf"},
	}
	filtered := unclaimedObjectResources(group, map[string]bool{
		"datasets/roads/roads.shp": true,
	})

	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
	if filtered[0].Path == "datasets/roads/roads.shp" {
		t.Fatalf("claimed component should be removed: %#v", filtered)
	}
}

func TestObjectCatalogCompositeNameUsesSingleFileEntryPath(t *testing.T) {
	t.Parallel()

	name, objectPath := ObjectCatalogCompositeName(ObjectCatalogCompositeItem{
		Bucket: "addp",
		Prefix: "lake",
		Item: &metaitem.DetectedItem{
			ResolvedItem: commondataitem.ResolvedItem{
				Layout:    commondataitem.LayoutSingle,
				EntryPath: "addp/lake/sales.parquet",
			},
		},
	})

	if name != "sales.parquet" {
		t.Fatalf("name = %q, want sales.parquet", name)
	}
	if objectPath != "lake/sales.parquet" {
		t.Fatalf("objectPath = %q, want lake/sales.parquet", objectPath)
	}
}

func TestPlanObjectCatalogRelativePathRemovesScanPrefix(t *testing.T) {
	t.Parallel()

	plan := PlanObjectCatalogRelativePath("datasets/roads/roads.shp", "datasets")
	if plan.ExactBase {
		t.Fatal("child path should not be treated as exact base")
	}
	if len(plan.Segments) != 2 || plan.Segments[0] != "roads" || plan.Segments[1] != "roads.shp" {
		t.Fatalf("segments = %#v, want roads/roads.shp", plan.Segments)
	}
}

func TestPlanObjectCatalogRelativePathDetectsExactScanPrefix(t *testing.T) {
	t.Parallel()

	plan := PlanObjectCatalogRelativePath("datasets", "datasets")
	if !plan.ExactBase {
		t.Fatal("exact scan prefix should be detected")
	}
	if len(plan.Segments) != 0 {
		t.Fatalf("segments = %#v, want none", plan.Segments)
	}
}

func TestPlanObjectCatalogSingleItemBuildsIdentityAndAttributes(t *testing.T) {
	t.Parallel()

	modifiedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	plan := PlanObjectCatalogSingleItem(7, StorageResource{
		RootName:     "addp",
		Path:         "datasets/roads.geojson",
		FullPath:     "addp/datasets/roads.geojson",
		NodeType:     "object",
		Format:       "json",
		SizeBytes:    128,
		LastModified: &modifiedAt,
	}, "datasets/roads.geojson", "object")

	if plan.ItemType != "object" || plan.ItemName != "roads.geojson" {
		t.Fatalf("item identity = %#v", plan)
	}
	if plan.FullName != "addp/datasets/roads.geojson" || plan.Fingerprint == "" {
		t.Fatalf("fullName/fingerprint = %q/%q", plan.FullName, plan.Fingerprint)
	}
	item := plan.Attributes["item"].(map[string]interface{})
	if item["data_type"] != string(commondataitem.DataTypeDocument) || item["format"] != "json" {
		t.Fatalf("item attrs = %#v", item)
	}
	if capabilities, ok := plan.Attributes["capabilities"].(map[string]interface{}); ok {
		if spatial := capabilities["spatial"]; spatial != nil {
			t.Fatalf("geojson path should not imply spatial capability: %#v", spatial)
		}
	}
	storage := plan.Attributes["storage"].(map[string]interface{})
	if storage["physical_path"] != "addp/datasets/roads.geojson" || storage["total_size"] != int64(128) {
		t.Fatalf("storage attrs = %#v", storage)
	}
}

func TestPlanObjectCatalogCompositeItemBuildsStandardAttributes(t *testing.T) {
	t.Parallel()

	plan, ok := PlanObjectCatalogCompositeItem(7, ObjectCatalogCompositeItem{
		Bucket: "addp",
		Prefix: "datasets/roads",
		Item: &metaitem.DetectedItem{
			ResolvedItem: commondataitem.ResolvedItem{
				DataType:  commondataitem.DataTypeTable,
				Layout:    commondataitem.LayoutMulti,
				EntryPath: "addp/datasets/roads/roads.shp",
				SizeBytes: int64PtrForTest(256),
			},
			Fields: []format.FieldInfo{{
				Name: "id",
				Type: format.FieldTypeInt,
			}},
		},
	}, "object")
	if !ok {
		t.Fatal("composite item plan should be created")
	}
	if plan.ItemType != "object" {
		t.Fatalf("itemType = %q, want object", plan.ItemType)
	}
	if plan.ItemName != "roads.shp" || plan.ParentPath != "datasets/roads/" {
		t.Fatalf("plan identity = %#v", plan)
	}
	if plan.FullName != "addp/datasets/roads/roads.shp" || plan.Fingerprint == "" {
		t.Fatalf("fullName/fingerprint = %q/%q", plan.FullName, plan.Fingerprint)
	}
	storage := plan.Attributes["storage"].(map[string]interface{})
	if storage["bucket"] != "addp" || storage["path"] != "datasets/roads/" || storage["name"] != "roads.shp" {
		t.Fatalf("storage attrs = %#v", storage)
	}
	typeInfo := plan.Attributes["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	if table["fields"] == nil {
		t.Fatalf("type_info.table.fields missing: %#v", table)
	}
}

func TestStorageResourcesToFileEntriesUsesBucketRelativeObjectPath(t *testing.T) {
	t.Parallel()

	files := storageResourcesToFileEntries([]StorageResource{{
		RootName: "addp",
		Path:     "gis/roads.shp",
		FullPath: "addp/gis/roads.shp",
		NodeType: "object",
	}})

	if len(files) != 1 {
		t.Fatalf("files len = %d, want 1", len(files))
	}
	if files[0].Path != "gis/roads.shp" {
		t.Fatalf("file path = %q, want bucket-relative path", files[0].Path)
	}
}

func int64PtrForTest(value int64) *int64 {
	return &value
}
