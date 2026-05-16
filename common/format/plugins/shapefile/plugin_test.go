package shapefile

import (
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
)

func TestDescribeComponentsUsesComponentFormatFacts(t *testing.T) {
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
	if got := byRole["main"].Format; got != format.FormatUnknown {
		t.Fatalf("main component format = %s, want unknown component file format", got)
	}
	if got := byRole["attributes"].Format; got != format.FormatUnknown {
		t.Fatalf("attributes component format = %s, want unknown component file format", got)
	}
	if got := byRole["projection"].Format; got != format.FormatText {
		t.Fatalf("projection format = %s, want text", got)
	}
}
