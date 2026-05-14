package shapefile

import (
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
)

func TestDescribeComponentsUsesComponentPreviewSemantics(t *testing.T) {
	descriptors := DescribeComponents([]resource.ComponentRef{
		{
			ResourceRef:   resource.NewResourceRef("roads.shp", resource.ResourceRoleMain),
			ComponentRole: "main",
			Required:      true,
		},
		{
			ResourceRef:   resource.NewResourceRef("roads.dbf", resource.ResourceRoleComponent),
			ComponentRole: "attributes",
			Required:      true,
		},
		{
			ResourceRef:   resource.NewResourceRef("roads.prj", resource.ResourceRoleComponent),
			ComponentRole: "projection",
		},
	})

	byRole := map[string]format.ComponentDescriptor{}
	for _, descriptor := range descriptors {
		byRole[descriptor.Role] = descriptor
	}
	if got := byRole["main"].Format; got == format.FormatShapefile {
		t.Fatalf("main component format = %s, want component's own preview format", got)
	}
	if got := byRole["attributes"].Format; got == format.FormatShapefile {
		t.Fatalf("attributes component format = %s, want component's own preview format", got)
	}
	if got := byRole["projection"].PreviewFormat; got != format.FormatText {
		t.Fatalf("projection preview format = %s, want text", got)
	}
	if got := byRole["projection"].PreviewRenderer; got != "text" {
		t.Fatalf("projection preview renderer = %q, want text", got)
	}
}
