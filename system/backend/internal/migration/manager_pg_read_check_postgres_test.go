package migration

import "testing"

func TestManagerPGReadCheckRuntimeForwardMigrationAgainstPostgres(t *testing.T) {
	testRuntimePermissionMigration(t, "addp-manager", "tenant.manager_runtime", "000294_manager_pg_read_check.up.sql", 294, 1, "system.manager_pg_read_check.execute", []string{"tenant.manager_runtime"})
}
