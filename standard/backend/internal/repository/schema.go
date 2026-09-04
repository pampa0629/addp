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
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS standard").Error; err != nil {
				return fmt.Errorf("create standard schema: %w", err)
			}
		}
		if err := acquireStandardSchemaLock(tx); err != nil {
			return err
		}
		if err := prepareStandardSchemaMigration(tx); err != nil {
			return err
		}
		if err := removeLegacyDimensionHierarchyTables(tx); err != nil {
			return err
		}
		if err := tx.AutoMigrate(
			&models.Domain{},
			&models.StandardCollection{},
			&models.StandardCollectionRevision{},
			&models.StandardCollectionMember{},
			&models.StandardCollectionAssignment{},
			&models.StandardCollectionEvent{},
			&models.Glossary{},
			&models.GlossaryElementMapping{},
			&models.Element{},
			&models.ElementRevision{},
			&models.CodeSet{},
			&models.CodeSetRevision{},
			&models.CodeSetRevisionItem{},
			&models.MeasurementCategory{},
			&models.Unit{},
			&models.MetricCategory{},
			&models.Metric{},
			&models.MetricElementMapping{},
			&models.MetricDependency{},
			&models.Document{},
			&models.DocumentFileCleanup{},
			&models.DocumentElementMapping{},
			&models.DocumentGlossaryMapping{},
			&models.DocumentMetricMapping{},
			&models.StandardReferenceDeletion{},
			&models.CatalogResourceChangeRow{},
		); err != nil {
			return fmt.Errorf("auto migrate standard schema: %w", err)
		}
		if err := migrateStandardRevisionData(tx); err != nil {
			return err
		}
		if err := migrateStandardCatalogMetricChanges(tx); err != nil {
			return err
		}
		return applyStandardSchemaStatements(tx)
	})
}

func removeLegacyDimensionHierarchyTables(db *gorm.DB) error {
	for _, statement := range []string{
		"DROP TABLE IF EXISTS standard.dimension_hierarchy_levels",
		"DROP TABLE IF EXISTS standard.dimension_hierarchies",
	} {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("remove legacy Standard dimension hierarchy tables: %w", err)
		}
	}
	return nil
}

