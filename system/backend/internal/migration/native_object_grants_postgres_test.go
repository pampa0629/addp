package migration

import (
	"database/sql"
	"encoding/json"
	"testing"

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
