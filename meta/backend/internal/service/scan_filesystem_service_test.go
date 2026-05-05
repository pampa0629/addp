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
