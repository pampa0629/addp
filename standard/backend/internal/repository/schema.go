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
		if err := prepareGlossaryRevisionMigration(tx); err != nil {
			return err
		}
		if err := prepareDocumentRevisionMigration(tx); err != nil {
			return err
		}
		if err := prepareMetricDefinitionMigration(tx); err != nil {
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
			&models.GlossaryRevision{},
			&models.GlossaryElementMapping{},
			&models.Element{},
			&models.ElementRevision{},
			&models.CodeSet{},
			&models.CodeSetRevision{},
			&models.CodeSetRevisionItem{},
			&models.MeasurementCategory{},
			&models.Unit{},
			&models.MetricCategory{},
			&models.MetricDefinition{},
			&models.MetricDefinitionRevision{},
			&models.MetricDefinitionRevisionDependency{},
			&models.Document{},
			&models.DocumentRevision{},
			&models.DocumentExtraction{},
			&models.DocumentExtractionCandidate{},
			&models.DocumentExtractionEvidence{},
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
		if err := migrateGlossaryRevisionData(tx); err != nil {
			return err
		}
		if err := migrateDocumentRevisionData(tx); err != nil {
			return err
		}
		if err := alignCodeSetRevisionItemSequence(tx); err != nil {
			return err
		}
		if err := migrateMetricDefinitionData(tx); err != nil {
			return err
		}
		if err := migrateStandardCatalogMetricChanges(tx); err != nil {
			return err
		}
		return applyStandardSchemaStatements(tx)
	})
}

func prepareGlossaryRevisionMigration(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statement := `DO $do$ BEGIN
		IF to_regclass('standard.glossaries') IS NOT NULL THEN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='standard' AND table_name='glossaries' AND column_name='domain_id')
				AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='standard' AND table_name='glossaries' AND column_name='owner_domain_id') THEN
				ALTER TABLE standard.glossaries RENAME COLUMN domain_id TO owner_domain_id;
			END IF;
			ALTER TABLE standard.glossaries ADD COLUMN IF NOT EXISTS owner_domain_id BIGINT;
			ALTER TABLE standard.glossaries ADD COLUMN IF NOT EXISTS scope_type VARCHAR(20);
			ALTER TABLE standard.glossaries ADD COLUMN IF NOT EXISTS code VARCHAR(100);
			ALTER TABLE standard.glossaries ADD COLUMN IF NOT EXISTS draft_revision_id BIGINT;
			ALTER TABLE standard.glossaries ADD COLUMN IF NOT EXISTS lifecycle_state VARCHAR(16) NOT NULL DEFAULT 'active';
			UPDATE standard.glossaries SET scope_type=CASE WHEN owner_domain_id IS NULL THEN 'tenant_common' ELSE 'domain' END
			WHERE scope_type IS NULL OR scope_type NOT IN ('platform','tenant_common','domain');
			UPDATE standard.glossaries SET code='glossary_' || id::text WHERE code IS NULL OR BTRIM(code)='';
			ALTER TABLE standard.glossaries ALTER COLUMN scope_type SET DEFAULT 'tenant_common';
			ALTER TABLE standard.glossaries ALTER COLUMN scope_type SET NOT NULL;
			ALTER TABLE standard.glossaries ALTER COLUMN code SET NOT NULL;
		END IF;
	END $do$`
	if err := db.Exec(statement).Error; err != nil {
		return fmt.Errorf("prepare Glossary revision migration: %w", err)
	}
	return nil
}

func migrateGlossaryRevisionData(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`DO $do$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='standard' AND table_name='glossaries' AND column_name='name')
				AND NOT EXISTS (SELECT 1 FROM standard.data_migrations WHERE version=2026090501) THEN
				INSERT INTO standard.glossary_revisions (
					glossary_id, revision_no, status, name, alias, definition, example, note, related_ids,
					change_summary, effective_from, effective_to, created_by, updated_by, created_at, updated_at
				)
				SELECT id, 1,
					CASE status WHEN 'approved' THEN 'published' WHEN 'deprecated' THEN 'withdrawn' ELSE 'draft' END,
					name, COALESCE(alias, '[]'::jsonb), COALESCE(NULLIF(definition,''), name), COALESCE(example,''), COALESCE(note,''), COALESCE(related_ids, '[]'::jsonb),
					'从旧业务术语模型迁移',
					CASE WHEN status IN ('approved','deprecated') THEN COALESCE(created_at,NOW()) ELSE NULL END,
					CASE WHEN status='deprecated' THEN COALESCE(updated_at,NOW()) ELSE NULL END,
					created_by, updated_by, created_at, updated_at
				FROM standard.glossaries
				ORDER BY id;

				UPDATE standard.glossaries AS glossary
				SET draft_revision_id=revision.id
				FROM standard.glossary_revisions AS revision
				WHERE revision.glossary_id=glossary.id AND revision.revision_no=1 AND revision.status IN ('draft','in_review');
			END IF;
		END $do$`,
		`INSERT INTO standard.data_migrations (version, name) VALUES (2026090501, 'standard_glossary_revision_model_v1') ON CONFLICT (version) DO NOTHING`,
		"ALTER TABLE standard.glossaries DROP CONSTRAINT IF EXISTS fk_standard_glossaries_domain",
		"ALTER TABLE standard.glossaries DROP CONSTRAINT IF EXISTS glossaries_domain_id_fkey",
		"ALTER TABLE standard.glossaries DROP COLUMN IF EXISTS name CASCADE",
		"ALTER TABLE standard.glossaries DROP COLUMN IF EXISTS alias CASCADE",
		"ALTER TABLE standard.glossaries DROP COLUMN IF EXISTS definition CASCADE",
		"ALTER TABLE standard.glossaries DROP COLUMN IF EXISTS example CASCADE",
		"ALTER TABLE standard.glossaries DROP COLUMN IF EXISTS note CASCADE",
		"ALTER TABLE standard.glossaries DROP COLUMN IF EXISTS status CASCADE",
		"ALTER TABLE standard.glossaries DROP COLUMN IF EXISTS related_ids CASCADE",
		"ALTER TABLE standard.glossaries DROP COLUMN IF EXISTS domain_id CASCADE",
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate Glossary revision data: %w", err)
		}
	}
	return nil
}