func migrateStandardCatalogMetricChanges(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_standard_catalog_changes_tenant_id
			ON standard.catalog_resource_changes (tenant_id, id)`,
		`CREATE INDEX IF NOT EXISTS idx_standard_catalog_changes_source
			ON standard.catalog_resource_changes (tenant_id, source_type, source_identity, id DESC)`,
		`INSERT INTO standard.catalog_resource_changes (
			tenant_id, source_type, source_identity, operation, resource_version, snapshot, observed_at
		)
		SELECT
			metric.tenant_id,
			'metric',
			metric.id,
			'upsert',
			metric.version,
			jsonb_strip_nulls(jsonb_build_object(
				'name', metric.name,
				'code', metric.code,
				'object_kind', 'metric',
				'metric_type', metric.type,
				'metric_status', metric.status,
				'lifecycle_state', metric.lifecycle_state,
				'domain_id', CASE WHEN metric.domain_id IS NULL THEN NULL ELSE metric.domain_id::TEXT END,
				'category_id', CASE WHEN metric.category_id IS NULL THEN NULL ELSE metric.category_id::TEXT END,
				'unit_id', CASE WHEN metric.unit_id IS NULL THEN NULL ELSE metric.unit_id::TEXT END
			)),
			COALESCE(metric.updated_at, metric.created_at, NOW())
		FROM standard.metrics AS metric
		WHERE NOT EXISTS (
			SELECT 1 FROM standard.data_migrations WHERE version = 2026082601
		)
		ORDER BY metric.id`,
		`INSERT INTO standard.data_migrations (version, name)
		VALUES (2026082601, 'catalog_metric_change_feed_v1')
		ON CONFLICT (version) DO NOTHING`,
		`CREATE OR REPLACE FUNCTION standard.capture_metric_catalog_change()
		RETURNS TRIGGER
		LANGUAGE plpgsql
		AS $function$
		DECLARE
			changed standard.metrics%ROWTYPE;
		BEGIN
			changed := CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
			INSERT INTO standard.catalog_resource_changes (
				tenant_id, source_type, source_identity, operation, resource_version, snapshot, observed_at
			) VALUES (
				changed.tenant_id,
				'metric',
				changed.id,
				CASE WHEN TG_OP = 'DELETE' THEN 'missing' ELSE 'upsert' END,
				changed.version,
				jsonb_strip_nulls(jsonb_build_object(
					'name', changed.name,
					'code', changed.code,
					'object_kind', 'metric',
					'metric_type', changed.type,
					'metric_status', changed.status,
					'lifecycle_state', changed.lifecycle_state,
					'domain_id', CASE WHEN changed.domain_id IS NULL THEN NULL ELSE changed.domain_id::TEXT END,
					'category_id', CASE WHEN changed.category_id IS NULL THEN NULL ELSE changed.category_id::TEXT END,
					'unit_id', CASE WHEN changed.unit_id IS NULL THEN NULL ELSE changed.unit_id::TEXT END
				)),
				NOW()
			);
			RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
		END;
		$function$`,
		`DROP TRIGGER IF EXISTS trg_standard_metric_catalog_change ON standard.metrics`,
		`CREATE TRIGGER trg_standard_metric_catalog_change
		AFTER INSERT OR UPDATE OR DELETE ON standard.metrics
		FOR EACH ROW EXECUTE FUNCTION standard.capture_metric_catalog_change()`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate Standard catalog Metric changes: %w", err)
		}
	}
	return nil
}

func prepareStandardSchemaMigration(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{`DO $do$ BEGIN
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
	END $do$`,
		`DO $do$ BEGIN
			IF to_regclass('standard.code_sets') IS NOT NULL THEN
				IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'standard' AND table_name = 'code_sets' AND column_name = 'domain_id')
					AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'standard' AND table_name = 'code_sets' AND column_name = 'owner_domain_id') THEN
					ALTER TABLE standard.code_sets RENAME COLUMN domain_id TO owner_domain_id;
				END IF;
				ALTER TABLE standard.code_sets ADD COLUMN IF NOT EXISTS owner_domain_id BIGINT;
				ALTER TABLE standard.code_sets ADD COLUMN IF NOT EXISTS scope_type VARCHAR(20);
				ALTER TABLE standard.code_sets ADD COLUMN IF NOT EXISTS origin VARCHAR(20) NOT NULL DEFAULT 'tenant';
				ALTER TABLE standard.code_sets ADD COLUMN IF NOT EXISTS steward_id BIGINT;
				ALTER TABLE standard.code_sets ADD COLUMN IF NOT EXISTS tags JSONB;
				ALTER TABLE standard.code_sets ADD COLUMN IF NOT EXISTS current_revision_id BIGINT;
				ALTER TABLE standard.code_sets ADD COLUMN IF NOT EXISTS draft_revision_id BIGINT;
				ALTER TABLE standard.code_sets ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0;
				ALTER TABLE standard.code_sets ADD COLUMN IF NOT EXISTS updated_by BIGINT;
				ALTER TABLE standard.code_sets ADD COLUMN IF NOT EXISTS lifecycle_state VARCHAR(16) NOT NULL DEFAULT 'active';
				UPDATE standard.code_sets
				SET scope_type = CASE
					WHEN origin = 'platform' THEN 'platform'
					WHEN owner_domain_id IS NOT NULL THEN 'domain'
					ELSE 'tenant_common'
				END
				WHERE scope_type IS NULL OR scope_type NOT IN ('platform', 'tenant_common', 'domain');
				UPDATE standard.code_sets SET owner_domain_id = NULL WHERE scope_type = 'platform';
				ALTER TABLE standard.code_sets ALTER COLUMN scope_type SET DEFAULT 'tenant_common';
				ALTER TABLE standard.code_sets ALTER COLUMN scope_type SET NOT NULL;
			END IF;
		END $do$`,
		`DO $do$ BEGIN
			IF to_regclass('standard.elements') IS NOT NULL THEN
				IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'standard' AND table_name = 'elements' AND column_name = 'domain_id')
					AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'standard' AND table_name = 'elements' AND column_name = 'owner_domain_id') THEN
					ALTER TABLE standard.elements RENAME COLUMN domain_id TO owner_domain_id;
				END IF;
				ALTER TABLE standard.elements ADD COLUMN IF NOT EXISTS owner_domain_id BIGINT;
				ALTER TABLE standard.elements ADD COLUMN IF NOT EXISTS scope_type VARCHAR(20);
				ALTER TABLE standard.elements ADD COLUMN IF NOT EXISTS current_revision_id BIGINT;
				ALTER TABLE standard.elements ADD COLUMN IF NOT EXISTS draft_revision_id BIGINT;
				UPDATE standard.elements
				SET scope_type = CASE WHEN owner_domain_id IS NULL THEN 'tenant_common' ELSE 'domain' END
				WHERE scope_type IS NULL OR scope_type NOT IN ('platform', 'tenant_common', 'domain');
				ALTER TABLE standard.elements ALTER COLUMN scope_type SET DEFAULT 'tenant_common';
				ALTER TABLE standard.elements ALTER COLUMN scope_type SET NOT NULL;
			END IF;
		END $do$`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("prepare standard schema migration: %w", err)
		}
	}
	return nil
}

// migrateStandardRevisionData 将旧的可变标准记录一次性收敛为稳定身份和首个修订，随后删除旧列与旧码项表。
func migrateStandardRevisionData(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS standard.data_migrations (
			version BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`DO $do$ BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'standard' AND table_name = 'code_sets' AND column_name = 'name'
			) AND NOT EXISTS (SELECT 1 FROM standard.data_migrations WHERE version = 2026082801) THEN
				INSERT INTO standard.code_set_revisions (
					code_set_id, revision_no, status, name, description, value_type,
					change_summary, created_by, created_at, updated_at
				)
				SELECT id, 1, 'published', name, COALESCE(NULLIF(description, ''), name), 'string',
					'Converted to revision model', created_by, created_at, updated_at
				FROM standard.code_sets
				ORDER BY id;

				UPDATE standard.code_sets AS code_set
				SET current_revision_id = revision.id,
					origin = CASE WHEN code_set.type = 'system' THEN 'platform' ELSE 'tenant' END
				FROM standard.code_set_revisions AS revision
				WHERE revision.code_set_id = code_set.id AND revision.revision_no = 1;

				IF to_regclass('standard.code_items') IS NOT NULL THEN
					INSERT INTO standard.code_set_revision_items (
						id, code_set_revision_id, code, label, definition, sort_order, status, created_at, updated_at
					)
					SELECT item.id, code_set.current_revision_id, item.code, item.value,
						COALESCE(item.description, ''), item.sort_order,
						CASE WHEN item.is_active THEN 'active' ELSE 'deprecated' END,
						item.created_at, item.updated_at
					FROM standard.code_items AS item
					JOIN standard.code_sets AS code_set ON code_set.id = item.code_set_id
					ORDER BY item.id;
				END IF;
			END IF;
		END $do$`,
		`DO $do$ BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'standard' AND table_name = 'elements' AND column_name = 'name'
			) AND NOT EXISTS (SELECT 1 FROM standard.data_migrations WHERE version = 2026082801) THEN
				INSERT INTO standard.element_revisions (
					element_id, revision_no, status, name, definition, data_type, length, precision_num, scale,
					nullable, default_value, format, value_domain_kind, range_constraint, code_set_revision_id,
					unit_id, example_values, extra_quality_rules,
					compiled_quality_rules, change_summary, created_by, updated_by, created_at, updated_at
				)
				SELECT element.id, 1,
					CASE element.status WHEN 'approved' THEN 'published' WHEN 'deprecated' THEN 'withdrawn' ELSE 'draft' END,
					element.name, COALESCE(NULLIF(element.definition, ''), element.name), element.data_type,
					element.length, element.precision_num, element.scale, element.nullable,
					element.default_value, element.format,
					CASE WHEN element.code_set_id IS NOT NULL THEN 'enumeration'
						 WHEN element.value_range IS NOT NULL AND element.value_range <> '{}'::jsonb THEN 'range'
						 ELSE 'unrestricted' END,
					CASE WHEN element.code_set_id IS NULL THEN element.value_range ELSE NULL END,
					code_set.current_revision_id, element.unit_id, element.example_values,
					'{"schema_version":"addp.quality.rules/v1","rules":[]}'::jsonb,
					CASE WHEN element.status = 'approved' THEN COALESCE(element.quality_rules, '{"schema_version":"addp.quality.rules/v1","rules":[]}'::jsonb) ELSE NULL END,
					'Converted to revision model', element.created_by, element.updated_by, element.created_at, element.updated_at
				FROM standard.elements AS element
				LEFT JOIN standard.code_sets AS code_set ON code_set.id = element.code_set_id
				ORDER BY element.id;

				UPDATE standard.elements AS element
				SET current_revision_id = CASE WHEN revision.status = 'published' THEN revision.id ELSE NULL END,
					draft_revision_id = CASE WHEN revision.status = 'draft' THEN revision.id ELSE NULL END
				FROM standard.element_revisions AS revision
				WHERE revision.element_id = element.id AND revision.revision_no = 1;
			END IF;
		END $do$`,
		`INSERT INTO standard.data_migrations (version, name)
		 VALUES (2026082801, 'standard_element_code_set_revision_model_v1')
		 ON CONFLICT (version) DO NOTHING`,
		`ALTER TABLE standard.elements DROP CONSTRAINT IF EXISTS fk_standard_elements_unit`,
		`ALTER TABLE standard.elements DROP CONSTRAINT IF EXISTS fk_standard_elements_classification`,
		`ALTER TABLE standard.elements DROP CONSTRAINT IF EXISTS fk_standard_elements_code_set`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS name`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS data_type`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS length`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS precision_num`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS scale`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS nullable`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS default_value`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS format`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS value_range`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS unit_id`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS security_level`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS classification_id`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS code_set_id`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS definition`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS example_values`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS quality_rules`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS status`,
		`DROP TABLE IF EXISTS standard.code_items CASCADE`,
		`ALTER TABLE standard.code_sets DROP COLUMN IF EXISTS name`,
		`ALTER TABLE standard.code_sets DROP COLUMN IF EXISTS type`,
		`ALTER TABLE standard.code_sets DROP COLUMN IF EXISTS description`,
		`ALTER TABLE standard.elements DROP CONSTRAINT IF EXISTS fk_standard_elements_current_revision`,
		`ALTER TABLE standard.elements DROP CONSTRAINT IF EXISTS elements_current_revision_id_fkey`,
		`ALTER TABLE standard.code_sets DROP CONSTRAINT IF EXISTS fk_standard_code_sets_current_revision`,
		`ALTER TABLE standard.code_sets DROP CONSTRAINT IF EXISTS code_sets_current_revision_id_fkey`,
		`ALTER TABLE standard.elements DROP COLUMN IF EXISTS current_revision_id`,
		`ALTER TABLE standard.code_sets DROP COLUMN IF EXISTS current_revision_id`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate standard revision data: %w", err)
		}
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
		"DROP TRIGGER IF EXISTS trg_standard_element_revision_effective_interval ON standard.element_revisions",
		"DROP TRIGGER IF EXISTS trg_standard_code_set_revision_effective_interval ON standard.code_set_revisions",
		"ALTER TABLE standard.element_revisions DROP CONSTRAINT IF EXISTS ck_standard_element_revisions_status",
		"ALTER TABLE standard.code_set_revisions DROP CONSTRAINT IF EXISTS ck_standard_code_set_revisions_status",
		"ALTER TABLE standard.element_revisions DROP CONSTRAINT IF EXISTS ck_standard_element_revisions_effective_interval",
		"ALTER TABLE standard.code_set_revisions DROP CONSTRAINT IF EXISTS ck_standard_code_set_revisions_effective_interval",
		"UPDATE standard.element_revisions SET status = 'published' WHERE status = 'superseded'",
		"UPDATE standard.code_set_revisions SET status = 'published' WHERE status = 'superseded'",
		`WITH base AS (
			SELECT id, element_id, revision_no,
				COALESCE(effective_from, published_at, created_at, NOW()) AS base_from
			FROM standard.element_revisions WHERE status = 'published'
		), normalized AS (
			SELECT id, base_from + (ROW_NUMBER() OVER (PARTITION BY element_id, base_from ORDER BY revision_no, id) - 1) * INTERVAL '1 microsecond' AS normalized_from
			FROM base
		)
		UPDATE standard.element_revisions AS revision
		SET effective_from = normalized.normalized_from
		FROM normalized WHERE normalized.id = revision.id`,
		`WITH base AS (
			SELECT id, code_set_id, revision_no,
				COALESCE(effective_from, published_at, created_at, NOW()) AS base_from
			FROM standard.code_set_revisions WHERE status = 'published'
		), normalized AS (
			SELECT id, base_from + (ROW_NUMBER() OVER (PARTITION BY code_set_id, base_from ORDER BY revision_no, id) - 1) * INTERVAL '1 microsecond' AS normalized_from
			FROM base
		)
		UPDATE standard.code_set_revisions AS revision
		SET effective_from = normalized.normalized_from
		FROM normalized WHERE normalized.id = revision.id`,
		"UPDATE standard.element_revisions SET effective_to = NULL WHERE status = 'published' AND effective_to <= effective_from",
		"UPDATE standard.code_set_revisions SET effective_to = NULL WHERE status = 'published' AND effective_to <= effective_from",
		`WITH ordered AS (
			SELECT id, LEAD(effective_from) OVER (PARTITION BY element_id ORDER BY effective_from, revision_no, id) AS next_from
			FROM standard.element_revisions WHERE status = 'published'
		)
		UPDATE standard.element_revisions AS revision SET effective_to = ordered.next_from
		FROM ordered WHERE ordered.id = revision.id AND ordered.next_from IS NOT NULL
			AND (revision.effective_to IS NULL OR revision.effective_to > ordered.next_from)`,
		`WITH ordered AS (
			SELECT id, LEAD(effective_from) OVER (PARTITION BY code_set_id ORDER BY effective_from, revision_no, id) AS next_from
			FROM standard.code_set_revisions WHERE status = 'published'
		)
		UPDATE standard.code_set_revisions AS revision SET effective_to = ordered.next_from
		FROM ordered WHERE ordered.id = revision.id AND ordered.next_from IS NOT NULL
			AND (revision.effective_to IS NULL OR revision.effective_to > ordered.next_from)`,
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_domains_tenant_code ON standard.domains (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_collections_tenant_code ON standard.standard_collections (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_collection_revisions_collection_no ON standard.standard_collection_revisions (collection_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_collection_members_revision_member ON standard.standard_collection_members (collection_revision_id, member_type, member_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_collection_assignments_collection_principal_role ON standard.standard_collection_assignments (collection_id, principal_id, role)",
		"CREATE INDEX IF NOT EXISTS idx_standard_collection_events_collection_id ON standard.standard_collection_events (collection_id, id DESC)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_elements_tenant_code ON standard.elements (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_element_revisions_element_no ON standard.element_revisions (element_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_code_sets_tenant_code ON standard.code_sets (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_code_set_revisions_set_no ON standard.code_set_revisions (code_set_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_code_set_revision_items_revision_code ON standard.code_set_revision_items (code_set_revision_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_measurement_categories_tenant_code ON standard.measurement_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metric_categories_tenant_code ON standard.metric_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metrics_tenant_code ON standard.metrics (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metric_element_mappings_metric_element ON standard.metric_element_mappings (metric_id, element_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metric_dependencies_from_to ON standard.metric_dependencies (from_metric_id, to_metric_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_document_element_mappings_document_element ON standard.document_element_mappings (document_id, element_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_document_glossary_mappings_document_glossary ON standard.document_glossary_mappings (document_id, glossary_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_document_metric_mappings_document_metric ON standard.document_metric_mappings (document_id, metric_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_document_file_cleanups_object_key ON standard.document_file_cleanups (object_key)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_reference_deletions_resource ON standard.reference_deletions (tenant_id, resource_type, resource_id)",

		"DROP INDEX IF EXISTS standard.idx_codeset_tenant_code",
		"DROP INDEX IF EXISTS standard.idx_codeitem_set_code",
		"DROP INDEX IF EXISTS standard.idx_standard_elements_domain_id",
		"DROP INDEX IF EXISTS standard.idx_standard_code_sets_domain_id",
		"ALTER TABLE standard.measurement_categories DROP CONSTRAINT IF EXISTS measurement_categories_tenant_id_code_key",
		"ALTER TABLE standard.element_revisions DROP CONSTRAINT IF EXISTS fk_standard_element_revisions_classification",
		"ALTER TABLE standard.element_revisions DROP COLUMN IF EXISTS classification_id",
		"ALTER TABLE standard.element_revisions DROP COLUMN IF EXISTS security_level",
		"DROP TABLE IF EXISTS standard.grading_levels CASCADE",
		"DROP TABLE IF EXISTS standard.classifications CASCADE",
		"ALTER TABLE standard.metrics DROP CONSTRAINT IF EXISTS metrics_tenant_id_code_key",
		"ALTER TABLE standard.metric_element_mappings DROP CONSTRAINT IF EXISTS metric_element_mappings_metric_id_element_id_key",
		"ALTER TABLE standard.metric_dependencies DROP CONSTRAINT IF EXISTS metric_dependencies_from_metric_id_to_metric_id_key",
		"ALTER TABLE standard.document_element_mappings DROP CONSTRAINT IF EXISTS document_element_mappings_document_id_element_id_key",
		"ALTER TABLE standard.document_glossary_mappings DROP CONSTRAINT IF EXISTS document_glossary_mappings_document_id_glossary_id_key",
		"ALTER TABLE standard.document_metric_mappings DROP CONSTRAINT IF EXISTS document_metric_mappings_document_id_metric_id_key",

		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.standard_collection_revisions'::regclass AND conname = 'ck_standard_collection_revisions_status') THEN ALTER TABLE standard.standard_collection_revisions ADD CONSTRAINT ck_standard_collection_revisions_status CHECK (status IN ('draft','in_review','published','withdrawn')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.standard_collection_members'::regclass AND conname = 'ck_standard_collection_members_type') THEN ALTER TABLE standard.standard_collection_members ADD CONSTRAINT ck_standard_collection_members_type CHECK (member_type IN ('element','code_set','metric','glossary','document')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.standard_collection_assignments'::regclass AND conname = 'ck_standard_collection_assignments_role') THEN ALTER TABLE standard.standard_collection_assignments ADD CONSTRAINT ck_standard_collection_assignments_role CHECK (role IN ('owner','maintainer','reviewer')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.standard_collection_events'::regclass AND conname = 'ck_standard_collection_events_type') THEN ALTER TABLE standard.standard_collection_events ADD CONSTRAINT ck_standard_collection_events_type CHECK (event_type IN ('created','draft_created','draft_updated','submitted','returned','published','assignments_replaced')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.metric_dependencies'::regclass AND conname = 'ck_standard_metric_dependencies_distinct') THEN ALTER TABLE standard.metric_dependencies ADD CONSTRAINT ck_standard_metric_dependencies_distinct CHECK (from_metric_id <> to_metric_id); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.element_revisions'::regclass AND conname = 'ck_standard_element_revisions_status') THEN ALTER TABLE standard.element_revisions ADD CONSTRAINT ck_standard_element_revisions_status CHECK (status IN ('draft','in_review','published','withdrawn')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.element_revisions'::regclass AND conname = 'ck_standard_element_revisions_effective_interval') THEN ALTER TABLE standard.element_revisions ADD CONSTRAINT ck_standard_element_revisions_effective_interval CHECK ((status <> 'published' OR effective_from IS NOT NULL) AND (effective_to IS NULL OR (effective_from IS NOT NULL AND effective_from < effective_to))); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.element_revisions'::regclass AND conname = 'ck_standard_element_revisions_value_domain') THEN ALTER TABLE standard.element_revisions ADD CONSTRAINT ck_standard_element_revisions_value_domain CHECK ((value_domain_kind = 'unrestricted' AND range_constraint IS NULL AND code_set_revision_id IS NULL) OR (value_domain_kind = 'range' AND range_constraint IS NOT NULL AND code_set_revision_id IS NULL) OR (value_domain_kind = 'enumeration' AND range_constraint IS NULL AND code_set_revision_id IS NOT NULL)); END IF; END $do$",
		"ALTER TABLE standard.elements DROP CONSTRAINT IF EXISTS ck_standard_elements_scope",
		"ALTER TABLE standard.elements ADD CONSTRAINT ck_standard_elements_scope CHECK ((scope_type = 'platform' AND owner_domain_id IS NULL) OR (scope_type = 'tenant_common' AND owner_domain_id IS NULL) OR (scope_type = 'domain' AND owner_domain_id IS NOT NULL))",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.code_sets'::regclass AND conname = 'ck_standard_code_sets_origin') THEN ALTER TABLE standard.code_sets ADD CONSTRAINT ck_standard_code_sets_origin CHECK (origin IN ('platform','tenant')); END IF; END $do$",
		"ALTER TABLE standard.code_sets DROP CONSTRAINT IF EXISTS ck_standard_code_sets_tenant_domain",
		"ALTER TABLE standard.code_sets DROP CONSTRAINT IF EXISTS ck_standard_code_sets_scope",
		"ALTER TABLE standard.code_sets ADD CONSTRAINT ck_standard_code_sets_scope CHECK ((origin = 'platform' AND scope_type = 'platform' AND owner_domain_id IS NULL) OR (origin = 'tenant' AND scope_type = 'tenant_common' AND owner_domain_id IS NULL) OR (origin = 'tenant' AND scope_type = 'domain' AND owner_domain_id IS NOT NULL))",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.code_set_revisions'::regclass AND conname = 'ck_standard_code_set_revisions_status') THEN ALTER TABLE standard.code_set_revisions ADD CONSTRAINT ck_standard_code_set_revisions_status CHECK (status IN ('draft','in_review','published','withdrawn')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.code_set_revisions'::regclass AND conname = 'ck_standard_code_set_revisions_effective_interval') THEN ALTER TABLE standard.code_set_revisions ADD CONSTRAINT ck_standard_code_set_revisions_effective_interval CHECK ((status <> 'published' OR effective_from IS NOT NULL) AND (effective_to IS NULL OR (effective_from IS NOT NULL AND effective_from < effective_to))); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.code_set_revisions'::regclass AND conname = 'ck_standard_code_set_revisions_value_type') THEN ALTER TABLE standard.code_set_revisions ADD CONSTRAINT ck_standard_code_set_revisions_value_type CHECK (value_type IN ('string','int','bigint')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.code_set_revision_items'::regclass AND conname = 'ck_standard_code_set_revision_items_status') THEN ALTER TABLE standard.code_set_revision_items ADD CONSTRAINT ck_standard_code_set_revision_items_status CHECK (status IN ('active','deprecated')); END IF; END $do$",
		`CREATE OR REPLACE FUNCTION standard.enforce_element_revision_effective_interval()
		RETURNS TRIGGER LANGUAGE plpgsql AS $function$
		BEGIN
			IF NEW.status = 'published' AND EXISTS (
				SELECT 1 FROM standard.element_revisions AS existing
				WHERE existing.element_id = NEW.element_id
					AND existing.id <> NEW.id
					AND existing.status = 'published'
					AND tstzrange(existing.effective_from, existing.effective_to, '[)') && tstzrange(NEW.effective_from, NEW.effective_to, '[)')
			) THEN
				RAISE EXCEPTION 'published data element revision effective intervals overlap' USING ERRCODE = '23514';
			END IF;
			RETURN NEW;
		END;
		$function$`,
		"DROP TRIGGER IF EXISTS trg_standard_element_revision_effective_interval ON standard.element_revisions",
		`CREATE TRIGGER trg_standard_element_revision_effective_interval
		BEFORE INSERT OR UPDATE OF element_id, status, effective_from, effective_to ON standard.element_revisions
		FOR EACH ROW EXECUTE FUNCTION standard.enforce_element_revision_effective_interval()`,
		`CREATE OR REPLACE FUNCTION standard.enforce_code_set_revision_effective_interval()
		RETURNS TRIGGER LANGUAGE plpgsql AS $function$
		BEGIN
			IF NEW.status = 'published' AND EXISTS (
				SELECT 1 FROM standard.code_set_revisions AS existing
				WHERE existing.code_set_id = NEW.code_set_id
					AND existing.id <> NEW.id
					AND existing.status = 'published'
					AND tstzrange(existing.effective_from, existing.effective_to, '[)') && tstzrange(NEW.effective_from, NEW.effective_to, '[)')
			) THEN
				RAISE EXCEPTION 'published code set revision effective intervals overlap' USING ERRCODE = '23514';
			END IF;
			RETURN NEW;
		END;
		$function$`,
		"DROP TRIGGER IF EXISTS trg_standard_code_set_revision_effective_interval ON standard.code_set_revisions",
		`CREATE TRIGGER trg_standard_code_set_revision_effective_interval
		BEFORE INSERT OR UPDATE OF code_set_id, status, effective_from, effective_to ON standard.code_set_revisions
		FOR EACH ROW EXECUTE FUNCTION standard.enforce_code_set_revision_effective_interval()`,

		"CREATE INDEX IF NOT EXISTS idx_standard_glossary_element_mappings_element ON standard.glossary_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_metric_element_mappings_element ON standard.metric_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_metric_dependencies_to_metric ON standard.metric_dependencies (to_metric_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_element_mappings_element ON standard.document_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_glossary_mappings_glossary ON standard.document_glossary_mappings (glossary_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_metric_mappings_metric ON standard.document_metric_mappings (metric_id)",
	}

	// 清理早期 AutoMigrate/手工迁移生成的非统一约束名，后续仅保留下方单一路线。
	for _, legacyConstraint := range []struct {
		table string
		name  string
	}{
		{"standard.document_element_mappings", "document_element_mappings_document_id_fkey"},
		{"standard.document_glossary_mappings", "document_glossary_mappings_document_id_fkey"},
		{"standard.document_metric_mappings", "document_metric_mappings_document_id_fkey"},
		{"standard.elements", "elements_unit_id_fkey"},
		{"standard.elements", "fk_standard_elements_domain"},
		{"standard.elements", "elements_domain_id_fkey"},
		{"standard.code_sets", "fk_standard_code_sets_domain"},
		{"standard.code_sets", "code_sets_domain_id_fkey"},
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
		{"standard.standard_collections", "fk_standard_collections_draft_revision", "draft_revision_id", "standard.standard_collection_revisions(id)", "SET NULL"},
		{"standard.standard_collection_revisions", "fk_standard_collection_revisions_collection", "collection_id", "standard.standard_collections(id)", "CASCADE"},
		{"standard.standard_collection_members", "fk_standard_collection_members_revision", "collection_revision_id", "standard.standard_collection_revisions(id)", "CASCADE"},
		{"standard.standard_collection_assignments", "fk_standard_collection_assignments_collection", "collection_id", "standard.standard_collections(id)", "CASCADE"},
		{"standard.standard_collection_events", "fk_standard_collection_events_collection", "collection_id", "standard.standard_collections(id)", "CASCADE"},
		{"standard.standard_collection_events", "fk_standard_collection_events_revision", "revision_id", "standard.standard_collection_revisions(id)", "CASCADE"},
		{"standard.glossaries", "fk_standard_glossaries_domain", "domain_id", "standard.domains(id)", "RESTRICT"},
		{"standard.elements", "fk_standard_elements_owner_domain", "owner_domain_id", "standard.domains(id)", "RESTRICT"},
		{"standard.elements", "fk_standard_elements_draft_revision", "draft_revision_id", "standard.element_revisions(id)", "SET NULL"},
		{"standard.element_revisions", "fk_standard_element_revisions_element", "element_id", "standard.elements(id)", "CASCADE"},
		{"standard.element_revisions", "fk_standard_element_revisions_unit", "unit_id", "standard.units(id)", "RESTRICT"},
		{"standard.element_revisions", "fk_standard_element_revisions_code_set_revision", "code_set_revision_id", "standard.code_set_revisions(id)", "RESTRICT"},
		{"standard.code_sets", "fk_standard_code_sets_owner_domain", "owner_domain_id", "standard.domains(id)", "RESTRICT"},
		{"standard.code_sets", "fk_standard_code_sets_draft_revision", "draft_revision_id", "standard.code_set_revisions(id)", "SET NULL"},
		{"standard.code_set_revisions", "fk_standard_code_set_revisions_code_set", "code_set_id", "standard.code_sets(id)", "CASCADE"},
		{"standard.code_set_revision_items", "fk_standard_code_set_revision_items_revision", "code_set_revision_id", "standard.code_set_revisions(id)", "CASCADE"},
		{"standard.code_set_revision_items", "fk_standard_code_set_revision_items_replacement", "replacement_item_id", "standard.code_set_revision_items(id)", "RESTRICT"},
		{"standard.units", "fk_standard_units_measurement_category", "category_id", "standard.measurement_categories(id)", "RESTRICT"},
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
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_collections_tenant_code ON standard_collections (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_collection_revisions_collection_no ON standard_collection_revisions (collection_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_collection_members_revision_member ON standard_collection_members (collection_revision_id, member_type, member_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_collection_assignments_collection_principal_role ON standard_collection_assignments (collection_id, principal_id, role)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_collection_events_collection_id ON standard_collection_events (collection_id, id DESC)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_elements_tenant_code ON elements (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_element_revisions_element_no ON element_revisions (element_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_code_sets_tenant_code ON code_sets (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_code_set_revisions_set_no ON code_set_revisions (code_set_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_code_set_revision_items_revision_code ON code_set_revision_items (code_set_revision_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_measurement_categories_tenant_code ON measurement_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metric_categories_tenant_code ON metric_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metrics_tenant_code ON metrics (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metric_element_mappings_metric_element ON metric_element_mappings (metric_id, element_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metric_dependencies_from_to ON metric_dependencies (from_metric_id, to_metric_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_element_mappings_document_element ON document_element_mappings (document_id, element_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_glossary_mappings_document_glossary ON document_glossary_mappings (document_id, glossary_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_metric_mappings_document_metric ON document_metric_mappings (document_id, metric_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_file_cleanups_object_key ON document_file_cleanups (object_key)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_reference_deletions_resource ON reference_deletions (tenant_id, resource_type, resource_id)",

		"DROP INDEX IF EXISTS standard.idx_codeset_tenant_code",
		"DROP INDEX IF EXISTS standard.idx_codeitem_set_code",

		"CREATE INDEX IF NOT EXISTS standard.idx_standard_glossary_element_mappings_element ON glossary_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_metric_element_mappings_element ON metric_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_metric_dependencies_to_metric ON metric_dependencies (to_metric_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_element_mappings_element ON document_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_glossary_mappings_glossary ON document_glossary_mappings (glossary_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_metric_mappings_metric ON document_metric_mappings (metric_id)",
	}
}
