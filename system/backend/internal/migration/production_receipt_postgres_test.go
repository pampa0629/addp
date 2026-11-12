package migration

import (
	"database/sql"
	"testing"
)

func TestProductionReceiptForwardMigrationAgainstPostgres(t *testing.T) {
	testRuntimePermissionMigration(t, "addp-system", "tenant.system_runtime", "000219_transfer_production_receipt.up.sql", 219, 1, "transfer.production_receipt.read", []string{"tenant.system_runtime"})
}

func TestDevelopProductionReceiptForwardMigrationAgainstPostgres(t *testing.T) {
	for _, runtime := range []struct{ client, role string }{{"addp-system", "tenant.system_runtime"}, {"addp-meta", "tenant.meta_runtime"}} {
		t.Run(runtime.client, func(t *testing.T) {
			testRuntimePermissionMigration(t, runtime.client, runtime.role, "000288_develop_production_receipt.up.sql", 288, 1, "develop.production_receipt.read", []string{"tenant.system_runtime", "tenant.meta_runtime"})
		})
	}
}

func TestFileProductionRuntimeForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000289_file_production_grants.up.sql", 289, 0, func(db *sql.DB) func() {
		return func() {
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='system.engine_access_initial_grant.execute' AND r.role_key='tenant.develop_runtime'`).Scan(&count); err != nil || count != 1 {
				t.Fatal("Develop delivery permission missing", count, err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='system.engine_access_initial_grant.execute' AND r.role_key NOT IN ('tenant.develop_runtime','tenant.transfer_runtime','tenant.manager_runtime')`).Scan(&count); err != nil || count != 0 {
				t.Fatal("initial Grant machine permission spread", count, err)
			}
		}
	})
}
