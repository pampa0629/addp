package repository

import (
	"fmt"

	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

const standardSchemaLockID int64 = 2026081003

// Migrate 在同一 advisory transaction lock 内完成表字段迁移和约束收紧。
func Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("standard schema database is required")
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := acquireStandardSchemaLock(tx); err != nil {
			return err
		}
		if err := prepareStandardSchemaMigration(tx); err != nil {
			return err
		}
		if err := tx.AutoMigrate(
			&models.Domain{},
			&models.Glossary{},
			&models.GlossaryElementMapping{},
			&models.Element{},
			&models.CodeSet{},
			&models.CodeItem{},
			&models.MeasurementCategory{},
			&models.Unit{},
			&models.Classification{},
			&models.GradingLevel{},
			&models.MetricCategory{},
			&models.Metric{},
			&models.MetricElementMapping{},
			&models.MetricDependency{},
			&models.Document{},
			&models.DocumentFileCleanup{},
			&models.DocumentElementMapping{},
			&models.DocumentGlossaryMapping{},
			&models.DocumentMetricMapping{},
			&models.DimensionHierarchy{},
			&models.DimensionHierarchyLevel{},
		); err != nil {
			return fmt.Errorf("auto migrate standard schema: %w", err)
		}
		return applyStandardSchemaStatements(tx)
	})
}

func prepareStandardSchemaMigration(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statement := `DO $do$ BEGIN
		IF EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'standard' AND table_name = 'documents' AND column_name = 'version'
			AND data_type IN ('character varying', 'text')
		) AND NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'standard' AND table_name = 'documents' AND column_name = 'document_version'
		) THEN
			ALTER TABLE standard.documents RENAME COLUMN version TO document_version;
		END IF;
	END $do$`
	if err := db.Exec(statement).Error; err != nil {
		return fmt.Errorf("prepare standard schema migration: %w", err)
	}
	return nil
}

// EnsureSchema 仅收紧 AutoMigrate 无法可靠变更的唯一索引和 CHECK 约束，供约束测试使用。
// 任何存量冲突都会阻止服务启动，避免在并发写入时继续扩大不一致数据。
func EnsureSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("standard schema database is required")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := acquireStandardSchemaLock(tx); err != nil {
			return err
		}
		return applyStandardSchemaStatements(tx)
	})
}

func acquireStandardSchemaLock(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	if err := db.Exec("SELECT pg_advisory_xact_lock(?)", standardSchemaLockID).Error; err != nil {
		return fmt.Errorf("acquire standard schema lock: %w", err)
	}
	return nil
}

func applyStandardSchemaStatements(db *gorm.DB) error {
	statements, err := standardSchemaStatements(db.Dialector.Name())
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("apply standard schema statement %q: %w", statement, err)
		}
	}
	return nil
}

func standardSchemaStatements(dialect string) ([]string, error) {
	switch dialect {
	case "postgres":
		return postgresStandardSchemaStatements(), nil
	case "sqlite":
		return sqliteStandardSchemaStatements(), nil
	default:
		return nil, fmt.Errorf("unsupported standard schema dialect %q", dialect)
	}
}

