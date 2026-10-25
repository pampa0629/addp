package engineaccess

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"gorm.io/gorm"
)

func exerciseGLTFCompositionSQL(t *testing.T, db *gorm.DB) {
	for _, object := range []bool{false, true} {
		primary := plugin.FileItemPath(9, "models/scene.gltf")
		buffer := plugin.FileItemPath(9, "models/buffers/ a%20.mesh")
		if object {
			primary = plugin.ObjectItemPath(9, "models", "scene.gltf")
			buffer = plugin.ObjectItemPath(9, "models", "buffers/ a%20.mesh")
		}
		composition, err := authorizationfacts.NewFileCompositionBoundary("gltf", []authorizationfacts.FileCompositionRef{{Target: primary, Role: "manifest", Primary: true, Required: true}, {Target: buffer, Role: "buffer", Required: true}})
		if err != nil {
			t.Fatal(err)
		}
		pathJSON, _ := json.Marshal(primary)
		check := func(want bool) {
			t.Helper()
			raw, _ := json.Marshal(composition)
			var valid bool
			if err := db.Raw("SELECT system.valid_source_file_composition(?::jsonb,?::jsonb,?)", string(raw), string(pathJSON), 9).Scan(&valid).Error; err != nil || valid != want {
				t.Fatalf("glTF SQL admitted=%v want=%v err=%v", valid, want, err)
			}
		}
		check(true)
		scope, _ := json.Marshal(map[string]any{"config_digest": strings.Repeat("a", 64), "owner_task_id": "1", "owner_task_version": "1", "engine_version": "1", "file_composition": composition})
		var valid bool
		if err := db.Raw("SELECT system.valid_manager_model3d_scope(?::jsonb)", string(scope)).Scan(&valid).Error; err != nil || !valid {
			t.Fatal("glTF conversion scope rejected", err)
		}
		for i := range composition.Refs {
			prior := composition.Refs[i]
			composition.Refs[i].Required = false
			check(false)
			composition.Refs[i] = prior
			composition.Refs[i].Role = "model"
			check(false)
			composition.Refs[i] = prior
		}
	}
}
