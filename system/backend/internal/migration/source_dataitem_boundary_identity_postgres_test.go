package migration

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestSourceDataItemBoundaryIdentityForwardMigrationAgainstPostgres(t *testing.T) {
	testCoordinationForwardMigration(t, "000208_source_dataitem_boundary_identity.up.sql", 208, "engine_access_grants", func(db *sql.DB) func() {
		principal, tenant := seedInitializedMigrationTenant(t, db, "boundary_identity", "Boundary identity")
		var engine int64
		if err := db.QueryRow(`INSERT INTO system.engines(tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin)
 VALUES($1,'Boundary identity fixture','postgresql','{}','{"database":"boundary-identity"}','active',false) RETURNING id`, tenant).Scan(&engine); err != nil {
			t.Fatal(err)
		}
		path := plugin.TabularItemPath(uint(engine), "schema", "outdoor", "identity_upgrade")
		binding := factstest.Binding(path)
		encoded, err := json.Marshal(path)
		if err != nil {
			t.Fatal(err)
		}
		var oldChecksum string
		if err := db.QueryRow(`SELECT sha256 FROM system.schema_migration_checksums WHERE version=207`).Scan(&oldChecksum); err != nil {
			t.Fatal(err)
		}
		if oldChecksum != "558122ca553b3ffdd60264186af4c531359a21bb219ce4eb233691ecd94abbb7" {
			t.Fatal("fixture did not reproduce the actually applied migration 207")
		}
		var valid bool
		if err := db.QueryRow(`SELECT system.valid_source_single_identity($1::jsonb,$2,$3)`, string(encoded), engine, binding.ItemFingerprint).Scan(&valid); err != nil || valid {
			t.Fatal("207 fixture must reproduce the old typed-kind rejection", err)
		}
		return func() {
			var checksum string
			if err := db.QueryRow(`SELECT sha256 FROM system.schema_migration_checksums WHERE version=207`).Scan(&checksum); err != nil || checksum != oldChecksum {
				t.Fatal("forward correction rewrote the historical checksum", err)
			}
			if err := db.QueryRow(`SELECT system.valid_source_single_identity($1::jsonb,$2,$3)`, string(encoded), engine, binding.ItemFingerprint).Scan(&valid); err != nil || !valid {
				t.Fatal("208 did not accept the canonical schema namespace path", err)
			}
			if _, err := db.Exec(`INSERT INTO system.engine_access_approval_requirements(id,tenant_id,engine_id,catalog_path,item_fingerprint,mode,version,updated_at)
 VALUES($1,$2,$3,$4::jsonb,$5,'catalog',1,clock_timestamp())`, uuid.New(), tenant, engine, string(encoded), binding.ItemFingerprint); err != nil {
				t.Fatal("canonical DataItem initialization still blocked", err)
			}
			for _, change := range []func(*plugin.EngineCatalogPath){
				func(p *plugin.EngineCatalogPath) { p.Segments[0].Name = "not-a-structural-root" },
				func(p *plugin.EngineCatalogPath) { p.Segments[1].Kind = "schema" },
				func(p *plugin.EngineCatalogPath) { p.Segments[1].Name = "another_schema" },
			} {
				other := path
				other.Segments = append([]plugin.EngineCatalogSegment(nil), path.Segments...)
				change(&other)
				bad, _ := json.Marshal(other)
				if err := db.QueryRow(`SELECT system.valid_source_single_identity($1::jsonb,$2,$3)`, string(bad), engine, binding.ItemFingerprint).Scan(&valid); err != nil || valid {
					t.Fatal("malformed or mismatched DataItem path accepted", err)
				}
			}
			var present bool
			if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM system.principals WHERE id=$1)`, principal).Scan(&present); err != nil || !present {
				t.Fatal("identity correction changed IAM", err)
			}
		}
	})
}
