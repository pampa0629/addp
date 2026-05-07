package metaitem

import (
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/format"
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

func TestObjectStorageSingleFileItemTypeUsesBuiltinRule(t *testing.T) {
	t.Parallel()

	got := ObjectStorageSingleFileItemType(&dataitem.DetectedItem{
		Format:       "geojson",
		Organization: dataitem.OrganizationSingle,
	})
	if got != "table" {
		t.Fatalf("itemType = %q, want table", got)
	}

	got = ObjectStorageSingleFileItemType(&dataitem.DetectedItem{
		Format:       "pdf",
		Organization: dataitem.OrganizationSingle,
	})
	if got != "file" {
		t.Fatalf("pdf itemType = %q, want file", got)
	}

	got = ObjectStorageSingleFileItemType(&dataitem.DetectedItem{
		Format:       "parquet",
		Organization: dataitem.OrganizationSingle,
	})
	if got != "lake_table" {
		t.Fatalf("parquet itemType = %q, want lake_table", got)
	}
}

func TestObjectStorageCompositeNameUsesSingleFileEntryPath(t *testing.T) {
	t.Parallel()

	name, objectPath := ObjectStorageCompositeName(ObjectStorageCompositeItem{
		Bucket: "addp",
		Prefix: "lake",
		Item: &dataitem.DetectedItem{
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
