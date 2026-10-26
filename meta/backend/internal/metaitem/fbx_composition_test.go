package metaitem

import (
	"github.com/addp/common/format/plugins/fbx"
	"os"
	"path"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestFBXBasicCompositionUsesOnlyTheObservedPrimary(t *testing.T) {
	const primary = "models/model.fbx"
	valid, err := os.ReadFile("../../../../business/fixtures/manager/addp_online_model_fixture.fbx")
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range []bool{false, true} {
		for _, fault := range []string{"none", "truncated", "no-range"} {
			t.Run(map[bool]string{false: "file", true: "object"}[object]+"/"+fault, func(t *testing.T) {
				raw := valid
				if fault == "truncated" {
					raw = []byte("solid triangle\nfacet normal 0 0 1\n")
				}
				reader := &objStructureFixture{refMapContentReader: refMapContentReader{content: map[string][]byte{primary: raw, "models/neighbor.png": []byte("never read")}}}
				input := DirectoryResolveInput{EngineID: 9, DirPath: "models", Options: ResolveOptions{IncludeSingleResources: true}, ContentReader: reader, StructureReader: reader,
					Files: []StorageFileRef{{Name: path.Base(primary), Path: primary, Size: int64(len(raw))}, {Name: "neighbor.png", Path: "models/neighbor.png", Size: 10}}}
				if object {
					input.EngineCatalogPathFor = plugin.ObjectItemPathForBucket(9, "bucket")
				}
				if fault == "no-range" {
					input.StructureReader = nil
				}
				result, err := (&commonDataItemResolver{}).ResolveItems(t.Context(), input)
				if err != nil {
					t.Fatal(err)
				}
				var model *DetectedItem
				for _, item := range result.Items {
					if item.PrimaryContentPath == primary {
						if model != nil {
							t.Fatal("duplicate FBX identity")
						}
						model = item
					}
				}
				if model == nil {
					t.Fatal("FBX identity missing")
				}
				native := model.Attributes["format_info"].(map[string]interface{})["fbx"].(map[string]interface{})
				if native["composition_complete"] != (fault == "none") || native["scan_complete"] != nil || native["texture_refs"] != nil ||
					len(model.RefList) != 1 || model.RefList[0].Path != primary || model.RefList[0].Role != "model" || !model.RefList[0].Required || !model.RefList[0].Primary || reader.sequential != 0 {
					t.Fatalf("FBX basic boundary escaped its exact primary: model=%+v native=%+v reads=%v", model, native, reader.opened)
				}
				for _, name := range reader.opened {
					if name != primary {
						t.Fatal("FBX basic read a neighbor")
					}
				}
				if (fault == "none" && native["profile"] != fbx.Profile) || (fault != "none" && native["profile"] != nil) {
					t.Fatal("FBX profile not bound to basic proof", native)
				}
				if fault == "none" && len(reader.opened) == 0 {
					t.Fatal("composition not based on observed structure")
				}
			})
		}
	}
}
