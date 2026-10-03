package migration

import "testing"

func TestFulfillmentGrantRevocationForwardMigrationAgainstPostgres(t *testing.T) {
	testApprovalPermissionForwardMigration(t, "000183_engine_access_grant_revocations.up.sql", 183, map[string][3]string{
		"system.engine_access_grant.revoke": {"system", "revoke", "high"},
	})
}
