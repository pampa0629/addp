package dataitem

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/addp/common/format/plugins/obj"
)

func TestOBJCompositionRefsUseExactMaterialsAndDistinctSubdirectoryTextures(t *testing.T) {
	libraries := []obj.MaterialLibraryDeclaration{{Reference: "a material.mtl", TextureRefs: []string{"one/same.png", "shared.jpg"}},
		{Reference: "b.mtl", TextureRefs: []string{"two/same.png", "shared.jpg"}}}
	refs, err := OBJCompositionRefs("models/model.obj", libraries)
	if err != nil || len(refs) != 6 {
		t.Fatalf("refs=%#v err=%v", refs, err)
	}
	for i, expected := range []struct{ path, role string }{{"models/model.obj", "model"}, {"models/a material.mtl", "material_library"},
		{"models/b.mtl", "material_library_2"}, {"models/one/same.png", "texture"}, {"models/shared.jpg", "texture_2"}, {"models/two/same.png", "texture_3"}} {
		if refs[i].Path != expected.path || refs[i].Role != expected.role || !refs[i].Required || refs[i].Primary != (i == 0) {
			t.Fatalf("ref[%d]=%#v", i, refs[i])
		}
	}
	knownEmpty := []obj.MaterialLibraryDeclaration{}
	if refs, err := OBJCompositionRefs("model.obj", knownEmpty); err != nil || len(refs) != 1 {
		t.Fatalf("known empty composition: %#v %v", refs, err)
	}
}

func TestOBJCompositionRefsRejectAmbiguousAndOutOfScopeDeclarations(t *testing.T) {
	for _, name := range []string{"nested/a.mtl", "../a.mtl", "./a.mtl", "a.png", "a\\b.mtl", "/a.mtl", "http:a.mtl"} {
		if ValidOBJMaterialLibraryRef(name) {
			t.Fatalf("accepted material %q", name)
		}
	}
	for _, name := range []string{"../a.png", "./a.png", "textures//a.png", "/a.png", "a.tga", "http:a.png", "x\\a.png", "a\n.png"} {
		if refs, err := OBJCompositionRefs("model.obj", []obj.MaterialLibraryDeclaration{{Reference: "a.mtl", TextureRefs: []string{name}}}); err == nil || refs != nil {
			t.Fatalf("accepted texture %q: %#v %v", name, refs, err)
		}
	}
	for _, libs := range [][]obj.MaterialLibraryDeclaration{nil, {{Reference: "a.mtl"}},
		{{Reference: "a.mtl", TextureRefs: []string{"a.png", "a.png"}}},
		{{Reference: "b.mtl", TextureRefs: []string{}}, {Reference: "a.mtl", TextureRefs: []string{}}}} {
		if refs, err := OBJCompositionRefs("model.obj", libs); err == nil || refs != nil {
			t.Fatalf("accepted ambiguous libraries: %#v %v", refs, err)
		}
	}
	libraries := []obj.MaterialLibraryDeclaration{{Reference: "a.mtl", TextureRefs: []string{}}}
	for i := 0; i < 199; i++ {
		libraries[0].TextureRefs = append(libraries[0].TextureRefs, fmt.Sprintf("t%d.png", i))
	}
	if _, err := OBJCompositionRefs("model.obj", libraries); err == nil {
		t.Fatal("primary and material must count toward the 200-ref budget")
	}
}

func TestDecodeOBJLibrariesRequiresCompleteStrictSnapshot(t *testing.T) {
	for _, raw := range []any{nil, []any{map[string]any{"reference": "a.mtl"}},
		[]any{map[string]any{"reference": "a.mtl", "texture_refs": []string{}, "extra": true}}} {
		if _, err := DecodeOBJMaterialLibraries(raw); err == nil {
			t.Fatalf("accepted partial snapshot %#v", raw)
		}
	}
	libs, err := DecodeOBJMaterialLibraries([]any{map[string]any{"reference": "a.mtl", "texture_refs": []string{}}})
	if err != nil || !reflect.DeepEqual(libs, []obj.MaterialLibraryDeclaration{{Reference: "a.mtl", TextureRefs: []string{}}}) {
		t.Fatalf("empty MTL declaration lost: %#v %v", libs, err)
	}
}
