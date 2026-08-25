package migration

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMigrationNamesReturnsOnlyOrderedUpMigrations(t *testing.T) {
	names, err := migrationNames(fstest.MapFS{
		"sql/002_second.up.sql":   {Data: []byte("select 2")},
		"sql/001_first.up.sql":    {Data: []byte("select 1")},
		"sql/002_second.down.sql": {Data: []byte("select 2")},
		"sql/readme.md":           {Data: []byte("ignored")},
	}, "sql")
	if err != nil {
		t.Fatalf("migrationNames() error = %v", err)
	}
	if len(names) != 2 || names[0] != "001_first.up.sql" || names[1] != "002_second.up.sql" {
		t.Fatalf("migration names = %#v", names)
	}
}

func TestConcurrencyMigrationDefinesPositiveVersionConstraints(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/004_add_concurrency_versions.up.sql")
	if err != nil {
		t.Fatalf("read concurrency migration: %v", err)
	}
	sql := string(content)
	for _, constraint := range []string{
		"ck_model_entities_version_positive",
		"ck_model_logical_tables_version_positive",
		"ck_model_dw_layers_version_positive",
		"ck_model_entity_relations_version_positive",
		"ck_model_entity_model_revision_positive",
	} {
		if !strings.Contains(sql, constraint) {
			t.Fatalf("concurrency migration missing constraint %s", constraint)
		}
	}
}

func TestMaterializationMigrationRemovesLegacyTargetFields(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/006_unify_materialization_target_locator.up.sql")
	if err != nil {
		t.Fatalf("read materialization migration: %v", err)
	}
	sql := string(content)
	for _, field := range []string{"schema_name", "table_name"} {
		if !strings.Contains(sql, "- '"+field+"'") {
			t.Fatalf("materialization migration does not remove %s", field)
		}
	}
	if strings.Contains(sql, "target_parent_locator") || strings.Contains(sql, "target_name") {
		t.Fatal("materialization migration must not guess a new target from legacy fields")
	}
}

func TestMaterializationBatchMigrationDefinesControlledLifecycle(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/007_add_materialization_batches.up.sql")
	if err != nil {
		t.Fatalf("read materialization batch migration: %v", err)
	}
	sql := string(content)
	for _, fragment := range []string{
		"model.materialization_batches",
		"ck_model_materialization_batch_status",
		"uq_model_materialization_active_target",
		"prepare_execution_id UUID NOT NULL UNIQUE",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("materialization batch migration missing %s", fragment)
		}
	}
}
