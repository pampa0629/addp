package authorizationfacts

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestGLTFMultiBindingRoundTripAndLayoutSeparation(t *testing.T) {
	for _, object := range []bool{false, true} {
		for _, embedded := range []bool{false, true} {
			primary := plugin.FileItemPath(33, "models/ 原名 %.gltf")
			buffer := plugin.FileItemPath(33, "models/buffers/literal%20.bin")
			if object {
				primary = plugin.ObjectItemPath(33, "models", " 原名 %.gltf")
				buffer = plugin.ObjectItemPath(33, "models", "buffers/literal%20.bin")
			}
			refs := []FileCompositionRef{{Target: primary, Role: "manifest", Required: true, Primary: true}}
			if !embedded {
				refs = append(refs, FileCompositionRef{Target: buffer, Role: "buffer", Required: true})
			}
			composition, err := NewFileCompositionBoundary("gltf", refs)
			if err != nil {
				t.Fatal(err)
			}
			facts := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: 33, ItemFingerprint: models.GenerateItemFingerprint(33, primary.StringPath()), ItemType: primary.Segments[len(primary.Segments)-1].Term, FullName: primary.StringPath(), ObservedAt: time.Now(), BasisState: AuthorizationBasisComplete, Boundary: &AuthorizationBoundary{Layout: "multi", FileComposition: composition}}
			hash, err := facts.ComputeBoundaryHash()
			if err != nil {
				t.Fatal(err)
			}
			facts.BoundaryHash = &hash
			binding, err := facts.ApprovedBinding()
			if err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(binding)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Binding
			if json.Unmarshal(wire, &decoded) != nil || !reflect.DeepEqual(binding, decoded) {
				t.Fatal("multi approval lost on roundtrip")
			}
			factsWire, err := json.Marshal(facts)
			if err != nil {
				t.Fatal(err)
			}
			var decodedFacts AuthorizationFacts
			if err = json.Unmarshal(factsWire, &decodedFacts); err != nil {
				t.Fatal(err)
			}
			facts.Boundary.Layout = "single"
			if _, err = facts.ComputeBoundaryHash(); err == nil {
				t.Fatal("glTF disguised as single")
			}
			facts.Boundary.Layout = "whole"
			if _, err = facts.ComputeBoundaryHash(); err == nil {
				t.Fatal("glTF expanded to whole")
			}
			composition.Format = "obj"
			facts.Boundary.Layout = "multi"
			if _, err = facts.ComputeBoundaryHash(); err == nil {
				t.Fatal("unadmitted multi accepted")
			}
		}
	}
}

func TestGLTFCompositionRejectsForeignRolesAndOptionalMembers(t *testing.T) {
	primary := plugin.FileItemPath(9, "models/scene.gltf")
	for _, role := range []string{"model", "image", "manifest_1"} {
		if _, err := NewFileCompositionBoundary("gltf", []FileCompositionRef{{Target: primary, Role: role, Required: true, Primary: true}}); err == nil {
			t.Fatal("foreign primary role", role)
		}
	}
	for _, role := range []string{"texture", "manifest", "image_0", "buffer_01"} {
		if _, err := NewFileCompositionBoundary("gltf", []FileCompositionRef{{Target: primary, Role: "manifest", Required: true, Primary: true}, {Target: plugin.FileItemPath(9, "models/a.bin"), Role: role, Required: true}}); err == nil {
			t.Fatal("foreign dependency role", role)
		}
	}
	if _, err := NewFileCompositionBoundary("gltf", []FileCompositionRef{{Target: primary, Role: "manifest", Required: true, Primary: true}, {Target: plugin.FileItemPath(9, "models/a.bin"), Role: "buffer"}}); err == nil {
		t.Fatal("optional member accepted")
	}
}
