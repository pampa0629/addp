package metaenrich

import (
	"fmt"
	"github.com/addp/common/format/plugins/fbx"
	"os"
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

func TestFBXDeepGeometryNeverCreatesOrPromotesBasicComposition(t *testing.T) {
	raw, err := os.ReadFile("../../../../business/fixtures/manager/addp_online_model_fixture.fbx")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"ftp", "nfs", "minio"} {
		for _, basis := range []string{"absent", "incomplete", "complete"} {
			t.Run(fmt.Sprintf("%s/%s", kind, basis), func(t *testing.T) {
				attrs := models.JSONMap{}
				if basis != "absent" {
					attrs["format_info"] = map[string]interface{}{"fbx": map[string]interface{}{"composition_complete": basis == "complete"}}
				}
				if basis == "complete" {
					attrs["format_info"].(map[string]interface{})["fbx"].(map[string]interface{})["profile"] = fbx.Profile
				}
				item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{DataType: datatype.Model3D, Layout: format.LayoutSingle, Format: "fbx", PrimaryContentPath: "models/model.fbx"}}
				if err := EnrichSingleModel3DItem(t.Context(), attrs, bytesContentReader{content: raw}, nil, 9, &commonModels.Engine{ID: 9, EngineType: kind}, item, "models/model.fbx", plugin.FileItemPathForEngine(9)); err != nil {
					t.Fatal(err)
				}
				native := commonJSON.Section(attrs, "format_info.fbx")
				want := interface{}(nil)
				if basis != "absent" {
					want = basis == "complete"
				}
				profile := interface{}(nil)
				if basis == "complete" {
					profile = fbx.Profile
				}
				if native["composition_complete"] != want || native["profile"] != profile {
					t.Fatalf("deep geometry promoted or lost independent basic facts: %+v", attrs)
				}
			})
		}
	}
}
