package authorizationfacts

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestENVIBoundaryIsAnExactMediaPair(t *testing.T) {
	for _, object := range []bool{false, true} {
		primary := plugin.FileItemPath(14, "imagery/ 原名 %.img")
		header := plugin.FileItemPath(14, "imagery/ 原名 %.hdr")
		if object {
			primary = plugin.ObjectItemPath(14, "bucket", "imagery/ 原名 %.img")
			header = plugin.ObjectItemPath(14, "bucket", "imagery/ 原名 %.hdr")
		}
		refs := []FileCompositionRef{{Target: primary, Role: "raster", Required: true, Primary: true}, {Target: header, Role: "header", Required: true}}
		boundary, err := NewFileCompositionBoundary("envi", refs)
		if err != nil || boundary.DataType() != "media" || boundary.Layout() != "multi" || boundary.PrimaryRole() != "raster" {
			t.Fatalf("incorrect ENVI identity: %#v %v", boundary, err)
		}
		facts := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: 14, ItemFingerprint: models.GenerateItemFingerprint(14, primary.StringPath()), ItemType: primary.Segments[len(primary.Segments)-1].Term, FullName: primary.StringPath(), ObservedAt: time.Now(), BasisState: AuthorizationBasisComplete, Boundary: &AuthorizationBoundary{Layout: "multi", FileComposition: boundary}}
		hash, err := facts.ComputeBoundaryHash()
		if err != nil {
			t.Fatal(err)
		}
		facts.BoundaryHash = &hash
		binding, err := facts.ApprovedBinding()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(binding)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Binding
		if err := json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(binding, decoded) {
			t.Fatalf("ENVI binding roundtrip failed: %v", err)
		}
		facts.Boundary.Layout = "single"
		if _, err := facts.ComputeBoundaryHash(); err == nil {
			t.Fatal("pair reduced to main-only boundary")
		}
		for name, mutate := range map[string]func([]FileCompositionRef) []FileCompositionRef{
			"main only":         func(r []FileCompositionRef) []FileCompositionRef { return r[:1] },
			"model role":        func(r []FileCompositionRef) []FileCompositionRef { r[0].Role = "model"; return r },
			"optional header":   func(r []FileCompositionRef) []FileCompositionRef { r[1].Required = false; return r },
			"wrong header role": func(r []FileCompositionRef) []FileCompositionRef { r[1].Role = "texture"; return r },
			"nested header": func(r []FileCompositionRef) []FileCompositionRef {
				if object {
					r[1].Target = plugin.ObjectItemPath(14, "bucket", "imagery/sub/data.hdr")
				} else {
					r[1].Target = plugin.FileItemPath(14, "imagery/sub/data.hdr")
				}
				return r
			},
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := NewFileCompositionBoundary("envi", mutate(append([]FileCompositionRef{}, refs...))); err == nil {
					t.Fatal("invalid ENVI boundary accepted")
				}
			})
		}
	}
}
