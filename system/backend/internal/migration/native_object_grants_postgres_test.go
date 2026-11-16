package migration

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

func TestExactNativeObjectGrantsForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000297_exact_native_object_grants.up.sql", 297, 0, func(db *sql.DB) func() {
		check := func(target plugin.EngineCatalogPath, mode string, want bool) {
			t.Helper()
			raw, err := json.Marshal(target)
			if err != nil {
				t.Fatal(err)
			}
			var expression string
			if err := db.QueryRow("SELECT pg_get_expr(conbin,conrelid) FROM pg_constraint WHERE conrelid='system.engine_access_grants'::regclass AND conname='source_grant_file_composition'").Scan(&expression); err != nil {
				t.Fatal(err)
			}
			var actual bool
			if err := db.QueryRow("SELECT "+expression+" FROM (SELECT $1::jsonb AS catalog_path,$2::bigint AS engine_id,NULL::jsonb AS file_composition,NULL::jsonb AS metadata_target,$3::text AS approval_mode) g", string(raw), target.EngineID, mode).Scan(&actual); err != nil || actual != want {
				t.Fatalf("exact object constraint want=%v got=%v error=%v", want, actual, err)
			}
		}
		target := plugin.ObjectItemPath(39, "addp-demo", "tables/订单.csv")
		check(target, "independent", false)
		return func() {
			check(target, "independent", true)
			check(target, "catalog", false)
			check(plugin.FileItemPath(39, "tables/订单.csv"), "independent", false)
		}
	})
}

func TestExactNativeObjectGrantsCompositionModesForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000298_preserve_file_composition_grant_modes.up.sql", 298, 0, func(db *sql.DB) func() {
		check := func(restored bool) {
			t.Helper()
			var expression string
			if err := db.QueryRow("SELECT pg_get_expr(conbin,conrelid) FROM pg_constraint WHERE conrelid='system.engine_access_grants'::regclass AND conname='source_grant_file_composition'").Scan(&expression); err != nil {
				t.Fatal(err)
			}
			for _, target := range []plugin.EngineCatalogPath{plugin.FileItemPath(39, "images/a.tif"), plugin.ObjectItemPath(39, "images", "a.tif")} {
				composition, err := authorizationfacts.NewScannedFileCompositionBoundary("tiff", "single", []authorizationfacts.FileCompositionRef{{Target: target, Role: "image", Required: true, Primary: true}})
				if err != nil {
					t.Fatal(err)
				}
				rawTarget, _ := json.Marshal(target)
				rawComposition, _ := json.Marshal(composition)
				for _, mode := range []string{"independent", "upload_creation", "task_production", "catalog"} {
					for _, withComposition := range []bool{false, true} {
						var encoded any
						if withComposition {
							encoded = string(rawComposition)
						}
						object := target.Segments[len(target.Segments)-1].Term == plugin.EngineCatalogTermObject
						want := withComposition && (mode == "independent" || restored && (mode == "upload_creation" || mode == "task_production")) || !withComposition && object && mode == "independent"
						var actual bool
						if err := db.QueryRow("SELECT "+expression+" FROM (SELECT $1::jsonb AS catalog_path,39::bigint AS engine_id,$2::jsonb AS file_composition,NULL::jsonb AS metadata_target,$3::text AS approval_mode) g", string(rawTarget), encoded, mode).Scan(&actual); err != nil || actual != want {
							t.Fatalf("restored=%v object=%v mode=%s composition=%v: got=%v want=%v err=%v", restored, object, mode, withComposition, actual, want, err)
						}
					}
				}
			}
		}
		check(false)
		return func() { check(true) }
	})
}
