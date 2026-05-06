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
	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	if table["fields"] == nil {
		t.Fatalf("type_info.table.fields missing: %#v", table)
	}
}

func TestInferDetectedItemNameUsesEntryPathForMultiFile(t *testing.T) {
	t.Parallel()

	name, fullName := inferDetectedItemName("/shp", &dataitem.DetectedItem{
		Organization: dataitem.OrganizationMulti,
		EntryPath:    "/shp/farmland.shp",
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
		Organization: dataitem.OrganizationSingle,
		EntryPath:    "/lake/sales.parquet",
	})

	if name != "sales.parquet" {
		t.Fatalf("name = %q, want sales.parquet", name)
	}
	if fullName != "lake/sales.parquet" {
		t.Fatalf("fullName = %q, want lake/sales.parquet", fullName)
	}
}

func TestInferDetectedItemNameKeepsWholeScopePath(t *testing.T) {
	t.Parallel()

	name, fullName := inferDetectedItemName("/lake/sales", &dataitem.DetectedItem{
		Organization: dataitem.OrganizationWhole,
		EntryPath:    "/lake/sales/_metadata",
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
		Format:       "csv",
		Organization: dataitem.OrganizationSingle,
	})
	if got != "table" {
		t.Fatalf("itemType = %q, want table", got)
	}

	got = fileSystemSingleFileItemType(&dataitem.DetectedItem{
		Format:       "sqlite",
		Organization: dataitem.OrganizationSingle,
	})
	if got != "file" {
		t.Fatalf("container itemType = %q, want file", got)
	}

	got = fileSystemSingleFileItemType(&dataitem.DetectedItem{
		Format:       "parquet",
		Organization: dataitem.OrganizationSingle,
	})
	if got != "lake_table" {
		t.Fatalf("parquet itemType = %q, want lake_table", got)
	}
}

func TestApplyContainerSummaryWritesStandardTypeInfo(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	applyContainerSummary(attrs, &dataitem.DetectedItem{
		DataType: dataitem.DataTypeContainer,
	})

	typeInfo := attrs["type_info"].(map[string]interface{})
	container := typeInfo["container"].(map[string]interface{})
	if container["child_count"] != 0 || container["resource_count"] != 1 {
		t.Fatalf("type_info.container = %#v", container)
	}
	if _, ok := container["children"]; !ok {
		t.Fatalf("type_info.container.children missing: %#v", container)
	}
}
