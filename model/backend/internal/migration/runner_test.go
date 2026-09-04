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

func TestMaterializationPhysicalTargetMigrationBindsActiveBatch(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/008_bind_active_materialization_to_physical_target.up.sql")
	if err != nil {
		t.Fatalf("read physical-target migration: %v", err)
	}
	sql := string(content)
	for _, fragment := range []string{
		"DROP INDEX model.uq_model_materialization_active_target",
		"tenant_id, engine_id, target_parent_locator, target_name",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("physical-target migration missing %s", fragment)
		}
	}
}

func TestMaterializationSealMigrationRemovesWriterCoupling(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/014_replace_write_attempt_with_seal.up.sql")
	if err != nil {
		t.Fatalf("read materialization seal migration: %v", err)
	}
	sql := string(content)
	for _, fragment := range []string{
		"DROP TABLE model.materialization_write_attempts",
		"DROP COLUMN completed_write_attempt_id",
		"ADD COLUMN writer_execution_id VARCHAR(255)",
		"ADD COLUMN seal_execution_id VARCHAR(255)",
		"'sealed'",
		"status = 'aborted'",
		"uq_model_materialization_seal_execution",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("materialization seal migration missing %s", fragment)
		}
	}
}

func TestMaterializationPartitionNormalizationMigrationRemovesEmptyDesign(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/015_normalize_empty_partition_materialization.up.sql")
	if err != nil {
		t.Fatalf("read materialization partition normalization migration: %v", err)
	}
	sql := string(content)
	for _, fragment := range []string{
		"materialization - 'partition_by' - 'partition_type'",
		"btrim(materialization->>'partition_by') = ''",
		"materialization->'partition_by' = 'null'::jsonb",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("partition normalization migration missing %s", fragment)
		}
	}
}

func TestMaterializationTargetPredecessorMigrationPersistsCompareAndSwapState(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/017_capture_materialization_target_predecessor.up.sql")
	if err != nil {
		t.Fatalf("read materialization predecessor migration: %v", err)
	}
	sql := string(content)
	for _, fragment := range []string{
		"ADD COLUMN expected_target_marker TEXT",
		"NULL means the target did not exist",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("materialization predecessor migration missing %s", fragment)
		}
	}
}

func TestDimensionHierarchyMigrationEstablishesModelOwnedAggregate(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/018_move_dimension_hierarchies_to_model.up.sql")
	if err != nil {
		t.Fatalf("read dimension hierarchy migration: %v", err)
	}
	sql := string(content)
	for _, fragment := range []string{
		"DROP COLUMN IF EXISTS hierarchy_id",
		"DROP COLUMN IF EXISTS hierarchy_level",
		"CREATE TABLE model.dimension_hierarchies",
		"CREATE TABLE model.dimension_hierarchy_levels",
		"FOREIGN KEY (table_id, tenant_id)",
		"FOREIGN KEY (field_id)",
		"CHECK (resource_type IN ('domain', 'element', 'metric'))",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("dimension hierarchy migration missing %s", fragment)
		}
	}
}

func TestMaterializationExecutionIDMigrationUsesCommonExecutionIdentityType(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/010_normalize_materialization_execution_ids.up.sql")
	if err != nil {
		t.Fatalf("read execution ID migration: %v", err)
	}
	sql := string(content)
	for _, fragment := range []string{
		"prepare_execution_id TYPE VARCHAR(255)",
		"publish_execution_id TYPE VARCHAR(255)",
		"writer_execution_id TYPE VARCHAR(255)",
		"model_execution_id TYPE VARCHAR(255)",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("execution ID migration missing %s", fragment)
		}
	}
}

func TestMaterializationGroupMigrationDefinesVersionedAggregate(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/011_add_materialization_groups.up.sql")
	if err != nil {
		t.Fatalf("read materialization group migration: %v", err)
	}
	sql := string(content)
	for _, fragment := range []string{
		"model.materialization_groups",
		"model.materialization_group_members",
		"UNIQUE (tenant_id, code)",
		"CHECK (version > 0)",
		"FOREIGN KEY (logical_table_id, tenant_id)",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("materialization group migration missing %s", fragment)
		}
	}
}

func TestMaterializationGroupPublishMigrationAllowsOneExecutionToOwnAllBatches(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/012_allow_group_publish_execution_batches.up.sql")
	if err != nil {
		t.Fatalf("read materialization group publish migration: %v", err)
	}
	sql := string(content)
	for _, fragment := range []string{
		"DROP CONSTRAINT IF EXISTS materialization_batches_publish_execution_id_key",
		"idx_model_materialization_batches_publish_execution",
		"tenant_id, publish_execution_id",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("materialization group publish migration missing %s", fragment)
		}
	}
}

func TestCatalogResourceChangeMigrationUsesRootTriggersAndMinimalProjection(t *testing.T) {
	content, err := fs.ReadFile(migrationFiles, "sql/013_add_catalog_resource_changes.up.sql")
	if err != nil {
		t.Fatalf("read catalog resource change migration: %v", err)
	}
	sql := string(content)
	for _, fragment := range []string{
		"model.catalog_resource_changes",
		"trg_model_entity_catalog_change",
		"trg_model_logical_table_catalog_change",
		"'object_kind'",
		"'model_status'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("catalog resource change migration missing %s", fragment)
		}
	}
	for _, forbidden := range []string{"materialization'", "entity_attributes", "logical_fields", "fact_metric_mappings"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("catalog resource change migration copies forbidden professional fact %s", forbidden)
		}
	}
}