func prepareDocumentRevisionMigration(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statement := `DO $do$ BEGIN
		IF to_regclass('standard.documents') IS NOT NULL THEN
			ALTER TABLE standard.documents ADD COLUMN IF NOT EXISTS owner_domain_id BIGINT;
			ALTER TABLE standard.documents ADD COLUMN IF NOT EXISTS scope_type VARCHAR(20);
			ALTER TABLE standard.documents ADD COLUMN IF NOT EXISTS code VARCHAR(100);
			ALTER TABLE standard.documents ADD COLUMN IF NOT EXISTS steward_id BIGINT;
			ALTER TABLE standard.documents ADD COLUMN IF NOT EXISTS tags JSONB;
			ALTER TABLE standard.documents ADD COLUMN IF NOT EXISTS draft_revision_id BIGINT;
			ALTER TABLE standard.documents ADD COLUMN IF NOT EXISTS lifecycle_state VARCHAR(16) NOT NULL DEFAULT 'active';
			UPDATE standard.documents SET scope_type=CASE WHEN owner_domain_id IS NULL THEN 'tenant_common' ELSE 'domain' END
			WHERE scope_type IS NULL OR scope_type NOT IN ('platform','tenant_common','domain');
			UPDATE standard.documents SET code='document_' || id::text WHERE code IS NULL OR BTRIM(code)='';
			ALTER TABLE standard.documents ALTER COLUMN scope_type SET DEFAULT 'tenant_common';
			ALTER TABLE standard.documents ALTER COLUMN scope_type SET NOT NULL;
			ALTER TABLE standard.documents ALTER COLUMN code SET NOT NULL;
		END IF;
	END $do$`
	if err := db.Exec(statement).Error; err != nil {
		return fmt.Errorf("prepare Document revision migration: %w", err)
	}
	return nil
}

func migrateDocumentRevisionData(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`DO $do$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='standard' AND table_name='documents' AND column_name='name')
				AND NOT EXISTS (SELECT 1 FROM standard.data_migrations WHERE version=2026090601) THEN
				INSERT INTO standard.document_revisions (
					document_id, revision_no, status, name, version_label, publish_date, description,
					file_key, file_name, file_size, media_type, content_sha256, change_summary,
					created_by, updated_by, created_at, updated_at
				)
				SELECT id, 1, 'draft', name, COALESCE(document_version,''), publish_date, COALESCE(description,''),
					COALESCE(file_key,''), COALESCE(file_name,''), COALESCE(file_size,0),
					CASE WHEN LOWER(COALESCE(file_name,'')) LIKE '%.md' THEN 'text/markdown' ELSE 'application/octet-stream' END,
					'', '从旧标准文档模型迁移', created_by, updated_by, created_at, updated_at
				FROM standard.documents ORDER BY id;

				UPDATE standard.documents AS document SET draft_revision_id=revision.id
				FROM standard.document_revisions AS revision
				WHERE revision.document_id=document.id AND revision.revision_no=1;
			END IF;
		END $do$`,
		`INSERT INTO standard.data_migrations (version, name) VALUES (2026090601, 'standard_document_revision_model_v1') ON CONFLICT (version) DO NOTHING`,
		"ALTER TABLE standard.documents DROP COLUMN IF EXISTS name CASCADE",
		"ALTER TABLE standard.documents DROP COLUMN IF EXISTS document_version CASCADE",
		"ALTER TABLE standard.documents DROP COLUMN IF EXISTS publish_date CASCADE",
		"ALTER TABLE standard.documents DROP COLUMN IF EXISTS description CASCADE",
		"ALTER TABLE standard.documents DROP COLUMN IF EXISTS file_key CASCADE",
		"ALTER TABLE standard.documents DROP COLUMN IF EXISTS file_name CASCADE",
		"ALTER TABLE standard.documents DROP COLUMN IF EXISTS file_size CASCADE",
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate Document revision data: %w", err)
		}
	}
	return nil
}

// alignCodeSetRevisionItemSequence repairs the identity sequence after the
// one-time legacy migration preserves code_items.id values explicitly.
func alignCodeSetRevisionItemSequence(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statement := `
		SELECT setval('standard.code_set_revision_items_id_seq', MAX(item.id), true)
		FROM standard.code_set_revision_items AS item
		CROSS JOIN standard.code_set_revision_items_id_seq AS sequence_state
		GROUP BY sequence_state.last_value, sequence_state.is_called
		HAVING MAX(item.id) IS NOT NULL
			AND (MAX(item.id) > sequence_state.last_value OR NOT sequence_state.is_called)`
	if err := db.Exec(statement).Error; err != nil {
		return fmt.Errorf("align code set revision item sequence: %w", err)
	}
	return nil
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

func prepareMetricDefinitionMigration(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`DO $do$ BEGIN IF to_regclass('standard.metrics') IS NOT NULL THEN
			DROP TRIGGER IF EXISTS trg_standard_metric_catalog_change ON standard.metrics;
		END IF; END $do$`,
		`DO $do$ BEGIN
			IF to_regclass('standard.metrics') IS NOT NULL AND to_regclass('standard.metric_definitions') IS NULL THEN
				ALTER TABLE standard.metrics RENAME TO metric_definitions;
			END IF;
		END $do$`,
		`DO $do$ BEGIN
			IF to_regclass('standard.metric_definitions') IS NOT NULL THEN
				IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='standard' AND table_name='metric_definitions' AND column_name='domain_id')
					AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='standard' AND table_name='metric_definitions' AND column_name='owner_domain_id') THEN
					ALTER TABLE standard.metric_definitions RENAME COLUMN domain_id TO owner_domain_id;
				END IF;
				ALTER TABLE standard.metric_definitions ADD COLUMN IF NOT EXISTS scope_type VARCHAR(20);
				ALTER TABLE standard.metric_definitions ADD COLUMN IF NOT EXISTS owner_domain_id BIGINT;
				ALTER TABLE standard.metric_definitions ADD COLUMN IF NOT EXISTS draft_revision_id BIGINT;
				UPDATE standard.metric_definitions SET scope_type=CASE WHEN owner_domain_id IS NULL THEN 'tenant_common' ELSE 'domain' END WHERE scope_type IS NULL;
				ALTER TABLE standard.metric_definitions ALTER COLUMN scope_type SET DEFAULT 'tenant_common';
				ALTER TABLE standard.metric_definitions ALTER COLUMN scope_type SET NOT NULL;
			END IF;
		END $do$`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("prepare MetricDefinition migration: %w", err)
		}
	}
	return nil
}

