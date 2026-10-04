package migration

import (
	"database/sql"
	"strings"
	"testing"
)

func TestFulfillmentGrantRevocationForwardMigrationAgainstPostgres(t *testing.T) {
	testApprovalPermissionForwardMigration(t, "000183_engine_access_grant_revocations.up.sql", 183, map[string][3]string{
		"system.engine_access_grant.revoke": {"system", "revoke", "high"},
	})
}

func TestFulfillmentGrantRevocationExpiryForwardMigrationAgainstPostgres(t *testing.T) {
	testCoordinationForwardMigration(t, "000184_engine_access_grant_revocation_expiry.up.sql", 184,
		"engine_access_grant_revocations", func(db *sql.DB) func() {
			return func() {
				var definition string
				if err := db.QueryRow("SELECT pg_get_functiondef('system.guard_engine_access_grant_revocation_insert()'::regprocedure)").Scan(&definition); err != nil {
					t.Fatal(err)
				}
				for _, fragment := range []string{"clock_timestamp()", "engine_access_grant_revoker_binding", "engine_access_grant_revocation_expiry", "o.grant_expires_at > NEW.revoked_at", "until_revoked"} {
					if !strings.Contains(definition, fragment) {
						t.Fatalf("forward upgrade lost guard %q", fragment)
					}
				}
			}
		})
}
