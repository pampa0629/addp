package scanflow

import (
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/models"
)

func TestValidateKnownItemRefreshDescriptorRequiresMultiRefs(t *testing.T) {
	err := ValidateKnownItemRefreshDescriptor(dataitem.ItemDescriptor{
		Layout:   format.LayoutMulti,
		DataType: datatype.Table,
		Format:   "shapefile",
	}, &models.MetaItem{Name: "roads.shp"})
	if err == nil {
		t.Fatal("expected incomplete multi item error")
	}
}

func TestKnownItemObjectPathRemovesBucketPrefix(t *testing.T) {
	got := KnownItemObjectPath(dataitem.ItemDescriptor{
		StorageBucket: "addp",
	}, "addp/gis/roads.shp")
	if got != "gis/roads.shp" {
		t.Fatalf("object path = %q", got)
	}
}

func TestKnownWholeItemPhysicalPathPrefersItemFullName(t *testing.T) {
	descriptor := dataitem.DescriptorFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"layout": string(format.LayoutWhole),
			"refs": []map[string]interface{}{
				{"path": "3d/baita/metadata.xml", "role": "manifest", "primary": true},
			},
		},
		"storage": map[string]interface{}{
			"physical_path": "3d/baita/metadata.xml",
			"path":          "3d/baita/",
			"name":          "metadata.xml",
		},
	})
	item := &models.MetaItem{FullName: "3d/baita"}

	if got := KnownItemPhysicalPath(descriptor, item); got != "3d/baita" {
		t.Fatalf("physical path = %q, want whole item full_name root", got)
	}
	if got := KnownItemPrimaryContentPath(descriptor, item); got != "3d/baita/metadata.xml" {
		t.Fatalf("primary content path = %q, want manifest ref", got)
	}
}

func TestKnownItemCatalogPathResolverUsesDescriptorBucket(t *testing.T) {
	resolver := KnownItemCatalogPathResolver(9, nil, dataitem.ItemDescriptor{
		StorageBucket: "addp",
	})

	if got := resolver("addp/gis/roads.shp").StringPath(); got != "addp/gis/roads.shp" {
		t.Fatalf("catalog path = %q", got)
	}
}
