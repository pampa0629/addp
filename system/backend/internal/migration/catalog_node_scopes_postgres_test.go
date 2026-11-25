package migration

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestCatalogNodeScopesForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000312_business_node_profiles.up.sql", 312, 0, func(db *sql.DB) func() {
		owner, tenant := seedInitializedMigrationTenant(t, db, "business-nodes", "Business nodes")
		var membership, version int64
		if err := db.QueryRow(`SELECT m.id,p.authorization_version FROM system.tenant_memberships m
			JOIN system.principals p ON p.id=m.principal_id WHERE m.principal_id=$1 AND m.tenant_id=$2`, owner, tenant).Scan(&membership, &version); err != nil {
			t.Fatal(err)
		}
		engines := map[string]uint{}
		for _, kind := range []string{"gaussdb", "ftp", "rustfs"} {
			var id uint
			if err := db.QueryRow(`INSERT INTO system.engines(tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin)
				VALUES($1,$2,$3,'{}','{"host":"fixture.invalid"}','active',false) RETURNING id`, tenant, "Business node "+kind, kind).Scan(&id); err != nil {
				t.Fatal(err)
			}
			engines[kind] = id
		}
		check := func(path plugin.EngineCatalogPath, recursive bool, action string, allowed bool) {
			t.Helper()
			raw, err := json.Marshal(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode,tenant_id,engine_id,catalog_path,item_fingerprint,boundary_hash,node_recursive,recipient_type,recipient_id,action,expiry_mode,operator_principal_id,operator_membership_id,operator_authorization_version,reason)
				VALUES($1,'independent',$2,$3,$4::jsonb,'','',$5,'user',$6,$7,'until_revoked',$6,$8,$9,'Business node profile fixture')`, uuid.New(), tenant, path.EngineID, string(raw), recursive, owner, action, membership, version)
			if allowed {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var violation *pq.Error
			if !errors.As(err, &violation) || violation.Constraint != "source_grant_node_engine" {
				t.Fatalf("expected node engine constraint for %v, got %v", path, err)
			}
		}
		gaussRoot := plugin.EngineCatalogRootPath(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), engines["gaussdb"])
		roots := []plugin.EngineCatalogPath{gaussRoot, plugin.FileRootPath(engines["ftp"]), plugin.ObjectRootPath(engines["rustfs"])}
		for _, root := range roots {
			check(root, true, "read", false)
		}
		return func() {
			check(plugin.FileDirectoryPath(engines["ftp"], "existing"), false, "read", true)
			grants := func() string {
				t.Helper()
				var snapshot string
				if err := db.QueryRow(`SELECT jsonb_agg(to_jsonb(g) ORDER BY request_id)::text
					FROM system.engine_access_grants g WHERE tenant_id=$1`, tenant).Scan(&snapshot); err != nil {
					t.Fatal(err)
				}
				return snapshot
			}
			checkOriginal := func() {
				t.Helper()
				var name, digest string
				if err := db.QueryRow(`SELECT filename,sha256 FROM system.schema_migration_checksums WHERE version=312`).Scan(&name, &digest); err != nil {
					t.Fatal(err)
				}
				if name != "000312_business_node_profiles.up.sql" || digest != "682c2350b4177d2270bb52887aebcc900e25a840807edea2a4556d54fcaac3a8" {
					t.Fatalf("unexpected deployed migration: %s %s", name, digest)
				}
			}
			checkOriginal()
			saved := grants()
			runner := NewRunner(os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN"))
			for range 2 {
				if err := runner.Run(t.Context()); err != nil {
					t.Fatal(err)
				}
				checkOriginal()
				if grants() != saved {
					t.Fatal("migration changed existing Grants")
				}
			}
			nodes := []plugin.EngineCatalogPath{
				plugin.TabularNamespacePath(engines["gaussdb"], "schema", "sales"),
				plugin.FileDirectoryPath(engines["ftp"], "samples"),
				plugin.ObjectDirectoryPath(engines["rustfs"], "data", ""),
				plugin.ObjectDirectoryPath(engines["rustfs"], "data", "samples"),
			}
			for _, action := range []string{"metadata_read", "read"} {
				for _, root := range roots {
					check(root, true, action, true)
				}
				for _, node := range nodes {
					check(node, false, action, true)
				}
			}
			check(plugin.ObjectRootPath(engines["rustfs"]), false, "read", false)
		}
	})
}

func TestCatalogDatabaseNamespaceForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000315_catalog_database_namespace.up.sql", 315, 0, func(db *sql.DB) func() {
		const engine = 9
		namespace := plugin.TabularNamespacePath(engine, "database", "sales")
		root := plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("database"), engine)
		item := plugin.TabularItemPath(engine, "database", "sales", "orders")
		check := func(scope, target plugin.EngineCatalogPath, recursive, want bool) {
			t.Helper()
			path, err := json.Marshal(scope)
			if err != nil {
				t.Fatal(err)
			}
			leaf, err := json.Marshal(target)
			if err != nil {
				t.Fatal(err)
			}
			var covered bool
			if err := db.QueryRow("SELECT system.source_node_covers($1::jsonb,$2,$3::jsonb)", string(path), recursive, string(leaf)).Scan(&covered); err != nil || covered != want {
				t.Fatalf("scope %v recursive=%t target %v: got %t, want %t, err=%v", scope, recursive, target, covered, want, err)
			}
		}
		check(namespace, item, false, false)
		return func() {
			check(namespace, item, false, true)
			check(root, item, true, true)
			check(root, item, false, false)
			check(namespace, plugin.TabularItemPath(engine, "database", "sales_archive", "orders"), true, false)
			check(namespace, plugin.TabularItemPath(engine, "schema", "sales", "orders"), true, false)
			check(namespace, plugin.TabularItemPath(engine+1, "database", "sales", "orders"), true, false)
		}
	})
}
