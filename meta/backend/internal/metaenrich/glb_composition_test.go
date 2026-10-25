package metaenrich

import (
	"encoding/binary"
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

func TestGLBDeepGeometryNeverCreatesOrPromotesBasicComposition(t *testing.T) {
	doc := []byte(`{"asset":{"version":"2.0"},"scenes":[{}]}`)
	for len(doc)%4 != 0 {
		doc = append(doc, ' ')
	}
	raw := []byte{'g', 'l', 'T', 'F'}
	raw = binary.LittleEndian.AppendUint32(raw, 2)
	raw = binary.LittleEndian.AppendUint32(raw, uint32(20+len(doc)))
	raw = binary.LittleEndian.AppendUint32(raw, uint32(len(doc)))
	raw = binary.LittleEndian.AppendUint32(raw, 0x4e4f534a)
	raw = append(raw, doc...)

	for _, kind := range []string{"ftp", "nfs", "minio"} {
		for _, basis := range []string{"absent", "incomplete", "complete"} {
			t.Run(fmt.Sprintf("%s/%s", kind, basis), func(t *testing.T) {
				attrs := models.JSONMap{}
				if basis != "absent" {
					attrs["format_info"] = map[string]interface{}{"glb": map[string]interface{}{"composition_complete": basis == "complete"}}
				}
				item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{DataType: datatype.Model3D, Layout: format.LayoutSingle, Format: "glb", PrimaryContentPath: "models/model.glb"}}
				if err := EnrichSingleModel3DItem(t.Context(), attrs, bytesContentReader{content: raw}, nil, 9, &commonModels.Engine{ID: 9, EngineType: kind}, item, "models/model.glb", plugin.FileItemPathForEngine(9)); err != nil {
					t.Fatal(err)
				}
				native := commonJSON.Section(attrs, "format_info.glb")
				want := interface{}(nil)
				if basis != "absent" {
					want = basis == "complete"
				}
				if native["composition_complete"] != want || commonJSON.InterfaceInt64(native["glb_version"]) != 2 {
					t.Fatalf("deep geometry promoted or lost independent basic facts: %+v", attrs)
				}
			})
		}
	}
}
