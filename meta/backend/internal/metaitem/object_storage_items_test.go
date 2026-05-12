package metaitem

import (
	"testing"
	"time"

	"github.com/addp/common/format"
	"github.com/addp/meta/internal/dataitem"
)

func TestObjectMetasByParentPrefixDoesNotAddCrossLayerCompositeCandidates(t *testing.T) {
	t.Parallel()

	groups := objectMetasByParentPrefix([]format.ObjectMetadata{
		{Bucket: "addp", Path: "datasets/roads/roads.shp", NodeType: "object"},
		{Bucket: "addp", Path: "datasets/roads/roads.shx", NodeType: "object"},
		{Bucket: "addp", Path: "datasets/roads/attributes/roads.dbf", NodeType: "object"},
	})

	if got := len(groups["addp\x00datasets/roads"]); got != 2 {
		t.Fatalf("datasets/roads candidate size = %d, want 2", got)
	}
	if got := len(groups["addp\x00datasets/roads/attributes"]); got != 1 {
		t.Fatalf("direct child candidate size = %d, want 1", got)
	}
}

func TestUnclaimedObjectMetasFiltersAlreadyClaimedComponents(t *testing.T) {
	t.Parallel()

	group := []format.ObjectMetadata{
		{Path: "datasets/roads/roads.shp"},
		{Path: "datasets/roads/roads.shx"},
		{Path: "datasets/roads/roads.dbf"},
	}
	filtered := unclaimedObjectMetas(group, map[string]bool{
		"datasets/roads/roads.shp": true,
	})

	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
	if filtered[0].Path == "datasets/roads/roads.shp" {
		t.Fatalf("claimed component should be removed: %#v", filtered)
	}
}

func TestObjectStorageCompositeNameUsesSingleFileEntryPath(t *testing.T) {
	t.Parallel()

	name, objectPath := ObjectStorageCompositeName(ObjectStorageCompositeItem{
		Bucket: "addp",
		Prefix: "lake",
		Item: &DetectedItem{
			Organization: dataitem.OrganizationSingle,
			EntryPath:    "addp/lake/sales.parquet",
		},
	})

	if name != "sales.parquet" {
		t.Fatalf("name = %q, want sales.parquet", name)
	}
	if objectPath != "lake/sales.parquet" {
		t.Fatalf("objectPath = %q, want lake/sales.parquet", objectPath)
	}
}

func TestPlanObjectStorageRelativePathRemovesScanPrefix(t *testing.T) {
	t.Parallel()

	plan := PlanObjectStorageRelativePath("datasets/roads/roads.shp", "datasets")
	if plan.ExactBase {
		t.Fatal("child path should not be treated as exact base")
	}
	if len(plan.Segments) != 2 || plan.Segments[0] != "roads" || plan.Segments[1] != "roads.shp" {
		t.Fatalf("segments = %#v, want roads/roads.shp", plan.Segments)
	}
}

func TestPlanObjectStorageRelativePathDetectsExactScanPrefix(t *testing.T) {
	t.Parallel()

	plan := PlanObjectStorageRelativePath("datasets", "datasets")
	if !plan.ExactBase {
		t.Fatal("exact scan prefix should be detected")
	}
	if len(plan.Segments) != 0 {
		t.Fatalf("segments = %#v, want none", plan.Segments)
	}
}

func TestPlanObjectStorageSingleItemBuildsIdentityAndAttributes(t *testing.T) {
	t.Parallel()

	modifiedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	plan := PlanObjectStorageSingleItem(7, format.ObjectMetadata{
		Bucket:       "addp",
		Path:         "datasets/roads.geojson",
		NodeType:     "object",
		FileType:     "json",
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
	if item["data_type"] != string(dataitem.DataTypeTable) || item["format"] != "json" {
		t.Fatalf("item attrs = %#v", item)
	}
	spatial := plan.Attributes["capabilities"].(map[string]interface{})["spatial"].(map[string]interface{})
	if spatial["primary_geometry_column"] != "geometry" {
		t.Fatalf("capabilities.spatial = %#v", spatial)
	}
	storage := plan.Attributes["storage"].(map[string]interface{})
	if storage["physical_path"] != "addp/datasets/roads.geojson" || storage["total_size"] != int64(128) {
		t.Fatalf("storage attrs = %#v", storage)
	}
}

func TestPlanObjectStorageCompositeItemBuildsStandardAttributes(t *testing.T) {
	t.Parallel()

	plan, ok := PlanObjectStorageCompositeItem(7, ObjectStorageCompositeItem{
		Bucket: "addp",
		Prefix: "datasets/roads",
		Item: &DetectedItem{
			DataType:     dataitem.DataTypeTable,
			Organization: dataitem.OrganizationMulti,
			EntryPath:    "addp/datasets/roads/roads.shp",
			SizeBytes:    256,
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
