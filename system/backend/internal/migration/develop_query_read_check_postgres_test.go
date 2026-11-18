package migration

import "testing"

func TestDevelopQueryReadCheckRuntimeForwardMigrationAgainstPostgres(t *testing.T) {
	testRuntimePermissionMigration(t, "addp-develop", "tenant.develop_runtime", "000301_develop_query_read_check.up.sql", 301, 1, "system.develop_query_read_check.execute", []string{"tenant.develop_runtime"})
}
