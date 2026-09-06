package repository

import (
	"errors"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnsureSchemaEnforcesTenantAndAggregateUniqueness(t *testing.T) {
	db := openStandardSchemaTestDB(t)
	if err := db.Exec(`INSERT INTO standard.code_sets (id, tenant_id, code) VALUES (1, 10, 'shared')`).Error; err != nil {
		t.Fatalf("seed code set: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX standard.idx_codeset_tenant_code ON code_sets (code)`).Error; err != nil {
		t.Fatalf("create legacy code-set index: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.code_set_revisions (id, code_set_id, revision_no) VALUES (10, 1, 1)`).Error; err != nil {
		t.Fatalf("seed code set revision: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.code_set_revision_items (id, code_set_revision_id, code) VALUES (1, 10, 'enabled')`).Error; err != nil {
		t.Fatalf("seed code item: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX standard.idx_codeitem_set_code ON code_set_revision_items (code)`).Error; err != nil {
		t.Fatalf("create legacy code-item index: %v", err)
	}

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema() should be idempotent: %v", err)
	}

	if err := db.Exec(`INSERT INTO standard.code_sets (id, tenant_id, code) VALUES (2, 20, 'shared')`).Error; err != nil {
		t.Fatalf("same code in another tenant should be allowed: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.code_sets (id, tenant_id, code) VALUES (3, 10, 'shared')`).Error; err == nil {
		t.Fatal("same code in one tenant should be rejected")
	}
	if err := db.Exec(`INSERT INTO standard.code_set_revisions (id, code_set_id, revision_no) VALUES (20, 2, 1)`).Error; err != nil {
		t.Fatalf("seed second code set revision: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.code_set_revision_items (id, code_set_revision_id, code) VALUES (2, 20, 'enabled')`).Error; err != nil {
		t.Fatalf("same item code in another code set should be allowed: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.code_set_revision_items (id, code_set_revision_id, code) VALUES (3, 10, 'enabled')`).Error; err == nil {
		t.Fatal("same item code in one code set should be rejected")
	}
}

func TestEnsureSchemaRejectsExistingDuplicates(t *testing.T) {
	db := openStandardSchemaTestDB(t)
	if err := db.Exec(`INSERT INTO standard.domains (id, tenant_id, code) VALUES (1, 10, 'duplicate'), (2, 10, 'duplicate')`).Error; err != nil {
		t.Fatalf("seed duplicate domains: %v", err)
	}

	err := EnsureSchema(db)
	if err == nil {
		t.Fatal("EnsureSchema() should reject existing duplicate business keys")
	}
	if !strings.Contains(err.Error(), "uq_standard_domains_tenant_code") {
		t.Fatalf("EnsureSchema() error = %v, want failing constraint name", err)
	}
}

func TestMetricDependencyCycleDetectsIndirectCycle(t *testing.T) {
	db := openStandardSchemaTestDB(t)
	if err := db.Exec(`INSERT INTO standard.metric_definitions (id, tenant_id, code) VALUES (1, 10, 'a'), (2, 10, 'b'), (3, 10, 'c'), (4, 20, 'foreign'), (5, 10, 'leaf')`).Error; err != nil {
		t.Fatalf("seed metrics: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.metric_definition_revisions (id, metric_definition_id, revision_no) VALUES (11, 1, 1), (12, 2, 1), (13, 3, 1), (14, 4, 1)`).Error; err != nil {
		t.Fatalf("seed metric revisions: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.metric_definition_revision_dependencies (id, metric_definition_revision_id, dependency_definition_id, relation_kind) VALUES (1, 11, 2, 'base'), (2, 12, 3, 'base'), (3, 14, 1, 'base')`).Error; err != nil {
		t.Fatalf("seed metric dependencies: %v", err)
	}

	cycle, err := metricDependencyCycle(db, 3, 10, []models.MetricDefinitionRevisionDependency{{DependencyDefinitionID: 1}})
	if err != nil {
		t.Fatalf("metricDependencyCycle() error = %v", err)
	}
	if !cycle {
		t.Fatal("expected 3 -> 1 -> 2 -> 3 to be rejected")
	}

	cycle, err = metricDependencyCycle(db, 3, 10, []models.MetricDefinitionRevisionDependency{{DependencyDefinitionID: 5}})
	if err != nil {
		t.Fatalf("metricDependencyCycle() error = %v", err)
	}
	if cycle {
		t.Fatal("replacing metric 3 dependencies with a leaf should not form a cycle")
	}
}

func TestTenantScopedDeleteDoesNotReportFalseSuccess(t *testing.T) {
	db := openStandardSchemaTestDB(t)
	if err := db.Exec(`INSERT INTO standard.domains (id, tenant_id, code) VALUES (1, 20, 'foreign')`).Error; err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	err := NewDomainRepository(db).Delete(1, 10)
	if !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("Delete() error = %v, want ErrNotFound", err)
	}
	var count int64
	if err := db.Table("standard.domains").Where("id = 1").Count(&count).Error; err != nil {
		t.Fatalf("count domain: %v", err)
	}
	if count != 1 {
		t.Fatalf("foreign domain count = %d, want 1", count)
	}
}

func TestPostgresSchemaStatementsDefineDeletePolicies(t *testing.T) {
	joined := strings.Join(postgresStandardSchemaStatements(), "\n")
	for _, expected := range []string{
		"CONSTRAINT fk_standard_domains_parent FOREIGN KEY (parent_id) REFERENCES standard.domains(id) ON DELETE RESTRICT",
		"CONSTRAINT fk_standard_glossaries_owner_domain FOREIGN KEY (owner_domain_id) REFERENCES standard.domains(id) ON DELETE RESTRICT",
		"CONSTRAINT fk_standard_glossary_revisions_glossary FOREIGN KEY (glossary_id) REFERENCES standard.glossaries(id) ON DELETE CASCADE",
		"CONSTRAINT ck_standard_glossaries_scope CHECK",
		"CONSTRAINT ck_standard_glossary_revisions_effective_interval CHECK",
		"CREATE TRIGGER trg_standard_glossary_revision_effective_interval",
		"CONSTRAINT fk_standard_elements_owner_domain FOREIGN KEY (owner_domain_id) REFERENCES standard.domains(id) ON DELETE RESTRICT",
		"CONSTRAINT fk_standard_code_sets_owner_domain FOREIGN KEY (owner_domain_id) REFERENCES standard.domains(id) ON DELETE RESTRICT",
		"CONSTRAINT ck_standard_elements_scope CHECK",
		"CONSTRAINT ck_standard_code_sets_scope CHECK",
		"CONSTRAINT fk_standard_glossary_element_mappings_element FOREIGN KEY (element_id) REFERENCES standard.elements(id) ON DELETE CASCADE",
		"CONSTRAINT fk_standard_metric_revision_dependencies_definition FOREIGN KEY (dependency_definition_id) REFERENCES standard.metric_definitions(id) ON DELETE RESTRICT",
		"CONSTRAINT ck_standard_metric_definition_revisions_effective_interval CHECK",
		"CREATE TRIGGER trg_standard_metric_revision_effective_interval",
		"CONSTRAINT fk_standard_document_metric_mappings_metric FOREIGN KEY (metric_id) REFERENCES standard.metric_definitions(id) ON DELETE CASCADE",
		"ALTER TABLE standard.document_extraction_candidates DROP CONSTRAINT IF EXISTS fk_standard_document_extractions_candidates",
		"ALTER TABLE standard.document_extraction_evidences DROP CONSTRAINT IF EXISTS fk_standard_document_extraction_candidates_evidences",
		"CONSTRAINT fk_standard_document_candidate_formalizations_candidate FOREIGN KEY (candidate_id) REFERENCES standard.document_extraction_candidates(id) ON DELETE RESTRICT",
		"CONSTRAINT ck_standard_document_candidate_formalizations_action CHECK",
		"CONSTRAINT ck_standard_document_candidate_formalizations_status CHECK",
		"CONSTRAINT ck_standard_collection_events_type CHECK",
		"CONSTRAINT fk_standard_collection_events_revision FOREIGN KEY (revision_id) REFERENCES standard.standard_collection_revisions(id) ON DELETE CASCADE",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("postgres schema statements missing %q", expected)
		}
	}
}

func openStandardSchemaTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatalf("attach standard schema: %v", err)
	}
	statements := []string{
		`CREATE TABLE standard.reference_deletions (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, resource_type TEXT NOT NULL, resource_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.domains (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.standard_collections (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.standard_collection_revisions (id INTEGER PRIMARY KEY, collection_id INTEGER NOT NULL, revision_no INTEGER NOT NULL)`,
		`CREATE TABLE standard.standard_collection_members (id INTEGER PRIMARY KEY, collection_revision_id INTEGER NOT NULL, member_type TEXT NOT NULL, member_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.standard_collection_assignments (id INTEGER PRIMARY KEY, collection_id INTEGER NOT NULL, principal_id INTEGER NOT NULL, role TEXT NOT NULL)`,
		`CREATE TABLE standard.standard_collection_events (id INTEGER PRIMARY KEY, collection_id INTEGER NOT NULL, revision_id INTEGER, event_type TEXT NOT NULL, actor_id INTEGER NOT NULL, detail TEXT NOT NULL)`,
		`CREATE TABLE standard.glossaries (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.glossary_revisions (id INTEGER PRIMARY KEY, glossary_id INTEGER NOT NULL, revision_no INTEGER NOT NULL)`,
		`CREATE TABLE standard.elements (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.element_revisions (id INTEGER PRIMARY KEY, element_id INTEGER NOT NULL, revision_no INTEGER NOT NULL)`,
		`CREATE TABLE standard.code_sets (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.code_set_revisions (id INTEGER PRIMARY KEY, code_set_id INTEGER NOT NULL, revision_no INTEGER NOT NULL)`,
		`CREATE TABLE standard.code_set_revision_items (id INTEGER PRIMARY KEY, code_set_revision_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.measurement_categories (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.metric_categories (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.metric_definitions (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.metric_definition_revisions (id INTEGER PRIMARY KEY, metric_definition_id INTEGER NOT NULL, revision_no INTEGER NOT NULL)`,
		`CREATE TABLE standard.metric_definition_revision_dependencies (id INTEGER PRIMARY KEY, metric_definition_revision_id INTEGER NOT NULL, dependency_definition_id INTEGER NOT NULL, dependency_revision_id INTEGER, relation_kind TEXT NOT NULL)`,
		`CREATE TABLE standard.documents (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.document_revisions (id INTEGER PRIMARY KEY, document_id INTEGER NOT NULL, revision_no INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_extractions (id INTEGER PRIMARY KEY, document_revision_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_extraction_candidates (id INTEGER PRIMARY KEY, extraction_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_candidate_formalizations (candidate_id INTEGER PRIMARY KEY, standard_id INTEGER NOT NULL, revision_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_extraction_evidences (id INTEGER PRIMARY KEY, document_revision_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_element_mappings (id INTEGER PRIMARY KEY, document_id INTEGER NOT NULL, element_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_glossary_mappings (id INTEGER PRIMARY KEY, document_id INTEGER NOT NULL, glossary_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_metric_mappings (id INTEGER PRIMARY KEY, document_id INTEGER NOT NULL, metric_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_file_cleanups (id INTEGER PRIMARY KEY, object_key TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at DATETIME NOT NULL, last_error TEXT)`,
		`CREATE TABLE standard.glossary_element_mappings (glossary_id INTEGER NOT NULL, element_id INTEGER NOT NULL, PRIMARY KEY (glossary_id, element_id))`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test schema with %q: %v", statement, err)
		}
	}
	return db
}
