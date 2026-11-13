package migration

import "testing"

func TestWorkflowPrivateFilesRuntimeForwardMigrationAgainstPostgres(t *testing.T) {
	testRuntimePermissionMigration(t, "addp-develop", "tenant.develop_runtime", "000293_workflow_private_files_runtime.up.sql", 293, 1, "system.workflow_private_files.execute", []string{"tenant.develop_runtime"})
}
