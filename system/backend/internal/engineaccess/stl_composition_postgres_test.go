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
	for _, object := range []bool{false, true} {
		primary := plugin.FileItemPath(9, "models/model.stl")
		if object {
			primary = plugin.ObjectItemPath(9, "models", "model.stl")
		}
		composition, err := authorizationfacts.NewFileCompositionBoundary("stl", []authorizationfacts.FileCompositionRef{{Target: primary, Role: "model", Primary: true, Required: true}})
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
		extra := primary
		extra.Segments = append([]plugin.EngineCatalogSegment{}, primary.Segments...)
		extra.Segments[len(extra.Segments)-1].Name = "unapproved.png"
		composition.Refs = append(composition.Refs, authorizationfacts.FileCompositionRef{Target: extra, Role: "texture", Required: true})
		check(false)
	}
}
