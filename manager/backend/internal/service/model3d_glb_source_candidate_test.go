package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

func model3DGLBCandidateFixture(format string) (Model3DGLBSourceConfig, *commonModels.MetaItem) {
	item := &commonModels.MetaItem{ID: 41, TenantID: 7, EngineID: 33, ItemType: "file", Name: "model." + format, FullName: "models/model." + format, Fingerprint: "scanned-model"}
	refs := []map[string]any{{"path": item.FullName, "role": "model", "required": true, "primary": true}}
	if format == "3ds" || format == "dae" || format == "obj" {
		refs = append(refs, map[string]any{"path": "models/TEST.PNG", "role": "texture", "required": true})
	}
	if format == "obj" {
		refs = append(refs, map[string]any{"path": "models/model.mtl", "role": "material_library", "required": true})
	}
	item.Attributes = map[string]any{
		"item":        map[string]any{"layout": "single", "data_type": "model_3d", "format": format, "refs": refs},
		"storage":     map[string]any{"physical_path": item.FullName, "total_size": 999999},
		"format_info": map[string]any{format: map[string]any{"scan_complete": true, "texture_refs": []string{"TEST.PNG"}}},
	}
	if format == "stl" {
		item.Attributes["format_info"] = map[string]any{"stl": map[string]any{"composition_complete": true}}
	}
	if format == "obj" {
		item.Attributes["format_info"] = map[string]any{"obj": map[string]any{"composition_complete": true, "material_libraries": []map[string]any{{"reference": "model.mtl", "texture_refs": []string{"TEST.PNG"}}}}}
	}
	if format == "gltf" {
		refs[0]["role"] = "manifest"
		refs = append(refs, map[string]any{"path": "models/buffers/literal%20.bin", "role": "buffer", "required": true}, map[string]any{"path": "models/textures/ TEST.PNG", "role": "image", "required": true})
		item.Attributes["item"].(map[string]any)["layout"] = "multi"
		item.Attributes["item"].(map[string]any)["refs"] = refs
		item.Attributes["format_info"] = map[string]any{"gltf": map[string]any{"composition_complete": true}}
	}
	locator := resourcetree.LocatorFromFullName(item.EngineID, "ftp", item.ItemType, item.FullName, &item.ID)
	return Model3DGLBSourceConfig{ItemLocator: locator.ToURI(), ItemID: item.ID, SourceEngineID: item.EngineID, ItemFingerprint: item.Fingerprint, Format: format}, item
}

func TestModel3DGLBCandidateRetainsExactRefsAndOriginalDeclarations(t *testing.T) {
	source, item := model3DGLBCandidateFixture("3ds")
	candidate, err := model3DGLBCandidateFromMeta(7, source, item)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.composition.Refs) != 2 || !reflect.DeepEqual(candidate.declarations, []string{"TEST.PNG"}) {
		t.Fatalf("candidate=%+v", candidate)
	}
	encoded, err := json.Marshal(candidate.composition)
	if err != nil {
		t.Fatal(err)
	}
	item.Attributes["storage"].(map[string]any)["total_size"] = 1
	item.Attributes["format_info"].(map[string]any)["3ds"].(map[string]any)["texture_refs"].([]string)[0] = "changed.png"
	if !reflect.DeepEqual(candidate.declarations, []string{"TEST.PNG"}) {
		t.Fatal("candidate aliases mutable Meta response")
	}
	again, _ := json.Marshal(candidate.composition)
	if string(encoded) != string(again) {
		t.Fatal("I/O size changed the candidate boundary")
	}
}

func TestOBJModelCandidateRequiresFullMaterialClosureAndCopiesNativeFacts(t *testing.T) {
	source, item := model3DGLBCandidateFixture("obj")
	candidate, err := model3DGLBCandidateFromMeta(7, source, item)
	if err != nil || len(candidate.composition.Refs) != 3 || len(candidate.libraries) != 1 {
		t.Fatalf("candidate=%+v err=%v", candidate, err)
	}
	native := item.Attributes["format_info"].(map[string]any)["obj"].(map[string]any)
	native["material_libraries"].([]map[string]any)[0]["texture_refs"].([]string)[0] = "changed.png"
	if candidate.libraries[0].TextureRefs[0] != "TEST.PNG" {
		t.Fatal("candidate aliases mutable declarations")
	}
	if _, err := model3DGLBCandidateFromMeta(7, source, item); err == nil {
		t.Fatal("changed declaration accepted without new exact refs")
	}
	for _, field := range []string{"composition_complete", "material_libraries"} {
		source, item := model3DGLBCandidateFixture("obj")
		delete(item.Attributes["format_info"].(map[string]any)["obj"].(map[string]any), field)
		if _, err := model3DGLBCandidateFromMeta(7, source, item); err == nil {
			t.Fatalf("incomplete %s accepted", field)
		}
	}
}

