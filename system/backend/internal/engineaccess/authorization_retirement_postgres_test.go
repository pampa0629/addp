package engineaccess

import (
	"strings"
	"testing"

	"github.com/addp/system/internal/migration"
	"gorm.io/gorm"
)

// Reuse the standard engineaccess fixture's actual grants, denials, recipient
// lifecycle, and production outcomes. The retirement itself stays rolled back.
func exerciseDevelopmentAuthorizationRetirement(t *testing.T, db *gorm.DB) {
	t.Helper()
	t.Run("development retirement removes rules without reviving production", func(t *testing.T) {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		preserved := []string{"principals", "tenant_memberships", "role_assignments", "departments", "project_groups", "engine_access_delegations", "meta_collection_qualifications", "engine_access_direct_permissions"}
		snapshot := func() string {
			var result strings.Builder
			for _, table := range preserved {
				var value string
				if err := tx.Raw("SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text, '[]') FROM system." + table + " t").Scan(&value).Error; err != nil {
					t.Fatal(table, err)
				}
				result.WriteString(table + value)
			}
			return result.String()
		}
		before := snapshot()
		var grants int64
		if err := tx.Table("system.engine_access_grants").Count(&grants).Error; err != nil || grants == 0 {
			t.Fatalf("fixture has no real source grants: %d %v", grants, err)
		}
		var outcomeCount int64
		if err := tx.Table("system.engine_access_initial_grant_outcomes").Count(&outcomeCount).Error; err != nil || outcomeCount == 0 {
			t.Fatalf("fixture has no production replay keys: %d %v", outcomeCount, err)
		}
		data, err := migration.EmbeddedSQL.ReadFile("sql/000246_retire_development_source_authorizations.up.sql")
		if err != nil {
			t.Fatal(err)
		}
		sql := strings.TrimSpace(string(data))
		sql = strings.TrimPrefix(sql, "BEGIN;")
		sql = strings.TrimSuffix(sql, "COMMIT;")
		if err := tx.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"engine_access_grants", "engine_access_grant_revocations", "engine_access_denies", "engine_access_deny_releases", "engine_access_fulfillment_outcomes"} {
			var n int64
			if err := tx.Table("system." + table).Count(&n).Error; err != nil || n != 0 {
				t.Fatalf("retirement left %s=%d: %v", table, n, err)
			}
		}
		var retained, invalid int64
		if err := tx.Table("system.engine_access_initial_grant_outcomes").Count(&retained).Error; err != nil || retained != outcomeCount {
			t.Fatalf("production replay keys lost: %d/%d %v", retained, outcomeCount, err)
		}
		if err := tx.Table("system.engine_access_initial_grant_outcomes").Where("outcome <> 'denied' OR grant_request_id IS NOT NULL").Count(&invalid).Error; err != nil || invalid != 0 {
			t.Fatalf("dangling issued outcome: %d %v", invalid, err)
		}
		if before != snapshot() {
			t.Fatal("retirement changed unrelated IAM, delegation, collection qualification or approval configuration")
		}
		var disabled int64
		if err := tx.Raw(`SELECT count(*) FROM pg_trigger WHERE tgrelid IN ('system.engine_access_grants'::regclass,'system.engine_access_initial_grant_outcomes'::regclass,'system.engine_access_denies'::regclass) AND tgenabled='D'`).Scan(&disabled).Error; err != nil || disabled != 0 {
			t.Fatalf("immutable protection not restored: %d %v", disabled, err)
		}
		// An applied migration is not replayed on ordinary startup; the standard
		// runner tests cover checksum/idempotence. This fixture tests its data effects.
	})
}
