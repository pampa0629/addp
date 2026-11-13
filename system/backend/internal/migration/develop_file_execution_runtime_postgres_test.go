package migration

import "testing"

func TestDevelopFileExecutionRuntimeForwardMigrationAgainstPostgres(t *testing.T) {
	testRuntimePermissionMigration(t, "addp-system", "tenant.system_runtime", "000291_develop_file_execution_runtime.up.sql", 291, 0, "develop.file_execution.read", []string{"tenant.system_runtime"})
}