func TestModel3DGLBCandidateRejectsUnknownAndMalformedFacts(t *testing.T) {
	for _, name := range []string{"tenant", "item", "engine", "fingerprint", "locator", "layout", "format", "physical path", "nil refs", "duplicate", "contradictory", "missing required", "optional", "outside", "wrong primary", "unknown declaration", "unfinished declaration", "nil declaration"} {
		t.Run(name, func(t *testing.T) {
			source, item := model3DGLBCandidateFixture("3ds")
			attrs := item.Attributes["item"].(map[string]any)
			refs := attrs["refs"].([]map[string]any)
			native := item.Attributes["format_info"].(map[string]any)["3ds"].(map[string]any)
			switch name {
			case "tenant":
				item.TenantID++
			case "item":
				item.ID++
			case "engine":
				item.EngineID++
			case "fingerprint":
				item.Fingerprint = "other"
			case "locator":
				source.ItemLocator = "addp://engine/33/path/models/other.3ds?type=file"
			case "layout":
				attrs["layout"] = "whole"
			case "format":
				attrs["format"] = "dae"
			case "physical path":
				item.Attributes["storage"].(map[string]any)["physical_path"] = "models"
			case "nil refs":
				attrs["refs"] = nil
			case "duplicate":
				attrs["refs"] = append(refs, refs[1])
			case "contradictory":
				attrs["refs"] = append(refs, map[string]any{"path": "models/TEST.PNG", "role": "other", "required": true})
			case "missing required":
				delete(refs[1], "required")
			case "optional":
				refs[1]["required"] = false
			case "outside":
				refs[1]["path"] = "outside/TEST.PNG"
			case "wrong primary":
				refs[1]["primary"] = true
			case "unknown declaration":
				delete(native, "texture_refs")
			case "unfinished declaration":
				native["scan_complete"] = false
			case "nil declaration":
				native["texture_refs"] = nil
			}
			if candidate, err := model3DGLBCandidateFromMeta(7, source, item); candidate != nil || !errors.Is(err, ErrModel3DGLBSourceAuthorizationRequired) {
				t.Fatalf("candidate=%+v err=%v", candidate, err)
			}
		})
	}
}

func TestModel3DGLBCandidateDistinguishesNoDeclaredTexturesFromUnknown(t *testing.T) {
	source, item := model3DGLBCandidateFixture("3ds")
	attrs := item.Attributes["item"].(map[string]any)
	attrs["refs"] = attrs["refs"].([]map[string]any)[:1]
	item.Attributes["format_info"].(map[string]any)["3ds"].(map[string]any)["texture_refs"] = []string{}
	candidate, err := model3DGLBCandidateFromMeta(7, source, item)
	if err != nil || candidate.declarations == nil || len(candidate.declarations) != 0 || len(candidate.composition.Refs) != 1 {
		t.Fatalf("candidate=%+v err=%v", candidate, err)
	}
	source, item = model3DGLBCandidateFixture("ifc")
	delete(item.Attributes["item"].(map[string]any), "refs")
	candidate, err = model3DGLBCandidateFromMeta(7, source, item)
	if err != nil || len(candidate.composition.Refs) != 1 {
		t.Fatalf("IFC candidate=%+v err=%v", candidate, err)
	}
}

func TestSTLModelCandidateRequiresBasicCompletenessAndExactSinglePrimary(t *testing.T) {
	for _, fault := range []string{"none", "deep-only", "extra", "missing-primary"} {
		t.Run(fault, func(t *testing.T) {
			source, item := model3DGLBCandidateFixture("stl")
			facts := item.Attributes["item"].(map[string]any)
			switch fault {
			case "deep-only":
				item.Attributes["format_info"] = map[string]any{"stl": map[string]any{"scan_complete": true}}
			case "extra":
				facts["refs"] = append(facts["refs"].([]map[string]any), map[string]any{"path": "models/neighbor.png", "role": "texture", "required": true})
			case "missing-primary":
				delete(facts, "refs")
			}
			candidate, err := model3DGLBCandidateFromMeta(7, source, item)
			if fault != "none" {
				if err == nil {
					t.Fatal("incomplete STL composition admitted")
				}
				return
			}
			if err != nil || len(candidate.composition.Refs) != 1 || len(candidate.declarations) != 0 || len(candidate.libraries) != 0 {
				t.Fatalf("candidate=%+v err=%v", candidate, err)
			}
		})
	}
}
