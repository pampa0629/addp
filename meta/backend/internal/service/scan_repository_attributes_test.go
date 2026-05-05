package service

import (
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestNormalizeMetaItemAttributesBuildsPartitionedCore(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{
		"bucket":           "addp",
		"path":             "roads/",
		"name":             "roads.geojson",
		"content_type":     "application/geo+json",
		"composition_type": "single_file",
		"data_family":      "tabular",
		"format":           "geojson",
		"entry_path":       "addp/roads/roads.geojson",
		"component_files":  []string{"addp/roads/roads.geojson"},
		"fields":           []map[string]interface{}{{"name": "id", "data_type": "integer"}},
	}

	normalized := normalizeMetaItemAttributes(attrs)

	if normalized["schema_version"] != 1 {
		t.Fatalf("schema_version = %v, want 1", normalized["schema_version"])
	}
	item := normalized["item"].(map[string]interface{})
	if item["data_family"] != "tabular" || item["format"] != "geojson" {
		t.Fatalf("item section = %#v, want tabular geojson", item)
	}
	storage := normalized["storage"].(map[string]interface{})
	if storage["bucket"] != "addp" || storage["path"] != "roads/" {
		t.Fatalf("storage section = %#v, want bucket/path", storage)
	}
	schema := normalized["schema"].(map[string]interface{})
	if _, ok := schema["fields"]; !ok {
		t.Fatalf("schema section = %#v, want fields", schema)
	}
}

func TestNormalizeMetaItemAttributesMigratesLegacySchemaName(t *testing.T) {
	t.Parallel()

	normalized := normalizeMetaItemAttributes(models.JSONMap{
		"schema":     "public",
		"table_type": "BASE TABLE",
	})

	if normalized["schema"] == "public" {
		t.Fatalf("top-level schema should be partition map, got legacy string")
	}
	if normalized["schema_name"] != "public" {
		t.Fatalf("schema_name = %v, want public", normalized["schema_name"])
	}
	storage := normalized["storage"].(map[string]interface{})
	if storage["schema_name"] != "public" {
		t.Fatalf("storage.schema_name = %v, want public", storage["schema_name"])
	}
	item := normalized["item"].(map[string]interface{})
	if item["namespace"] != "public" {
		t.Fatalf("item.namespace = %v, want public", item["namespace"])
	}
	schema := normalized["schema"].(map[string]interface{})
	if schema["table_type"] != "BASE TABLE" {
		t.Fatalf("schema.table_type = %v, want BASE TABLE", schema["table_type"])
	}
}

func TestNormalizeMetaItemAttributesBuildsSpatialExtension(t *testing.T) {
	t.Parallel()

	spatial := map[string]interface{}{"geometry_column": "shape"}
	normalized := normalizeMetaItemAttributes(models.JSONMap{
		"spatial_metadata": spatial,
	})

	extensions := normalized["extensions"].(map[string]interface{})
	spatialExtension := extensions["spatial"].(map[string]interface{})
	gotSpatial := spatialExtension["spatial_metadata"].(map[string]interface{})
	if gotSpatial["geometry_column"] != "shape" {
		t.Fatalf("extensions.spatial = %#v, want spatial metadata", spatialExtension)
	}
}

func TestUpsertSectionMergesExistingSection(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{
		"schema": map[string]interface{}{
			"fields": []interface{}{"id"},
		},
	}

	upsertSection(attrs, "schema", map[string]interface{}{
		"table_type": "BASE TABLE",
	})

	schema := attrs["schema"].(map[string]interface{})
	if schema["table_type"] != "BASE TABLE" {
		t.Fatalf("schema.table_type = %v, want BASE TABLE", schema["table_type"])
	}
	if _, ok := schema["fields"]; !ok {
		t.Fatalf("schema.fields missing after merge: %#v", schema)
	}
}

func TestSetAttributeHelpersWritePartitionAndFlatCompatibility(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	setStorageAttribute(attrs, "physical_path", "bucket/data.parquet")
	setItemAttribute(attrs, "format", "parquet")

	if attrs["physical_path"] != "bucket/data.parquet" || attrs["format"] != "parquet" {
		t.Fatalf("flat compatibility fields missing: %#v", attrs)
	}
	storage := attrs["storage"].(map[string]interface{})
	if storage["physical_path"] != "bucket/data.parquet" {
		t.Fatalf("storage.physical_path = %v, want bucket/data.parquet", storage["physical_path"])
	}
	item := attrs["item"].(map[string]interface{})
	if item["format"] != "parquet" {
		t.Fatalf("item.format = %v, want parquet", item["format"])
	}
}

func TestSetExtensionAttributeWritesNamespaceAndFlatCompatibility(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	spatial := map[string]interface{}{"geometry_column": "geom"}

	setExtensionAttribute(attrs, "spatial", "spatial_metadata", spatial)

	if attrs["spatial_metadata"] == nil {
		t.Fatalf("flat spatial_metadata missing: %#v", attrs)
	}
	extensions := attrs["extensions"].(map[string]interface{})
	spatialExt := extensions["spatial"].(map[string]interface{})
	if spatialExt["spatial_metadata"] == nil {
		t.Fatalf("extensions.spatial.spatial_metadata missing: %#v", spatialExt)
	}
}
