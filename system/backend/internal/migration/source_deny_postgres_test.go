package migration

import "testing"

func TestSourceDenyForwardMigrationAgainstPostgres(t *testing.T) {
	testApprovalPermissionForwardMigration(t, "000185_engine_access_denies.up.sql", 185, map[string][3]string{
		"system.engine_access_deny.create": {"system", "create", "high"},
	})
}

func TestSourceDenyReleaseForwardMigrationAgainstPostgres(t *testing.T) {
	testApprovalPermissionForwardMigration(t, "000186_engine_access_deny_releases.up.sql", 186, map[string][3]string{
		"system.engine_access_deny.release": {"system", "release", "high"},
	})
}