func migrateMetricDefinitionData(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	statements := []string{
		`DO $do$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='standard' AND table_name='metric_definitions' AND column_name='name') THEN
				INSERT INTO standard.metric_definition_revisions (
					metric_definition_id, revision_no, status, metric_type, name, definition, statistical_caliber,
					semantic_formula, unit_id, change_summary, effective_from, effective_to,
					created_by, updated_by, created_at, updated_at
				)
				SELECT m.id, 1,
					CASE m.status WHEN 'approved' THEN 'published' WHEN 'deprecated' THEN 'withdrawn' ELSE 'draft' END,
					CASE WHEN m.type IN ('atomic','derived','composite') THEN m.type ELSE 'atomic' END,
					m.name, COALESCE(NULLIF(m.definition,''), m.name), COALESCE(NULLIF(m.definition,''), m.name),
					COALESCE(m.formula,''), m.unit_id, '从旧指标模型迁移',
					CASE WHEN m.status IN ('approved','deprecated') THEN COALESCE(m.created_at,NOW()) ELSE NULL END,
					CASE WHEN m.status='deprecated' THEN COALESCE(m.updated_at,NOW()) ELSE NULL END,
					m.created_by, m.updated_by, m.created_at, m.updated_at
				FROM standard.metric_definitions m
				WHERE NOT EXISTS (SELECT 1 FROM standard.metric_definition_revisions r WHERE r.metric_definition_id=m.id);
			END IF;
		END $do$`,
		`DO $do$ BEGIN
			IF to_regclass('standard.metric_dependencies') IS NOT NULL THEN
				INSERT INTO standard.metric_definition_revision_dependencies (
					metric_definition_revision_id, dependency_definition_id, dependency_revision_id, relation_kind, coefficient, note, created_at
				)
				SELECT source_revision.id, dependency.to_metric_id,
					CASE WHEN source_revision.status IN ('published','withdrawn') THEN target_revision.id ELSE NULL END,
					'component', dependency.coefficient, dependency.note, dependency.created_at
				FROM standard.metric_dependencies dependency
				JOIN standard.metric_definition_revisions source_revision ON source_revision.metric_definition_id=dependency.from_metric_id AND source_revision.revision_no=1
				JOIN standard.metric_definition_revisions target_revision ON target_revision.metric_definition_id=dependency.to_metric_id AND target_revision.revision_no=1
				ON CONFLICT DO NOTHING;
			END IF;
		END $do$`,
		`DO $do$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='standard' AND table_name='metric_definitions' AND column_name='base_metric_id') THEN
				INSERT INTO standard.metric_definition_revision_dependencies (
					metric_definition_revision_id, dependency_definition_id, dependency_revision_id, relation_kind, created_at
				)
				SELECT source_revision.id, m.base_metric_id,
					CASE WHEN source_revision.status IN ('published','withdrawn') THEN target_revision.id ELSE NULL END,
					'base', COALESCE(m.created_at,NOW())
				FROM standard.metric_definitions m
				JOIN standard.metric_definition_revisions source_revision ON source_revision.metric_definition_id=m.id AND source_revision.revision_no=1
				JOIN standard.metric_definition_revisions target_revision ON target_revision.metric_definition_id=m.base_metric_id AND target_revision.revision_no=1
				WHERE m.base_metric_id IS NOT NULL
				ON CONFLICT DO NOTHING;
			END IF;
		END $do$`,
		`UPDATE standard.metric_definitions m SET draft_revision_id=r.id
		 FROM standard.metric_definition_revisions r
		 WHERE r.metric_definition_id=m.id AND r.status IN ('draft','in_review') AND m.draft_revision_id IS NULL`,
		"DROP TABLE IF EXISTS standard.metric_element_mappings CASCADE",
		"DROP TABLE IF EXISTS standard.metric_dependencies CASCADE",
		"ALTER TABLE standard.metric_definitions DROP COLUMN IF EXISTS name CASCADE",
		"ALTER TABLE standard.metric_definitions DROP COLUMN IF EXISTS type CASCADE",
		"ALTER TABLE standard.metric_definitions DROP COLUMN IF EXISTS definition CASCADE",
		"ALTER TABLE standard.metric_definitions DROP COLUMN IF EXISTS formula CASCADE",
		"ALTER TABLE standard.metric_definitions DROP COLUMN IF EXISTS unit_id CASCADE",
		"ALTER TABLE standard.metric_definitions DROP COLUMN IF EXISTS base_metric_id CASCADE",
		"ALTER TABLE standard.metric_definitions DROP COLUMN IF EXISTS derivation_config CASCADE",
		"ALTER TABLE standard.metric_definitions DROP COLUMN IF EXISTS status CASCADE",
		"ALTER TABLE standard.metric_definitions DROP COLUMN IF EXISTS domain_id CASCADE",
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate MetricDefinition data: %w", err)
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
				'name', revision.name,
				'code', metric.code,
				'object_kind', 'metric',
				'metric_type', revision.metric_type,
				'metric_status', revision.status,
				'lifecycle_state', metric.lifecycle_state,
				'domain_id', CASE WHEN metric.owner_domain_id IS NULL THEN NULL ELSE metric.owner_domain_id::TEXT END,
				'category_id', CASE WHEN metric.category_id IS NULL THEN NULL ELSE metric.category_id::TEXT END,
				'unit_id', CASE WHEN revision.unit_id IS NULL THEN NULL ELSE revision.unit_id::TEXT END
			)),
			COALESCE(metric.updated_at, metric.created_at, NOW())
		FROM standard.metric_definitions AS metric
		LEFT JOIN LATERAL (
			SELECT r.* FROM standard.metric_definition_revisions r
			WHERE r.metric_definition_id=metric.id
			ORDER BY CASE r.status WHEN 'draft' THEN 0 WHEN 'in_review' THEN 1 WHEN 'published' THEN 2 ELSE 3 END, r.revision_no DESC LIMIT 1
		) revision ON TRUE
		WHERE NOT EXISTS (
			SELECT 1 FROM standard.data_migrations WHERE version = 2026090401
		)
		ORDER BY metric.id`,
		`INSERT INTO standard.data_migrations (version, name)
		VALUES (2026090401, 'catalog_metric_definition_change_feed_v2')
		ON CONFLICT (version) DO NOTHING`,
		`CREATE OR REPLACE FUNCTION standard.capture_metric_catalog_change()
		RETURNS TRIGGER
		LANGUAGE plpgsql
		AS $function$
		DECLARE
			changed standard.metric_definitions%ROWTYPE;
			revision standard.metric_definition_revisions%ROWTYPE;
		BEGIN
			changed := CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
			IF TG_OP <> 'DELETE' THEN
				SELECT r.* INTO revision FROM standard.metric_definition_revisions r
				WHERE r.metric_definition_id=changed.id
				ORDER BY CASE r.status WHEN 'draft' THEN 0 WHEN 'in_review' THEN 1 WHEN 'published' THEN 2 ELSE 3 END, r.revision_no DESC LIMIT 1;
			END IF;
			INSERT INTO standard.catalog_resource_changes (
				tenant_id, source_type, source_identity, operation, resource_version, snapshot, observed_at
			) VALUES (
				changed.tenant_id,
				'metric',
				changed.id,
				CASE WHEN TG_OP = 'DELETE' THEN 'missing' ELSE 'upsert' END,
				changed.version,
				jsonb_strip_nulls(jsonb_build_object(
					'name', revision.name,
					'code', changed.code,
					'object_kind', 'metric',
					'metric_type', revision.metric_type,
					'metric_status', revision.status,
					'lifecycle_state', changed.lifecycle_state,
					'domain_id', CASE WHEN changed.owner_domain_id IS NULL THEN NULL ELSE changed.owner_domain_id::TEXT END,
					'category_id', CASE WHEN changed.category_id IS NULL THEN NULL ELSE changed.category_id::TEXT END,
					'unit_id', CASE WHEN revision.unit_id IS NULL THEN NULL ELSE revision.unit_id::TEXT END
				)),
				NOW()
			);
			RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
		END;
		$function$`,
		`DROP TRIGGER IF EXISTS trg_standard_metric_catalog_change ON standard.metric_definitions`,
		`CREATE TRIGGER trg_standard_metric_catalog_change
		AFTER INSERT OR UPDATE OR DELETE ON standard.metric_definitions
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
				IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'standard' AND table_name = 'code_sets' AND column_name = 'domain_id') THEN
					UPDATE standard.code_sets
					SET owner_domain_id = CASE WHEN origin = 'platform' THEN NULL ELSE COALESCE(owner_domain_id, domain_id) END,
						scope_type = CASE
							WHEN origin = 'platform' THEN 'platform'
							WHEN COALESCE(owner_domain_id, domain_id) IS NOT NULL THEN 'domain'
							ELSE 'tenant_common'
						END;
				END IF;
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
				IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'standard' AND table_name = 'elements' AND column_name = 'domain_id') THEN
					UPDATE standard.elements
					SET owner_domain_id = COALESCE(owner_domain_id, domain_id),
						scope_type = CASE
							WHEN COALESCE(owner_domain_id, domain_id) IS NOT NULL THEN 'domain'
							WHEN scope_type = 'platform' THEN 'platform'
							ELSE 'tenant_common'
						END;
				END IF;
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
		"DROP TRIGGER IF EXISTS trg_standard_glossary_revision_effective_interval ON standard.glossary_revisions",
		"DROP TRIGGER IF EXISTS trg_standard_element_revision_effective_interval ON standard.element_revisions",
		"DROP TRIGGER IF EXISTS trg_standard_code_set_revision_effective_interval ON standard.code_set_revisions",
		"DROP TRIGGER IF EXISTS trg_standard_metric_revision_effective_interval ON standard.metric_definition_revisions",
		"ALTER TABLE standard.element_revisions DROP CONSTRAINT IF EXISTS ck_standard_element_revisions_status",
		"ALTER TABLE standard.code_set_revisions DROP CONSTRAINT IF EXISTS ck_standard_code_set_revisions_status",
		"ALTER TABLE standard.metric_definition_revisions DROP CONSTRAINT IF EXISTS ck_standard_metric_definition_revisions_status",
		"ALTER TABLE standard.element_revisions DROP CONSTRAINT IF EXISTS ck_standard_element_revisions_effective_interval",
		"ALTER TABLE standard.code_set_revisions DROP CONSTRAINT IF EXISTS ck_standard_code_set_revisions_effective_interval",
		"ALTER TABLE standard.metric_definition_revisions DROP CONSTRAINT IF EXISTS ck_standard_metric_definition_revisions_effective_interval",
		"ALTER TABLE standard.glossary_revisions DROP CONSTRAINT IF EXISTS ck_standard_glossary_revisions_status",
		"ALTER TABLE standard.glossary_revisions DROP CONSTRAINT IF EXISTS ck_standard_glossary_revisions_effective_interval",
		"UPDATE standard.element_revisions SET status = 'published' WHERE status = 'superseded'",
		"UPDATE standard.code_set_revisions SET status = 'published' WHERE status = 'superseded'",
		`WITH base AS (
			SELECT id, glossary_id, revision_no,
				COALESCE(effective_from, published_at, created_at, NOW()) AS base_from
			FROM standard.glossary_revisions WHERE status = 'published'
		), normalized AS (
			SELECT id, base_from + (ROW_NUMBER() OVER (PARTITION BY glossary_id, base_from ORDER BY revision_no, id) - 1) * INTERVAL '1 microsecond' AS normalized_from
			FROM base
		)
		UPDATE standard.glossary_revisions AS revision
		SET effective_from = normalized.normalized_from
		FROM normalized WHERE normalized.id = revision.id`,
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
		`WITH base AS (
			SELECT id, metric_definition_id, revision_no,
				COALESCE(effective_from, published_at, created_at, NOW()) AS base_from
			FROM standard.metric_definition_revisions WHERE status = 'published'
		), normalized AS (
			SELECT id, base_from + (ROW_NUMBER() OVER (PARTITION BY metric_definition_id, base_from ORDER BY revision_no, id) - 1) * INTERVAL '1 microsecond' AS normalized_from
			FROM base
		)
		UPDATE standard.metric_definition_revisions AS revision
		SET effective_from = normalized.normalized_from
		FROM normalized WHERE normalized.id = revision.id`,
		"UPDATE standard.element_revisions SET effective_to = NULL WHERE status = 'published' AND effective_to <= effective_from",
		"UPDATE standard.glossary_revisions SET effective_to = NULL WHERE status = 'published' AND effective_to <= effective_from",
		"UPDATE standard.code_set_revisions SET effective_to = NULL WHERE status = 'published' AND effective_to <= effective_from",
		"UPDATE standard.metric_definition_revisions SET effective_to = NULL WHERE status = 'published' AND effective_to <= effective_from",
		`WITH ordered AS (
			SELECT id, LEAD(effective_from) OVER (PARTITION BY glossary_id ORDER BY effective_from, revision_no, id) AS next_from
			FROM standard.glossary_revisions WHERE status = 'published'
		)
		UPDATE standard.glossary_revisions AS revision SET effective_to = ordered.next_from
		FROM ordered WHERE ordered.id = revision.id AND ordered.next_from IS NOT NULL
			AND (revision.effective_to IS NULL OR revision.effective_to > ordered.next_from)`,
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
		`WITH ordered AS (
			SELECT id, LEAD(effective_from) OVER (PARTITION BY metric_definition_id ORDER BY effective_from, revision_no, id) AS next_from
			FROM standard.metric_definition_revisions WHERE status = 'published'
		)
		UPDATE standard.metric_definition_revisions AS revision SET effective_to = ordered.next_from
		FROM ordered WHERE ordered.id = revision.id AND ordered.next_from IS NOT NULL
			AND (revision.effective_to IS NULL OR revision.effective_to > ordered.next_from)`,
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_domains_tenant_code ON standard.domains (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_collections_tenant_code ON standard.standard_collections (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_collection_revisions_collection_no ON standard.standard_collection_revisions (collection_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_collection_members_revision_member ON standard.standard_collection_members (collection_revision_id, member_type, member_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_collection_assignments_collection_principal_role ON standard.standard_collection_assignments (collection_id, principal_id, role)",
		"CREATE INDEX IF NOT EXISTS idx_standard_collection_events_collection_id ON standard.standard_collection_events (collection_id, id DESC)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_glossaries_tenant_code ON standard.glossaries (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_glossary_revisions_glossary_no ON standard.glossary_revisions (glossary_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_elements_tenant_code ON standard.elements (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_element_revisions_element_no ON standard.element_revisions (element_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_code_sets_tenant_code ON standard.code_sets (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_code_set_revisions_set_no ON standard.code_set_revisions (code_set_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_code_set_revision_items_revision_code ON standard.code_set_revision_items (code_set_revision_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_measurement_categories_tenant_code ON standard.measurement_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metric_categories_tenant_code ON standard.metric_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metric_definitions_tenant_code ON standard.metric_definitions (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metric_definition_revisions_definition_no ON standard.metric_definition_revisions (metric_definition_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_metric_revision_dependencies ON standard.metric_definition_revision_dependencies (metric_definition_revision_id, dependency_definition_id, relation_kind)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_documents_tenant_code ON standard.documents (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_document_revisions_document_no ON standard.document_revisions (document_id, revision_no)",
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
		"ALTER TABLE standard.metric_definitions DROP CONSTRAINT IF EXISTS metrics_tenant_id_code_key",
		"ALTER TABLE standard.documents DROP CONSTRAINT IF EXISTS documents_tenant_id_code_key",
		"ALTER TABLE standard.document_element_mappings DROP CONSTRAINT IF EXISTS document_element_mappings_document_id_element_id_key",
		"ALTER TABLE standard.document_glossary_mappings DROP CONSTRAINT IF EXISTS document_glossary_mappings_document_id_glossary_id_key",
		"ALTER TABLE standard.document_metric_mappings DROP CONSTRAINT IF EXISTS document_metric_mappings_document_id_metric_id_key",

		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.standard_collection_revisions'::regclass AND conname = 'ck_standard_collection_revisions_status') THEN ALTER TABLE standard.standard_collection_revisions ADD CONSTRAINT ck_standard_collection_revisions_status CHECK (status IN ('draft','in_review','published','withdrawn')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.standard_collection_members'::regclass AND conname = 'ck_standard_collection_members_type') THEN ALTER TABLE standard.standard_collection_members ADD CONSTRAINT ck_standard_collection_members_type CHECK (member_type IN ('element','code_set','metric','glossary','document')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.standard_collection_assignments'::regclass AND conname = 'ck_standard_collection_assignments_role') THEN ALTER TABLE standard.standard_collection_assignments ADD CONSTRAINT ck_standard_collection_assignments_role CHECK (role IN ('owner','maintainer','reviewer')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.standard_collection_events'::regclass AND conname = 'ck_standard_collection_events_type') THEN ALTER TABLE standard.standard_collection_events ADD CONSTRAINT ck_standard_collection_events_type CHECK (event_type IN ('created','draft_created','draft_updated','submitted','returned','published','assignments_replaced')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.glossary_revisions'::regclass AND conname = 'ck_standard_glossary_revisions_status') THEN ALTER TABLE standard.glossary_revisions ADD CONSTRAINT ck_standard_glossary_revisions_status CHECK (status IN ('draft','in_review','published','withdrawn')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.glossary_revisions'::regclass AND conname = 'ck_standard_glossary_revisions_effective_interval') THEN ALTER TABLE standard.glossary_revisions ADD CONSTRAINT ck_standard_glossary_revisions_effective_interval CHECK ((status <> 'published' OR effective_from IS NOT NULL) AND (effective_to IS NULL OR (effective_from IS NOT NULL AND effective_from < effective_to))); END IF; END $do$",
		"ALTER TABLE standard.glossaries DROP CONSTRAINT IF EXISTS ck_standard_glossaries_scope",
		"ALTER TABLE standard.glossaries ADD CONSTRAINT ck_standard_glossaries_scope CHECK ((scope_type = 'platform' AND owner_domain_id IS NULL) OR (scope_type = 'tenant_common' AND owner_domain_id IS NULL) OR (scope_type = 'domain' AND owner_domain_id IS NOT NULL))",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.metric_definition_revisions'::regclass AND conname = 'ck_standard_metric_definition_revisions_status') THEN ALTER TABLE standard.metric_definition_revisions ADD CONSTRAINT ck_standard_metric_definition_revisions_status CHECK (status IN ('draft','in_review','published','withdrawn')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.metric_definition_revisions'::regclass AND conname = 'ck_standard_metric_definition_revisions_type') THEN ALTER TABLE standard.metric_definition_revisions ADD CONSTRAINT ck_standard_metric_definition_revisions_type CHECK (metric_type IN ('atomic','derived','composite')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.metric_definition_revisions'::regclass AND conname = 'ck_standard_metric_definition_revisions_effective_interval') THEN ALTER TABLE standard.metric_definition_revisions ADD CONSTRAINT ck_standard_metric_definition_revisions_effective_interval CHECK ((status <> 'published' OR effective_from IS NOT NULL) AND (effective_to IS NULL OR (effective_from IS NOT NULL AND effective_from < effective_to))); END IF; END $do$",
		"ALTER TABLE standard.metric_definitions DROP CONSTRAINT IF EXISTS ck_standard_metric_definitions_scope",
		"ALTER TABLE standard.metric_definitions ADD CONSTRAINT ck_standard_metric_definitions_scope CHECK ((scope_type = 'platform' AND owner_domain_id IS NULL) OR (scope_type = 'tenant_common' AND owner_domain_id IS NULL) OR (scope_type = 'domain' AND owner_domain_id IS NOT NULL))",
		"ALTER TABLE standard.documents DROP CONSTRAINT IF EXISTS ck_standard_documents_scope",
		"ALTER TABLE standard.documents ADD CONSTRAINT ck_standard_documents_scope CHECK ((scope_type = 'platform' AND owner_domain_id IS NULL) OR (scope_type = 'tenant_common' AND owner_domain_id IS NULL) OR (scope_type = 'domain' AND owner_domain_id IS NOT NULL))",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.documents'::regclass AND conname = 'ck_standard_documents_type') THEN ALTER TABLE standard.documents ADD CONSTRAINT ck_standard_documents_type CHECK (doc_type IN ('national','industry','internal','reference')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.document_revisions'::regclass AND conname = 'ck_standard_document_revisions_status') THEN ALTER TABLE standard.document_revisions ADD CONSTRAINT ck_standard_document_revisions_status CHECK (status IN ('draft','in_review','published','withdrawn')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.document_revisions'::regclass AND conname = 'ck_standard_document_revisions_effective_interval') THEN ALTER TABLE standard.document_revisions ADD CONSTRAINT ck_standard_document_revisions_effective_interval CHECK ((status <> 'published' OR effective_from IS NOT NULL) AND (effective_to IS NULL OR (effective_from IS NOT NULL AND effective_from < effective_to))); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.document_extractions'::regclass AND conname = 'ck_standard_document_extractions_status') THEN ALTER TABLE standard.document_extractions ADD CONSTRAINT ck_standard_document_extractions_status CHECK (status = 'completed'); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.document_extraction_candidates'::regclass AND conname = 'ck_standard_document_extraction_candidates_type') THEN ALTER TABLE standard.document_extraction_candidates ADD CONSTRAINT ck_standard_document_extraction_candidates_type CHECK (candidate_type IN ('glossary','element','code_set','metric')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.document_extraction_candidates'::regclass AND conname = 'ck_standard_document_extraction_candidates_status') THEN ALTER TABLE standard.document_extraction_candidates ADD CONSTRAINT ck_standard_document_extraction_candidates_status CHECK (status IN ('pending','retained','rejected')); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.document_extraction_evidences'::regclass AND conname = 'ck_standard_document_extraction_evidences_lines') THEN ALTER TABLE standard.document_extraction_evidences ADD CONSTRAINT ck_standard_document_extraction_evidences_lines CHECK (start_line > 0 AND end_line >= start_line); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.metric_definition_revision_dependencies'::regclass AND conname = 'ck_standard_metric_revision_dependencies_distinct') THEN ALTER TABLE standard.metric_definition_revision_dependencies ADD CONSTRAINT ck_standard_metric_revision_dependencies_distinct CHECK (metric_definition_revision_id > 0 AND dependency_definition_id > 0); END IF; END $do$",
		"DO $do$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'standard.metric_definition_revision_dependencies'::regclass AND conname = 'ck_standard_metric_revision_dependencies_kind') THEN ALTER TABLE standard.metric_definition_revision_dependencies ADD CONSTRAINT ck_standard_metric_revision_dependencies_kind CHECK (relation_kind IN ('base','component')); END IF; END $do$",
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
		`CREATE OR REPLACE FUNCTION standard.enforce_glossary_revision_effective_interval()
		RETURNS TRIGGER LANGUAGE plpgsql AS $function$
		BEGIN
			IF NEW.status = 'published' AND EXISTS (
				SELECT 1 FROM standard.glossary_revisions AS existing
				WHERE existing.glossary_id = NEW.glossary_id
					AND existing.id <> NEW.id
					AND existing.status = 'published'
					AND tstzrange(existing.effective_from, existing.effective_to, '[)') && tstzrange(NEW.effective_from, NEW.effective_to, '[)')
			) THEN
				RAISE EXCEPTION 'published glossary revision effective intervals overlap' USING ERRCODE = '23514';
			END IF;
			RETURN NEW;
		END;
		$function$`,
		"DROP TRIGGER IF EXISTS trg_standard_glossary_revision_effective_interval ON standard.glossary_revisions",
		`CREATE TRIGGER trg_standard_glossary_revision_effective_interval
		BEFORE INSERT OR UPDATE OF glossary_id, status, effective_from, effective_to ON standard.glossary_revisions
		FOR EACH ROW EXECUTE FUNCTION standard.enforce_glossary_revision_effective_interval()`,
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
		`CREATE OR REPLACE FUNCTION standard.enforce_metric_revision_effective_interval()
		RETURNS TRIGGER LANGUAGE plpgsql AS $function$
		BEGIN
			IF NEW.status = 'published' AND EXISTS (
				SELECT 1 FROM standard.metric_definition_revisions AS existing
				WHERE existing.metric_definition_id = NEW.metric_definition_id
					AND existing.id <> NEW.id
					AND existing.status = 'published'
					AND tstzrange(existing.effective_from, existing.effective_to, '[)') && tstzrange(NEW.effective_from, NEW.effective_to, '[)')
			) THEN
				RAISE EXCEPTION 'published metric definition revision effective intervals overlap' USING ERRCODE = '23514';
			END IF;
			RETURN NEW;
		END;
		$function$`,
		"DROP TRIGGER IF EXISTS trg_standard_metric_revision_effective_interval ON standard.metric_definition_revisions",
		`CREATE TRIGGER trg_standard_metric_revision_effective_interval
		BEFORE INSERT OR UPDATE OF metric_definition_id, status, effective_from, effective_to ON standard.metric_definition_revisions
		FOR EACH ROW EXECUTE FUNCTION standard.enforce_metric_revision_effective_interval()`,
		`CREATE OR REPLACE FUNCTION standard.enforce_document_revision_effective_interval()
		RETURNS TRIGGER LANGUAGE plpgsql AS $function$
		BEGIN
			IF NEW.status = 'published' AND EXISTS (
				SELECT 1 FROM standard.document_revisions AS existing
				WHERE existing.document_id = NEW.document_id
					AND existing.id <> NEW.id
					AND existing.status = 'published'
					AND tstzrange(existing.effective_from, existing.effective_to, '[)') && tstzrange(NEW.effective_from, NEW.effective_to, '[)')
			) THEN
				RAISE EXCEPTION 'published document revision effective intervals overlap' USING ERRCODE = '23514';
			END IF;
			RETURN NEW;
		END;
		$function$`,
		"DROP TRIGGER IF EXISTS trg_standard_document_revision_effective_interval ON standard.document_revisions",
		`CREATE TRIGGER trg_standard_document_revision_effective_interval
		BEFORE INSERT OR UPDATE OF document_id, status, effective_from, effective_to ON standard.document_revisions
		FOR EACH ROW EXECUTE FUNCTION standard.enforce_document_revision_effective_interval()`,

		"CREATE INDEX IF NOT EXISTS idx_standard_glossary_element_mappings_element ON standard.glossary_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_metric_revision_dependencies_definition ON standard.metric_definition_revision_dependencies (dependency_definition_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_metric_revision_dependencies_revision ON standard.metric_definition_revision_dependencies (dependency_revision_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_element_mappings_element ON standard.document_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_glossary_mappings_glossary ON standard.document_glossary_mappings (glossary_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_metric_mappings_metric ON standard.document_metric_mappings (metric_id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_extractions_revision ON standard.document_extractions (document_revision_id, id DESC)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_extraction_candidates_extraction ON standard.document_extraction_candidates (extraction_id, id)",
		"CREATE INDEX IF NOT EXISTS idx_standard_document_extraction_evidences_revision ON standard.document_extraction_evidences (document_revision_id, id)",
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
		{"standard.metric_definitions", "metrics_base_metric_id_fkey"},
		{"standard.metric_definitions", "metrics_category_id_fkey"},
		{"standard.metric_definitions", "metrics_unit_id_fkey"},
		{"standard.units", "fk_standard_units_category"},
		{"standard.units", "units_category_id_fkey"},
		{"standard.document_extraction_candidates", "fk_standard_document_extractions_candidates"},
		{"standard.document_extraction_evidences", "fk_standard_document_extraction_candidates_evidences"},
	} {
		statements = append(statements, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s", legacyConstraint.table, legacyConstraint.name))
	}
	statements = append(statements,
		"ALTER TABLE standard.elements DROP COLUMN IF EXISTS domain_id",
		"ALTER TABLE standard.code_sets DROP COLUMN IF EXISTS domain_id",
	)

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
		{"standard.glossaries", "fk_standard_glossaries_owner_domain", "owner_domain_id", "standard.domains(id)", "RESTRICT"},
		{"standard.glossaries", "fk_standard_glossaries_draft_revision", "draft_revision_id", "standard.glossary_revisions(id)", "SET NULL"},
		{"standard.glossary_revisions", "fk_standard_glossary_revisions_glossary", "glossary_id", "standard.glossaries(id)", "CASCADE"},
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
		{"standard.metric_definitions", "fk_standard_metric_definitions_category", "category_id", "standard.metric_categories(id)", "RESTRICT"},
		{"standard.metric_definitions", "fk_standard_metric_definitions_owner_domain", "owner_domain_id", "standard.domains(id)", "RESTRICT"},
		{"standard.metric_definitions", "fk_standard_metric_definitions_draft_revision", "draft_revision_id", "standard.metric_definition_revisions(id)", "SET NULL"},
		{"standard.metric_definition_revisions", "fk_standard_metric_definition_revisions_definition", "metric_definition_id", "standard.metric_definitions(id)", "CASCADE"},
		{"standard.metric_definition_revisions", "fk_standard_metric_definition_revisions_unit", "unit_id", "standard.units(id)", "RESTRICT"},
		{"standard.metric_definition_revision_dependencies", "fk_standard_metric_revision_dependencies_revision", "metric_definition_revision_id", "standard.metric_definition_revisions(id)", "CASCADE"},
		{"standard.metric_definition_revision_dependencies", "fk_standard_metric_revision_dependencies_definition", "dependency_definition_id", "standard.metric_definitions(id)", "RESTRICT"},
		{"standard.metric_definition_revision_dependencies", "fk_standard_metric_revision_dependencies_frozen_revision", "dependency_revision_id", "standard.metric_definition_revisions(id)", "RESTRICT"},
		{"standard.documents", "fk_standard_documents_owner_domain", "owner_domain_id", "standard.domains(id)", "RESTRICT"},
		{"standard.documents", "fk_standard_documents_draft_revision", "draft_revision_id", "standard.document_revisions(id)", "SET NULL"},
		{"standard.document_revisions", "fk_standard_document_revisions_document", "document_id", "standard.documents(id)", "CASCADE"},
		{"standard.document_extractions", "fk_standard_document_extractions_revision", "document_revision_id", "standard.document_revisions(id)", "CASCADE"},
		{"standard.document_extraction_candidates", "fk_standard_document_extraction_candidates_extraction", "extraction_id", "standard.document_extractions(id)", "CASCADE"},
		{"standard.document_extraction_evidences", "fk_standard_document_extraction_evidences_candidate", "candidate_id", "standard.document_extraction_candidates(id)", "CASCADE"},
		{"standard.document_extraction_evidences", "fk_standard_document_extraction_evidences_revision", "document_revision_id", "standard.document_revisions(id)", "CASCADE"},
		{"standard.glossary_element_mappings", "fk_standard_glossary_element_mappings_glossary", "glossary_id", "standard.glossaries(id)", "CASCADE"},
		{"standard.glossary_element_mappings", "fk_standard_glossary_element_mappings_element", "element_id", "standard.elements(id)", "CASCADE"},
		{"standard.document_element_mappings", "fk_standard_document_element_mappings_document", "document_id", "standard.documents(id)", "CASCADE"},
		{"standard.document_element_mappings", "fk_standard_document_element_mappings_element", "element_id", "standard.elements(id)", "CASCADE"},
		{"standard.document_glossary_mappings", "fk_standard_document_glossary_mappings_document", "document_id", "standard.documents(id)", "CASCADE"},
		{"standard.document_glossary_mappings", "fk_standard_document_glossary_mappings_glossary", "glossary_id", "standard.glossaries(id)", "CASCADE"},
		{"standard.document_metric_mappings", "fk_standard_document_metric_mappings_document", "document_id", "standard.documents(id)", "CASCADE"},
		{"standard.document_metric_mappings", "fk_standard_document_metric_mappings_metric", "metric_id", "standard.metric_definitions(id)", "CASCADE"},
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
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_glossaries_tenant_code ON glossaries (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_glossary_revisions_glossary_no ON glossary_revisions (glossary_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_elements_tenant_code ON elements (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_element_revisions_element_no ON element_revisions (element_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_code_sets_tenant_code ON code_sets (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_code_set_revisions_set_no ON code_set_revisions (code_set_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_code_set_revision_items_revision_code ON code_set_revision_items (code_set_revision_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_measurement_categories_tenant_code ON measurement_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metric_categories_tenant_code ON metric_categories (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metric_definitions_tenant_code ON metric_definitions (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metric_definition_revisions_definition_no ON metric_definition_revisions (metric_definition_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_metric_revision_dependencies ON metric_definition_revision_dependencies (metric_definition_revision_id, dependency_definition_id, relation_kind)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_documents_tenant_code ON documents (tenant_id, code)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_revisions_document_no ON document_revisions (document_id, revision_no)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_element_mappings_document_element ON document_element_mappings (document_id, element_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_glossary_mappings_document_glossary ON document_glossary_mappings (document_id, glossary_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_metric_mappings_document_metric ON document_metric_mappings (document_id, metric_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_document_file_cleanups_object_key ON document_file_cleanups (object_key)",
		"CREATE UNIQUE INDEX IF NOT EXISTS standard.uq_standard_reference_deletions_resource ON reference_deletions (tenant_id, resource_type, resource_id)",

		"DROP INDEX IF EXISTS standard.idx_codeset_tenant_code",
		"DROP INDEX IF EXISTS standard.idx_codeitem_set_code",

		"CREATE INDEX IF NOT EXISTS standard.idx_standard_glossary_element_mappings_element ON glossary_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_metric_revision_dependencies_definition ON metric_definition_revision_dependencies (dependency_definition_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_metric_revision_dependencies_revision ON metric_definition_revision_dependencies (dependency_revision_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_element_mappings_element ON document_element_mappings (element_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_glossary_mappings_glossary ON document_glossary_mappings (glossary_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_metric_mappings_metric ON document_metric_mappings (metric_id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_extractions_revision ON document_extractions (document_revision_id, id DESC)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_extraction_candidates_extraction ON document_extraction_candidates (extraction_id, id)",
		"CREATE INDEX IF NOT EXISTS standard.idx_standard_document_extraction_evidences_revision ON document_extraction_evidences (document_revision_id, id)",
	}
}
