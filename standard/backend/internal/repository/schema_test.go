package repository

import (
	"errors"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
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
	if err := db.Exec(`INSERT INTO standard.code_items (id, code_set_id, code) VALUES (1, 1, 'enabled')`).Error; err != nil {
		t.Fatalf("seed code item: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX standard.idx_codeitem_set_code ON code_items (code)`).Error; err != nil {
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
	if err := db.Exec(`INSERT INTO standard.code_items (id, code_set_id, code) VALUES (2, 2, 'enabled')`).Error; err != nil {
		t.Fatalf("same item code in another code set should be allowed: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.code_items (id, code_set_id, code) VALUES (3, 1, 'enabled')`).Error; err == nil {
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
	if err := db.Exec(`INSERT INTO standard.metrics (id, tenant_id, code) VALUES (1, 10, 'a'), (2, 10, 'b'), (3, 10, 'c'), (4, 20, 'foreign'), (5, 10, 'leaf')`).Error; err != nil {
		t.Fatalf("seed metrics: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.metric_dependencies (id, from_metric_id, to_metric_id) VALUES (1, 1, 2), (2, 2, 3), (3, 4, 1)`).Error; err != nil {
		t.Fatalf("seed metric dependencies: %v", err)
	}

	cycle, err := metricDependencyCycle(db, 3, 10, []int64{1})
	if err != nil {
		t.Fatalf("metricDependencyCycle() error = %v", err)
	}
	if !cycle {
		t.Fatal("expected 3 -> 1 -> 2 -> 3 to be rejected")
	}

	cycle, err = metricDependencyCycle(db, 3, 10, []int64{5, 5})
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
		"CONSTRAINT fk_standard_elements_domain FOREIGN KEY (domain_id) REFERENCES standard.domains(id) ON DELETE RESTRICT",
		"CONSTRAINT fk_standard_glossary_element_mappings_element FOREIGN KEY (element_id) REFERENCES standard.elements(id) ON DELETE CASCADE",
		"CONSTRAINT fk_standard_metric_dependencies_to FOREIGN KEY (to_metric_id) REFERENCES standard.metrics(id) ON DELETE RESTRICT",
		"CONSTRAINT fk_standard_document_metric_mappings_metric FOREIGN KEY (metric_id) REFERENCES standard.metrics(id) ON DELETE CASCADE",
		"CONSTRAINT fk_standard_dimension_hierarchy_levels_element FOREIGN KEY (element_id) REFERENCES standard.elements(id) ON DELETE SET NULL",
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
		`CREATE TABLE standard.elements (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.code_sets (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.code_items (id INTEGER PRIMARY KEY, code_set_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.measurement_categories (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.classifications (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.grading_levels (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, level TEXT NOT NULL)`,
		`CREATE TABLE standard.metric_categories (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.metrics (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.metric_element_mappings (id INTEGER PRIMARY KEY, metric_id INTEGER NOT NULL, element_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.metric_dependencies (id INTEGER PRIMARY KEY, from_metric_id INTEGER NOT NULL, to_metric_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_element_mappings (id INTEGER PRIMARY KEY, document_id INTEGER NOT NULL, element_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_glossary_mappings (id INTEGER PRIMARY KEY, document_id INTEGER NOT NULL, glossary_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_metric_mappings (id INTEGER PRIMARY KEY, document_id INTEGER NOT NULL, metric_id INTEGER NOT NULL)`,
		`CREATE TABLE standard.document_file_cleanups (id INTEGER PRIMARY KEY, object_key TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at DATETIME NOT NULL, last_error TEXT)`,
		`CREATE TABLE standard.dimension_hierarchies (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL)`,
		`CREATE TABLE standard.dimension_hierarchy_levels (id INTEGER PRIMARY KEY, hierarchy_id INTEGER NOT NULL, level_num INTEGER NOT NULL, element_id INTEGER)`,
		`CREATE TABLE standard.glossary_element_mappings (glossary_id INTEGER NOT NULL, element_id INTEGER NOT NULL, PRIMARY KEY (glossary_id, element_id))`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test schema with %q: %v", statement, err)
		}
	}
	return db
}
