package migration

import "testing"

func TestProductionReceiptForwardMigrationAgainstPostgres(t *testing.T) {
	testRuntimePermissionMigration(t, "addp-system", "tenant.system_runtime", "000219_transfer_production_receipt.up.sql", 219, 1, "transfer.production_receipt.read", []string{"tenant.system_runtime"})
}
