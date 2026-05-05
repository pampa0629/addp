package service

import (
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/meta/internal/models"
)

func TestSetSchemaFieldsWritesPartitionOnly(t *testing.T) {
	t.Parallel()

	fields := []map[string]interface{}{{"name": "id", "type": "integer"}}
	attrs := models.JSONMap{}

	setSchemaFields(attrs, fields)

	if attrs["fields"] != nil {
		t.Fatalf("flat fields should not be written: %#v", attrs)
	}
	schema := attrs["schema"].(map[string]interface{})
	if schema["fields"] == nil {
		t.Fatalf("schema.fields missing: %#v", schema)
	}
}

func TestInferDetectedItemNameUsesEntryPathForMultiFile(t *testing.T) {
	t.Parallel()

	name, fullName := inferDetectedItemName("/shp", &dataitem.DetectedItem{
		CompositionType: dataitem.CompositionTypeMultiFile,
		EntryPath:       "/shp/farmland.shp",
	})

	if name != "farmland.shp" {
		t.Fatalf("name = %q, want farmland.shp", name)
	}
	if fullName != "shp/farmland.shp" {
		t.Fatalf("fullName = %q, want shp/farmland.shp", fullName)
	}
}

func TestInferDetectedItemNameUsesEntryPathForSingleFile(t *testing.T) {
	t.Parallel()

	name, fullName := inferDetectedItemName("/lake", &dataitem.DetectedItem{
		CompositionType: dataitem.CompositionTypeSingleFile,
		EntryPath:       "/lake/sales.parquet",
	})

	if name != "sales.parquet" {
		t.Fatalf("name = %q, want sales.parquet", name)
	}
	if fullName != "lake/sales.parquet" {
		t.Fatalf("fullName = %q, want lake/sales.parquet", fullName)
	}
}

func TestInferDetectedItemNameKeepsDirectoryTreePath(t *testing.T) {
	t.Parallel()

	name, fullName := inferDetectedItemName("/lake/sales", &dataitem.DetectedItem{
		CompositionType: dataitem.CompositionTypeDirectoryTree,
		EntryPath:       "/lake/sales/_metadata",
	})

	if name != "sales" {
		t.Fatalf("name = %q, want sales", name)
	}
	if fullName != "lake/sales" {
		t.Fatalf("fullName = %q, want lake/sales", fullName)
	}
}

func TestFileSystemSingleFileItemTypeUsesBuiltinRule(t *testing.T) {
	t.Parallel()

	got := fileSystemSingleFileItemType(&dataitem.DetectedItem{
		Format:          "csv",
		CompositionType: dataitem.CompositionTypeSingleFile,
	})
	if got != "table" {
		t.Fatalf("itemType = %q, want table", got)
	}

	got = fileSystemSingleFileItemType(&dataitem.DetectedItem{
		Format:          "sqlite",
		CompositionType: dataitem.CompositionTypeContainerFile,
	})
	if got != "file" {
		t.Fatalf("container itemType = %q, want file", got)
	}

	got = fileSystemSingleFileItemType(&dataitem.DetectedItem{
		Format:          "parquet",
		CompositionType: dataitem.CompositionTypeSingleFile,
	})
	if got != "lake_table" {
		t.Fatalf("parquet itemType = %q, want lake_table", got)
	}
}
