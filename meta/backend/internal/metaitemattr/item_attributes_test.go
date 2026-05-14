package metaitemattr

import (
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaitem"
)

func int64PtrForTest(value int64) *int64 {
	return &value
}

func TestBuildDataItemAttributesWritesPartitionedItemAndStorage(t *testing.T) {
	item := metaitem.InferSingleResource(metaitem.SingleResourceInput{
		Name: "roads.parquet",
		Path: "bucket/roads.parquet",
		Size: 42,
	})

	attrs := BuildAttributes(item)
	itemAttrs := attrs["item"].(map[string]interface{})
	if itemAttrs["organization"] != string(dataitem.OrganizationSingle) {
		t.Fatalf("item.organization = %v, want %s", itemAttrs["organization"], dataitem.OrganizationSingle)
	}
	if itemAttrs["data_type"] != string(dataitem.DataTypeTable) {
		t.Fatalf("item.data_type = %v, want %s", itemAttrs["data_type"], dataitem.DataTypeTable)
	}
	if itemAttrs["format"] != string(format.FormatParquet) {
		t.Fatalf("item.format = %v, want %s", itemAttrs["format"], format.FormatParquet)
	}
	if attrs["data_type"] != nil || attrs["format"] != nil {
		t.Fatalf("flat item fields should not be written: %#v", attrs)
	}

	storageAttrs := attrs["storage"].(map[string]interface{})
	if storageAttrs["physical_path"] != "bucket/roads.parquet" {
		t.Fatalf("storage.physical_path = %v, want bucket/roads.parquet", storageAttrs["physical_path"])
	}
	if storageAttrs["total_size"] != int64(42) {
		t.Fatalf("storage.total_size = %v, want 42", storageAttrs["total_size"])
	}

}

func TestBuildDataItemAttributesWritesWholeScopePolicy(t *testing.T) {
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Organization: dataitem.OrganizationWhole,
			DataType:     dataitem.DataTypeTable,
			Format:       "parquet",
			SizeBytes:    int64PtrForTest(128),
		},
		PhysicalPath: "/lake/sales",
	}

	attrs := BuildAttributes(item)
	itemAttrs := attrs["item"].(map[string]interface{})
	if itemAttrs["scope_exclusive"] != true || itemAttrs["claim_policy"] != "whole_scope" {
		t.Fatalf("whole item attrs = %#v", itemAttrs)
	}
}

func TestMergeDataItemAttributesSkipsLegacyFlatStorageFields(t *testing.T) {
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Organization: dataitem.OrganizationSingle,
			DataType:     dataitem.DataTypeDocument,
		},
		Attributes: map[string]interface{}{
			"path": "legacy/path",
			"size": int64(10),
			"storage": map[string]interface{}{
				"path":         "legacy/path",
				"content_type": "text/plain",
				"custom":       "ok",
			},
		},
	}
	attrs := map[string]interface{}{
		"storage": map[string]interface{}{
			"bucket": "demo",
			"path":   "docs/",
		},
	}

	MergeDataItemAttributes(attrs, item)

	if attrs["path"] != nil || attrs["size"] != nil || attrs["content_type"] != nil {
		t.Fatalf("legacy flat storage fields should be skipped: %#v", attrs)
	}
	storage := attrs["storage"].(map[string]interface{})
	if storage["bucket"] != "demo" || storage["path"] != "docs/" {
		t.Fatalf("existing storage attrs should win over detected legacy storage keys: %#v", storage)
	}
	if storage["custom"] != "ok" {
		t.Fatalf("custom storage attr = %#v", storage["custom"])
	}
	if attrs["item"] == nil {
		t.Fatalf("item section missing: %#v", attrs)
	}
}
