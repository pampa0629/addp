package migration

import "testing"

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
