package metaitem

import (
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/meta/internal/models"
)

func TestFileSystemDetectedItemNameUsesEntryPathForMultiFile(t *testing.T) {
	t.Parallel()

	name, fullName := FileSystemDetectedItemName("/shp", &dataitem.DetectedItem{
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

func TestFileSystemDetectedItemNameKeepsWholeScopePath(t *testing.T) {
	t.Parallel()

	name, fullName := FileSystemDetectedItemName("/lake/sales", &dataitem.DetectedItem{
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

	got := FileSystemSingleFileItemType(&dataitem.DetectedItem{
		Format:       "csv",
		Organization: dataitem.OrganizationSingle,
	})
	if got != "table" {
		t.Fatalf("itemType = %q, want table", got)
	}

	got = FileSystemSingleFileItemType(&dataitem.DetectedItem{
		Format:       "sqlite",
		Organization: dataitem.OrganizationSingle,
	})
	if got != "file" {
		t.Fatalf("container itemType = %q, want file", got)
	}
}

func TestApplyContainerSummaryWritesStandardTypeInfo(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	ApplyContainerSummary(attrs, &dataitem.DetectedItem{
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
