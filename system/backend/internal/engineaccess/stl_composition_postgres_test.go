package engineaccess

import (
	"encoding/json"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"gorm.io/gorm"
)

func exerciseSTLCompositionSQL(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, format := range []string{"stl", "glb", "fbx"} {
		for _, object := range []bool{false, true} {
			primary := plugin.FileItemPath(9, "models/model."+format)
			if object {
				primary = plugin.ObjectItemPath(9, "models", "model."+format)
			}
			composition, err := authorizationfacts.NewFileCompositionBoundary(format, []authorizationfacts.FileCompositionRef{{Target: primary, Role: "model", Primary: true, Required: true}})
			if err != nil {
				t.Fatal(err)
			}
			pathJSON, _ := json.Marshal(primary)
			check := func(want bool) {
				t.Helper()
				raw, _ := json.Marshal(composition)
				var valid bool
				if err := db.Raw("SELECT system.valid_source_file_composition(?::jsonb, ?::jsonb, ?)", string(raw), string(pathJSON), 9).Scan(&valid).Error; err != nil || valid != want {
					t.Fatalf("STL SQL valid=%v want=%v err=%v", valid, want, err)
				}
			}
			check(true)
			if format == "fbx" {
				composition.Refs[0].Role = "texture"
				check(false)
				composition.Refs[0].Role = "model"
				scope, _ := json.Marshal(map[string]any{"config_digest": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "owner_task_id": "1", "owner_task_version": "1", "engine_version": "1", "file_composition": composition})
				var valid bool
				if err := db.Raw("SELECT system.valid_manager_model3d_scope(?::jsonb)", string(scope)).Scan(&valid).Error; err != nil || !valid {
					t.Fatal("FBX conversion SQL scope rejected", err)
				}
			}
			if format == "glb" {
				composition.Refs[0].Role = "texture"
				check(false)
				composition.Refs[0].Role = "model"
				scope, _ := json.Marshal(map[string]any{"config_digest": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "owner_task_id": "1", "owner_task_version": "1", "engine_version": "1", "file_composition": composition})
				var valid bool
				if err := db.Raw("SELECT system.valid_manager_model3d_scope(?::jsonb)", string(scope)).Scan(&valid).Error; err != nil || valid {
					t.Fatal("GLB conversion SQL scope accepted", err)
				}
			}
			extra := primary
			extra.Segments = append([]plugin.EngineCatalogSegment{}, primary.Segments...)
			extra.Segments[len(extra.Segments)-1].Name = "unapproved.png"
			composition.Refs = append(composition.Refs, authorizationfacts.FileCompositionRef{Target: extra, Role: "texture", Required: true})
			check(false)
		}
	}

}
