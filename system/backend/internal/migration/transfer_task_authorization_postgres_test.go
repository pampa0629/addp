package migration

import (
	"database/sql"
	"testing"
)

func TestTransferTaskAuthorizationForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000216_transfer_task_authorization.up.sql", 216, 0, func(db *sql.DB) func() {
		user, tenant := seedInitializedMigrationTenant(t, db, "task-subject", "Task subject")
		var membership int64
		if err := db.QueryRow("SELECT id FROM system.tenant_memberships WHERE tenant_id=$1 AND principal_id=$2", tenant, user).Scan(&membership); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO system.task_authorization_subjects(owner_module,task_type,task_ref,definition_hash,tenant_id,principal_id,tenant_membership_id,authorization_version,authorized_at,created_at,updated_at) VALUES('orchestrator','orchestration','32000000-0000-0000-0000-000000000216',repeat('a',64),$1,$2,$3,1,now(),now(),now())`, tenant, user, membership); err != nil {
			t.Fatal(err)
		}
		// Existing assigned Transfer runtime must have its authorization version
		// invalidated by the product binding publication, with no new assignment.
		if _, err := db.Exec(`INSERT INTO system.tenant_memberships(tenant_id,principal_id,status,source_type,joined_at,created_by_principal_id) SELECT $1,id,'active','bootstrap',now(),$2 FROM system.service_principals WHERE name='addp-transfer'`, tenant, user); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,status,valid_from,source_type) SELECT s.id,r.id,'tenant',$1,'active',now(),'bootstrap' FROM system.service_principals s CROSS JOIN system.roles r WHERE s.name='addp-transfer' AND r.tenant_id IS NULL AND r.role_key='tenant.transfer_runtime'`, tenant); err != nil {
			t.Fatal(err)
		}
		var before int64
		if err := db.QueryRow("SELECT p.authorization_version FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-transfer'").Scan(&before); err != nil {
			t.Fatal(err)
		}
		var assignments int64
		db.QueryRow("SELECT count(*) FROM system.role_assignments").Scan(&assignments)
		return func() {
			var count int64
			if err := db.QueryRow("SELECT p.authorization_version FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-transfer'").Scan(&count); err != nil || count != before+1 {
				t.Fatalf("runtime authority not invalidated: %d %v", count, err)
			}
			if err := db.QueryRow("SELECT count(*) FROM system.role_assignments").Scan(&count); err != nil || count != assignments {
				t.Fatal("migration created role assignments", err)
			}
			if err := db.QueryRow("SELECT count(*) FROM system.task_authorization_subjects WHERE owner_module='orchestrator' AND task_type='orchestration' AND task_ref='32000000-0000-0000-0000-000000000216'").Scan(&count); err != nil || count != 1 {
				t.Fatal("existing orchestration subject changed", err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='system.task_authorization.execute' AND r.role_key NOT IN ('tenant.orchestrator_runtime','tenant.transfer_runtime')`).Scan(&count); err != nil || count != 0 {
				t.Fatal("resolve permission spread to other roles", err)
			}
		}
	})
}
