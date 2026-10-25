package dataitem

import (
	"reflect"
	"testing"

	"github.com/addp/common/format"
)

func TestGLTFCompositionUsesAlreadyDecodedPathsAndStableRoles(t *testing.T) {
	resources := []format.GLTFResourceRef{{URI: "textures/%20a%2520.PNG", Path: "textures/ a%20.PNG", Role: "image"}, {URI: "buffers/mesh", Path: "buffers/mesh", Role: "buffer"}, {URI: "buffers/mesh", Path: "buffers/mesh", Role: "buffer"}}
	refs, err := GLTFCompositionRefs("models/ 原名 %.gltf", resources)
	if err != nil || len(refs) != 3 {
		t.Fatal(refs, err)
	}
	if refs[0].Role != "manifest" || refs[1].Path != "models/buffers/mesh" || refs[2].Path != "models/textures/ a%20.PNG" {
		t.Fatal("names or roles changed", refs)
	}
	resources[0], resources[2] = resources[2], resources[0]
	again, err := GLTFCompositionRefs("models/ 原名 %.gltf", resources)
	if err != nil || !reflect.DeepEqual(refs, again) {
		t.Fatal("reference order changed boundary", again, err)
	}
	for _, name := range []string{"../secret", "/secret", "a/../secret", "a//secret", "a\\secret", "a\x00secret"} {
		if _, err := GLTFCompositionRefs("models/scene.gltf", []format.GLTFResourceRef{{Path: name, Role: "buffer"}}); err == nil {
			t.Fatal("unsafe path", name)
		}
	}
	if _, err := GLTFCompositionRefs("scene.gltf", []format.GLTFResourceRef{{Path: "a.bin", Role: "buffer"}, {Path: "a.bin", Role: "image"}}); err == nil {
		t.Fatal("conflicting resource roles")
	}
	if refs, err := GLTFCompositionRefs("scene.gltf", []format.GLTFResourceRef{}); err != nil || len(refs) != 1 {
		t.Fatal("embedded glTF lost multi identity", err)
	}
}
