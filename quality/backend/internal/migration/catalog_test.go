package migration

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestEmbeddedCatalogContainsQualityQueryIndexes(t *testing.T) {
	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatalf("ReadCatalog: %v", err)
	}
	if catalog.LatestVersion != 8 {
		t.Fatalf("latest migration version = %d, want 8", catalog.LatestVersion)
	}
	queryIndexes := catalog.Files[2]
	if queryIndexes.Name != "000003_quality_query_indexes.up.sql" {
		t.Fatalf("query index migration = %q", queryIndexes.Name)
	}
	for _, required := range []string{
		"DROP INDEX IF EXISTS quality.idx_ra_tenant_engine",
		"idx_quality_rule_applications_tenant_engine_id",
		"idx_quality_rule_applications_enabled_scope",
		"WHERE enabled = TRUE",
		"idx_quality_issues_tenant_updated",
		"idx_quality_issues_tenant_engine_updated",
		"idx_quality_issues_tenant_status_engine_updated",
	} {
		if !strings.Contains(queryIndexes.Contents, required) {
			t.Fatalf("query index migration missing %q", required)
		}
	}
	cleanup := catalog.Files[3]
	if cleanup.Name != "000004_quality_remove_redundant_tenant_indexes.up.sql" {
		t.Fatalf("tenant index cleanup migration = %q", cleanup.Name)
	}
	for _, required := range []string{
		"DROP INDEX IF EXISTS quality.idx_quality_check_tasks_tenant_id",
		"DROP INDEX IF EXISTS quality.idx_quality_issues_tenant_id",
	} {
		if !strings.Contains(cleanup.Contents, required) {
			t.Fatalf("tenant index cleanup migration missing %q", required)
		}
	}
	ruleKeyIdentity := catalog.Files[4]
	if ruleKeyIdentity.Name != "000005_quality_rule_key_identity.up.sql" {
		t.Fatalf("rule key migration = %q", ruleKeyIdentity.Name)
	}
	for _, required := range []string{
		"ADD COLUMN rule_key UUID",
		"quality.issues contains rule identities that cannot be mapped uniquely to rule_key",
		"DROP INDEX quality.uq_quality_issue_rule_application",
		"CREATE UNIQUE INDEX uq_quality_issue_rule",
	} {
		if !strings.Contains(ruleKeyIdentity.Contents, required) {
			t.Fatalf("rule key migration missing %q", required)
		}
	}
	ruleKeyIdentityV2 := catalog.Files[5]
	if ruleKeyIdentityV2.Name != "000006_quality_rule_key_identity_v2.up.sql" {
		t.Fatalf("rule key identity v2 migration = %q", ruleKeyIdentityV2.Name)
	}
	for _, required := range []string{
		"f3889a4a-1675-4623-b6e3-773f9125a04d",
		"addp.quality.rule-backfill/v1|tenant_id=%s|element_id=%s|rule_fingerprint=%s|duplicate_occurrence=%s",
		"quality_rule_key_remap",
		"SET rule_key = remap.new_rule_key::UUID",
		"SET rule_config = jsonb_set",
	} {
		if !strings.Contains(ruleKeyIdentityV2.Contents, required) {
			t.Fatalf("rule key identity v2 migration missing %q", required)
		}
	}
	gateTasks := catalog.Files[6]
	if gateTasks.Name != "000007_materialization_gate_tasks.up.sql" || !strings.Contains(gateTasks.Contents, "quality.materialization_gate_tasks") {
		t.Fatalf("materialization gate migration = %#v", gateTasks)
	}
	revisionBinding := catalog.Files[7]
	if revisionBinding.Name != "000008_quality_element_revision_binding.up.sql" {
		t.Fatalf("element revision binding migration = %q", revisionBinding.Name)
	}
	for _, required := range []string{"TRUNCATE TABLE quality.rule_applications CASCADE", "ADD COLUMN element_revision_id BIGINT NOT NULL", "element_revision_id > 0"} {
		if !strings.Contains(revisionBinding.Contents, required) {
			t.Fatalf("element revision binding migration missing %q", required)
		}
	}
}

func TestReadCatalogRequiresContinuousOrderedMigrations(t *testing.T) {
	catalog, err := ReadCatalog(fstest.MapFS{
		"sql/000002_second.up.sql": {Data: []byte("SELECT 2")},
		"sql/000001_first.up.sql":  {Data: []byte("SELECT 1")},
	}, "sql")
	if err != nil {
		t.Fatalf("ReadCatalog: %v", err)
	}
	if catalog.LatestVersion != 2 || catalog.Files[0].Name != "000001_first.up.sql" || catalog.Files[1].Name != "000002_second.up.sql" {
		t.Fatalf("catalog = %#v", catalog)
	}
	if catalog.Files[0].SHA256 == "" || catalog.Files[0].Contents != "SELECT 1" {
		t.Fatalf("first migration = %#v", catalog.Files[0])
	}
}

func TestReadCatalogRejectsGapAndUnknownFile(t *testing.T) {
	for name, source := range map[string]fstest.MapFS{
		"gap": {
			"sql/000001_first.up.sql": {Data: []byte("SELECT 1")},
			"sql/000003_third.up.sql": {Data: []byte("SELECT 3")},
		},
		"unknown": {
			"sql/000001_first.up.sql": {Data: []byte("SELECT 1")},
			"sql/readme.md":           {Data: []byte("not a migration")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadCatalog(source, "sql"); err == nil {
				t.Fatal("ReadCatalog unexpectedly succeeded")
			}
		})
	}
}

func TestVerifyAppliedMigrationsRejectsChecksumMismatch(t *testing.T) {
	catalog, err := ReadCatalog(fstest.MapFS{
		"sql/000001_first.up.sql": {Data: []byte("SELECT 1")},
	}, "sql")
	if err != nil {
		t.Fatalf("ReadCatalog: %v", err)
	}
	err = verifyAppliedMigrations(catalog, []appliedMigration{{Version: 1, Filename: "000001_first.up.sql", SHA256: strings.Repeat("0", 64)}})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("verifyAppliedMigrations error = %v", err)
	}
}