func postgresStandardSchemaStatements() []string {
	statements := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_domains_tenant_code ON standard.domains (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_elements_tenant_code ON standard.elements (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_code_sets_tenant_code ON standard.code_sets (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_code_items_set_code ON standard.code_items (code_set_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_measurement_categories_tenant_code ON standard.measurement_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_classifications_tenant_code ON standard.classifications (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_grading_levels_tenant_level ON standard.grading_levels (tenant_id, level)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metric_categories_tenant_code ON standard.metric_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metrics_tenant_code ON standard.metrics (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metric_element_mappings_metric_element ON standard.metric_element_mappings (metric_id, element_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metric_dependencies_from_to ON standard.metric_dependencies (from_metric_id, to_metric_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_document_element_mappings_document_element ON standard.document_element_mappings (document_id, element_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_document_glossary_mappings_document_glossary ON standard.document_glossary_mappings (document_id, glossary_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_document_metric_mappings_document_metric ON standard.document_metric_mappings (document_id, metric_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_document_file_cleanups_object_key ON standard.document_file_cleanups (object_key)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_dimension_hierarchies_tenant_code ON standard.dimension_hierarchies (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_dimension_hierarchy_levels_hierarchy_level ON standard.dimension_hierarchy_levels (hierarchy_id, level_num)",

		"DROP INDEX IF EXISTS standard.idx_codeset_tenant_code",
		"DROP INDEX IF EXISTS standard.idx_codeitem_set_code",
		"ALTER TABLE standard.measurement_categories DROP CONSTRAINT IF EXISTS measurement_categories_tenant_id_code_key",
		"ALTER TABLE standard.grading_levels DROP CONSTRAINT IF EXISTS grading_levels_tenant_id_level_key",
		"ALTER TABLE standard.metrics DROP CONSTRAINT IF EXISTS metrics_tenant_id_code_key",
		"ALTER TABLE standard.metric_element_mappings DROP CONSTRAINT IF EXISTS metric_element_mappings_metric_id_element_id_key",
		"ALTER TABLE standard.metric_dependencies DROP CONSTRAINT IF EXISTS metric_dependencies_from_metric_id_to_metric_id_key",
		"ALTER TABLE standard.document_element_mappings DROP CONSTRAINT IF EXISTS document_element_mappings_document_id_element_id_key",
		"ALTER TABLE standard.document_glossary_mappings DROP CONSTRAINT IF EXISTS document_glossary_mappings_document_id_glossary_id_key",
		"ALTER TABLE standard.document_metric_mappings DROP CONSTRAINT IF EXISTS document_metric_mappings_document_id_metric_id_key",

		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.dimension_hierarchy_levels'::regclass AND conname = 'ck_standard_dimension_hierarchy_levels_level_num') THEN ALTER TABLE standard.dimension_hierarchy_levels ADD CONSTRAINT ck_standard_dimension_hierarchy_levels_level_num CHECK (level_num > 0); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.metric_dependencies'::regclass AND conname = 'ck_standard_metric_dependencies_distinct') THEN ALTER TABLE standard.metric_dependencies ADD CONSTRAINT ck_standard_metric_dependencies_distinct CHECK (from_metric_id <> to_metric_id); END IF; END $do$",

		"CREATE INDEX IF NOT EXISTS idx_standard_glossary_element_mappings_element ON standard.glossary_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_metric_element_mappings_element ON standard.metric_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_metric_dependencies_to_metric ON standard.metric_dependencies (to_metric_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_element_mappings_element ON standard.document_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_glossary_mappings_glossary ON standard.document_glossary_mappings (glossary_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_metric_mappings_metric ON standard.document_metric_mappings (metric_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_dimension_hierarchy_levels_element ON standard.dimension_hierarchy_levels (element_id)",
	}

	// 清理早期 AutoMigrate/手工迁移生成的非统一约束名，后续仅保留下方单一路线。
	for _, legacyConstraint := range []struct {
		table string
		name  string
	}{
		{"standard.classifications", "classifications_parent_id_fkey"},
		{"standard.document_element_mappings", "document_element_mappings_document_id_fkey"},
		{"standard.document_glossary_mappings", "document_glossary_mappings_document_id_fkey"},
		{"standard.document_metric_mappings", "document_metric_mappings_document_id_fkey"},
		{"standard.dimension_hierarchy_levels", "fk_standard_dimension_hierarchies_levels"},
		{"standard.elements", "elements_classification_id_fkey"},
		{"standard.elements", "elements_unit_id_fkey"},
		{"standard.metric_categories", "metric_categories_parent_id_fkey"},
		{"standard.metric_dependencies", "metric_dependencies_from_metric_id_fkey"},
		{"standard.metric_dependencies", "metric_dependencies_to_metric_id_fkey"},
		{"standard.metric_element_mappings", "metric_element_mappings_metric_id_fkey"},
		{"standard.metrics", "metrics_base_metric_id_fkey"},
		{"standard.metrics", "metrics_category_id_fkey"},
		{"standard.metrics", "metrics_unit_id_fkey"},
		{"standard.units", "fk_standard_units_category"},
		{"standard.units", "units_category_id_fkey"},
	} {
		statements = append(statements, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s", legacyConstraint.table, legacyConstraint.name))
	}

	for _, foreignKey := range []struct {
		table      string
		name       string
		columns    string
		references string
		onDelete   string
	}{
		{"standard.domains", "fk_standard_domains_parent", "parent_id", "standard.domains(id)", "RESTRICT"},
		{"standard.glossaries", "fk_standard_glossaries_domain", "domain_id", "standard.domains(id)", "RESTRICT"},
		{"standard.elements", "fk_standard_elements_domain", "domain_id", "standard.domains(id)", "RESTRICT"},
		{"standard.elements", "fk_standard_elements_unit", "unit_id", "standard.units(id)", "RESTRICT"},
		{"standard.elements", "fk_standard_elements_classification", "classification_id", "standard.classifications(id)", "RESTRICT"},
		{"standard.elements", "fk_standard_elements_code_set", "code_set_id", "standard.code_sets(id)", "RESTRICT"},
		{"standard.code_items", "fk_standard_code_items_code_set", "code_set_id", "standard.code_sets(id)", "CASCADE"},
		{"standard.code_items", "fk_standard_code_items_parent", "parent_id", "standard.code_items(id)", "RESTRICT"},
		{"standard.units", "fk_standard_units_measurement_category", "category_id", "standard.measurement_categories(id)", "RESTRICT"},
		{"standard.classifications", "fk_standard_classifications_parent", "parent_id", "standard.classifications(id)", "RESTRICT"},
		{"standard.metric_categories", "fk_standard_metric_categories_parent", "parent_id", "standard.metric_categories(id)", "RESTRICT"},
		{"standard.metrics", "fk_standard_metrics_category", "category_id", "standard.metric_categories(id)", "RESTRICT"},
		{"standard.metrics", "fk_standard_metrics_domain", "domain_id", "standard.domains(id)", "RESTRICT"},
		{"standard.metrics", "fk_standard_metrics_unit", "unit_id", "standard.units(id)", "RESTRICT"},
		{"standard.metrics", "fk_standard_metrics_base_metric", "base_metric_id", "standard.metrics(id)", "RESTRICT"},
		{"standard.glossary_element_mappings", "fk_standard_glossary_element_mappings_glossary", "glossary_id", "standard.glossaries(id)", "CASCADE"},
		{"standard.glossary_element_mappings", "fk_standard_glossary_element_mappings_element", "element_id", "standard.elements(id)", "CASCADE"},
		{"standard.metric_element_mappings", "fk_standard_metric_element_mappings_metric", "metric_id", "standard.metrics(id)", "CASCADE"},
		{"standard.metric_element_mappings", "fk_standard_metric_element_mappings_element", "element_id", "standard.elements(id)", "CASCADE"},
		{"standard.metric_dependencies", "fk_standard_metric_dependencies_from", "from_metric_id", "standard.metrics(id)", "CASCADE"},
		{"standard.metric_dependencies", "fk_standard_metric_dependencies_to", "to_metric_id", "standard.metrics(id)", "RESTRICT"},
		{"standard.document_element_mappings", "fk_standard_document_element_mappings_document", "document_id", "standard.documents(id)", "CASCADE"},
		{"standard.document_element_mappings", "fk_standard_document_element_mappings_element", "element_id", "standard.elements(id)", "CASCADE"},
		{"standard.document_glossary_mappings", "fk_standard_document_glossary_mappings_document", "document_id", "standard.documents(id)", "CASCADE"},
		{"standard.document_glossary_mappings", "fk_standard_document_glossary_mappings_glossary", "glossary_id", "standard.glossaries(id)", "CASCADE"},
		{"standard.document_metric_mappings", "fk_standard_document_metric_mappings_document", "document_id", "standard.documents(id)", "CASCADE"},
		{"standard.document_metric_mappings", "fk_standard_document_metric_mappings_metric", "metric_id", "standard.metrics(id)", "CASCADE"},
		{"standard.dimension_hierarchies", "fk_standard_dimension_hierarchies_domain", "domain_id", "standard.domains(id)", "RESTRICT"},
		{"standard.dimension_hierarchy_levels", "fk_standard_dimension_hierarchy_levels_hierarchy", "hierarchy_id", "standard.dimension_hierarchies(id)", "CASCADE"},
		{"standard.dimension_hierarchy_levels", "fk_standard_dimension_hierarchy_levels_element", "element_id", "standard.elements(id)", "SET NULL"},
	} {
		statements = append(statements, postgresForeignKeyStatement(foreignKey.table, foreignKey.name, foreignKey.columns, foreignKey.references, foreignKey.onDelete))
	}
	return statements
}

func postgresForeignKeyStatement(table, name, columns, references, onDelete string) string {
	return fmt.Sprintf(
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = '%s'::regclass AND conname = '%s') THEN ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s ON DELETE %s; END IF; END $do$",
		table, name, table, name, columns, references, onDelete,
	)
}

func sqliteStandardSchemaStatements() []string {
	return []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_domains_tenant_code ON domains (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_elements_tenant_code ON elements (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_code_sets_tenant_code ON code_sets (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_code_items_set_code ON code_items (code_set_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_measurement_categories_tenant_code ON measurement_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_classifications_tenant_code ON classifications (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_grading_levels_tenant_level ON grading_levels (tenant_id, level)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metric_categories_tenant_code ON metric_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metrics_tenant_code ON metrics (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metric_element_mappings_metric_element ON metric_element_mappings (metric_id, element_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metric_dependencies_from_to ON metric_dependencies (from_metric_id, to_metric_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_element_mappings_document_element ON document_element_mappings (document_id, element_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_glossary_mappings_document_glossary ON document_glossary_mappings (document_id, glossary_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_metric_mappings_document_metric ON document_metric_mappings (document_id, metric_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_file_cleanups_object_key ON document_file_cleanups (object_key)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_dimension_hierarchies_tenant_code ON dimension_hierarchies (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_dimension_hierarchy_levels_hierarchy_level ON dimension_hierarchy_levels (hierarchy_id, level_num)",

		"DROP INDEX IF EXISTS standard.idx_codeset_tenant_code",
		"DROP INDEX IF EXISTS standard.idx_codeitem_set_code",

		"CREATE INDEX IF NOT EXISTS standard.idx_standard_glossary_element_mappings_element ON glossary_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_metric_element_mappings_element ON metric_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_metric_dependencies_to_metric ON metric_dependencies (to_metric_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_element_mappings_element ON document_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_glossary_mappings_glossary ON document_glossary_mappings (glossary_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_metric_mappings_metric ON document_metric_mappings (metric_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_dimension_hierarchy_levels_element ON dimension_hierarchy_levels (element_id)",
	}
}
