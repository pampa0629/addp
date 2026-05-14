package metacatalog

import (
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

func TestFileCatalogDetectedItemNameUsesEntryPathForMultiFile(t *testing.T) {
	t.Parallel()

	name, fullName := FileCatalogDetectedItemName("/shp", &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Organization: dataitem.OrganizationMulti,
			EntryPath:    "/shp/farmland.shp",
		},
	})

	if name != "farmland.shp" {
		t.Fatalf("name = %q, want farmland.shp", name)
	}
	if fullName != "shp/farmland.shp" {
		t.Fatalf("fullName = %q, want shp/farmland.shp", fullName)
	}
}

func TestFileCatalogDetectedItemNameKeepsWholeScopePath(t *testing.T) {
	t.Parallel()

	name, fullName := FileCatalogDetectedItemName("/lake/sales", &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Organization: dataitem.OrganizationWhole,
			EntryPath:    "/lake/sales/_metadata",
		},
	})

	if name != "sales" {
		t.Fatalf("name = %q, want sales", name)
	}
	if fullName != "lake/sales" {
		t.Fatalf("fullName = %q, want lake/sales", fullName)
	}
}

func TestApplyContainerSummaryWritesStandardTypeInfo(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	ApplyContainerSummary(attrs, &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{DataType: dataitem.DataTypeContainer},
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
