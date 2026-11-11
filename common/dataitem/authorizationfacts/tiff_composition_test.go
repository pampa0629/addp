package authorizationfacts

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestTIFFScannedCompositionPreservesPAM(t *testing.T) {
	for _, object := range []bool{false, true} {
		target := func(name string) plugin.EngineCatalogPath {
			if object {
				return plugin.ObjectItemPath(33, "images", name)
			}
			return plugin.FileItemPath(33, "images/"+name)
		}
		refs := []FileCompositionRef{
			{Target: target("a.tif"), Role: "image", Required: true, Primary: true},
			{Target: target("a.tif.aux.xml"), Role: "auxiliary_metadata", Required: false},
		}
		boundary, err := NewScannedFileCompositionBoundary("tiff", "multi", refs)
		if err != nil {
			t.Fatalf("Meta-registered PAM rejected: %v", err)
		}
		encoded, err := json.Marshal(boundary)
		if err != nil {
			t.Fatal(err)
		}
		var decoded FileCompositionBoundary
		if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(boundary, &decoded) {
			t.Fatalf("composition changed in wire roundtrip: %v", err)
		}
		if boundary.Layout() != "multi" || len(boundary.Refs) != 2 || boundary.Refs[1].Required {
			t.Fatal("Meta layout or optional PAM was rewritten")
		}
		for _, change := range []func(*FileCompositionBoundary){
			func(b *FileCompositionBoundary) { b.Refs[1].Target = target("b.tif.aux.xml") },
			func(b *FileCompositionBoundary) { b.Refs[1].Target = target("sub/a.tif.aux.xml") },
			func(b *FileCompositionBoundary) { b.Refs[1].Role = "texture" },
			func(b *FileCompositionBoundary) { b.Refs[1].Primary = true },
			func(b *FileCompositionBoundary) { b.Refs[0].Required = false },
		} {
			bad := boundary.Clone()
			change(bad)
			if bad.Validate(33) == nil {
				t.Fatalf("invalid TIFF/PAM contract accepted: %+v", bad)
			}
		}
		// Same physical refs with a different recorded layout are distinct facts.
		single, err := NewScannedFileCompositionBoundary("tiff", "single", refs[:1])
		if err != nil || single.Layout() != "single" {
			t.Fatal("single-file contract changed", err)
		}
		oneRefMulti, err := NewScannedFileCompositionBoundary("tiff", "multi", refs[:1])
		if err != nil || oneRefMulti.Layout() != "multi" {
			t.Fatal("layout inferred from file count", err)
		}
	}
}
