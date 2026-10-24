package metaenrich

import (
	"fmt"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

func TestSTLDeepGeometryNeverCreatesOrPromotesBasicComposition(t *testing.T) {
	raw := []byte("solid triangle\nfacet normal 0 0 1\nouter loop\nvertex 0 0 0\nvertex 2 0 0\nvertex 0 1 0\nendloop\nendfacet\nendsolid triangle\n")
	for _, kind := range []string{"ftp", "nfs", "minio"} {
		for _, basis := range []string{"absent", "incomplete", "complete"} {
			t.Run(fmt.Sprintf("%s/%s", kind, basis), func(t *testing.T) {
				attrs := models.JSONMap{}
				if basis != "absent" {
					attrs["format_info"] = map[string]interface{}{"stl": map[string]interface{}{"composition_complete": basis == "complete"}}
				}
				item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{DataType: datatype.Model3D, Layout: format.LayoutSingle, Format: "stl", PrimaryContentPath: "models/model.stl"}}
				if err := EnrichSingleModel3DItem(t.Context(), attrs, bytesContentReader{content: raw}, nil, 9, &commonModels.Engine{ID: 9, EngineType: kind}, item, "models/model.stl", plugin.FileItemPathForEngine(9)); err != nil {
					t.Fatal(err)
				}
				native := commonJSON.Section(attrs, "format_info.stl")
				want := interface{}(nil)
				if basis != "absent" {
					want = basis == "complete"
				}
				if native["composition_complete"] != want || native["scan_complete"] != true || commonJSON.InterfaceInt64(native["triangle_count"]) != 1 {
					t.Fatalf("deep geometry promoted or lost independent basic facts: %+v", attrs)
				}
			})
		}
	}
}
