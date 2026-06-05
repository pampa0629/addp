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

func TestKnownItemCatalogPathResolverUsesDescriptorBucket(t *testing.T) {
	resolver := KnownItemCatalogPathResolver(9, nil, dataitem.ItemDescriptor{
		StorageBucket: "addp",
	})

	if got := resolver("addp/gis/roads.shp").StringPath(); got != "addp/gis/roads.shp" {
		t.Fatalf("catalog path = %q", got)
	}
}
