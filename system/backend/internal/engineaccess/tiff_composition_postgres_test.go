package engineaccess

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"gorm.io/gorm"
)

func exerciseTIFFCompositionSQL(t *testing.T, db *gorm.DB) {
	t.Helper()
	var factReaders []string
	if err := db.Raw(`SELECT r.role_key FROM system.roles r
 JOIN system.role_permissions rp ON rp.role_id=r.id
 JOIN system.permissions p ON p.id=rp.permission_id
 WHERE p.permission_key='meta.authorization_facts.read' ORDER BY r.role_key`).Scan(&factReaders).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Join(factReaders, ",") != "tenant.catalog_runtime,tenant.manager_runtime,tenant.system_runtime" {
		t.Fatalf("authorization facts reader roles escaped runtime boundary: %v", factReaders)
	}
	for _, object := range []bool{false, true} {
		target := func(name string) plugin.EngineCatalogPath {
			if object {
				return plugin.ObjectItemPath(9, "images", name)
			}
			return plugin.FileItemPath(9, "images/"+name)
		}
		primary := target("a.tif")
		composition, err := authorizationfacts.NewScannedFileCompositionBoundary("tiff", "multi", []authorizationfacts.FileCompositionRef{
			{Target: primary, Role: "image", Primary: true, Required: true},
			{Target: target("a.tif.aux.xml"), Role: "auxiliary_metadata"},
		})
		if err != nil {
			t.Fatal(err)
		}
		pathJSON, _ := json.Marshal(primary)
		check := func(b *authorizationfacts.FileCompositionBoundary, want bool) {
			t.Helper()
			raw, _ := json.Marshal(b)
			var valid bool
			if err := db.Raw("SELECT system.valid_source_file_composition(?::jsonb, ?::jsonb, ?)", string(raw), string(pathJSON), 9).Scan(&valid).Error; err != nil || valid != want {
				t.Fatalf("TIFF SQL valid=%v want=%v err=%v", valid, want, err)
			}
		}
		check(composition, true)
		single, err := authorizationfacts.NewScannedFileCompositionBoundary("tiff", "single", []authorizationfacts.FileCompositionRef{
			{Target: primary, Role: "image", Required: true, Primary: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		check(single, true)
		single.SourceLayout = "multi"
		check(single, false)
		for _, ref := range []authorizationfacts.FileCompositionRef{
			{Target: target("a.hdr"), Role: "header"},
			{Target: target("a.tfw"), Role: "world_file"},
			{Target: target("a.prj"), Role: "crs"},
			{Target: target("a.tif.ovr"), Role: "overview"},
			{Target: target("a.tif.msk"), Role: "mask"},
		} {
			composition, err = authorizationfacts.NewScannedFileCompositionBoundary("tiff", "multi", append(composition.Refs, ref))
			if err != nil {
				t.Fatal(err)
			}
			check(composition, true)
		}
		scope, _ := json.Marshal(execution.ManagerFileReadScope{ConfigDigest: strings.Repeat("a", 64), OwnerTaskID: "1", OwnerTaskVersion: "1", EngineVersion: "1", FileComposition: *composition})
		var valid bool
		if err := db.Raw("SELECT system.valid_manager_file_scope(?::jsonb)", string(scope)).Scan(&valid).Error; err != nil || !valid {
			t.Fatal("TIFF/PAM COG scope rejected", err)
		}
		for _, mutate := range []func(*authorizationfacts.FileCompositionBoundary){
			func(b *authorizationfacts.FileCompositionBoundary) { b.Refs[1].Role = "texture" },
			func(b *authorizationfacts.FileCompositionBoundary) { b.SourceLayout = "single" },
			func(b *authorizationfacts.FileCompositionBoundary) { b.Refs[1].Target = target("sub/a.tif.aux.xml") },
			func(b *authorizationfacts.FileCompositionBoundary) {
				for i := range b.Refs {
					if b.Refs[i].Primary {
						b.Refs[i].Required = false
					}
				}
			},
			func(b *authorizationfacts.FileCompositionBoundary) { b.Refs = append(b.Refs, b.Refs[1]) },
		} {
			bad := composition.Clone()
			mutate(bad)
			check(bad, false)
		}
	}
}
