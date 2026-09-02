package repository

import (
	"fmt"

	"github.com/addp/workbench/internal/models"
	"gorm.io/gorm"
)

const workbenchSchemaLockID int64 = 2026082602

func Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("workbench schema database is required")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS workbench").Error; err != nil {
				return fmt.Errorf("create workbench schema: %w", err)
			}
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", workbenchSchemaLockID).Error; err != nil {
				return fmt.Errorf("acquire workbench schema lock: %w", err)
			}
		}
		if err := tx.AutoMigrate(&models.DataApplication{}, &models.DataApplicationRevision{}, &models.CatalogResourceChangeRow{}, &models.ResourceAccessRule{}); err != nil {
			return fmt.Errorf("auto migrate workbench schema: %w", err)
		}
		if tx.Dialector.Name() != "postgres" {
			return nil
		}
		statements := []string{
			`ALTER TABLE workbench.data_applications DROP CONSTRAINT IF EXISTS ck_workbench_data_applications_status`,
			`ALTER TABLE workbench.data_applications ADD CONSTRAINT ck_workbench_data_applications_status CHECK (publication_status IN ('unpublished', 'published', 'offline'))`,
			`ALTER TABLE workbench.data_applications DROP CONSTRAINT IF EXISTS ck_workbench_data_applications_version`,
			`ALTER TABLE workbench.data_applications ADD CONSTRAINT ck_workbench_data_applications_version CHECK (version > 0)`,
			`ALTER TABLE workbench.data_applications DROP CONSTRAINT IF EXISTS ck_workbench_data_applications_revision`,
			`ALTER TABLE workbench.data_applications ADD CONSTRAINT ck_workbench_data_applications_revision CHECK ((publication_status = 'unpublished' AND current_revision_number IS NULL AND current_revision_hash = '') OR (publication_status IN ('published', 'offline') AND current_revision_number > 0 AND current_revision_hash LIKE 'sha256:%'))`,
			`ALTER TABLE workbench.data_application_revisions DROP CONSTRAINT IF EXISTS ck_workbench_data_application_revisions_number`,
			`ALTER TABLE workbench.data_application_revisions ADD CONSTRAINT ck_workbench_data_application_revisions_number CHECK (revision_number > 0)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS ux_workbench_data_applications_id_tenant ON workbench.data_applications (id, tenant_id)`,
			`ALTER TABLE workbench.data_application_revisions DROP CONSTRAINT IF EXISTS fk_workbench_application_revisions_application`,
			`ALTER TABLE workbench.data_application_revisions ADD CONSTRAINT fk_workbench_application_revisions_application FOREIGN KEY (application_id, tenant_id) REFERENCES workbench.data_applications(id, tenant_id) ON DELETE RESTRICT`,
			`CREATE INDEX IF NOT EXISTS idx_workbench_applications_owner_updated ON workbench.data_applications (tenant_id, owner_user_id, updated_at DESC, id)`,
			`CREATE INDEX IF NOT EXISTS idx_workbench_application_revisions_current ON workbench.data_application_revisions (tenant_id, application_id, revision_number DESC)`,
			`ALTER TABLE workbench.resource_access_rules DROP CONSTRAINT IF EXISTS ck_workbench_resource_access_rule_resource`,
			`ALTER TABLE workbench.resource_access_rules ADD CONSTRAINT ck_workbench_resource_access_rule_resource CHECK (resource_type = 'data_application' AND resource_id IS NOT NULL)`,
			`ALTER TABLE workbench.resource_access_rules DROP CONSTRAINT IF EXISTS ck_workbench_resource_access_rule_subject`,
			`ALTER TABLE workbench.resource_access_rules ADD CONSTRAINT ck_workbench_resource_access_rule_subject CHECK (subject_type = 'user' AND subject_id > 0)`,
			`ALTER TABLE workbench.resource_access_rules DROP CONSTRAINT IF EXISTS ck_workbench_resource_access_rule_permission`,
			`ALTER TABLE workbench.resource_access_rules ADD CONSTRAINT ck_workbench_resource_access_rule_permission CHECK (permission = 'workbench.data_application.execute')`,
			`ALTER TABLE workbench.resource_access_rules DROP CONSTRAINT IF EXISTS ck_workbench_resource_access_rule_effect`,
			`ALTER TABLE workbench.resource_access_rules ADD CONSTRAINT ck_workbench_resource_access_rule_effect CHECK (effect IN ('allow', 'deny'))`,
			`ALTER TABLE workbench.resource_access_rules DROP CONSTRAINT IF EXISTS ck_workbench_resource_access_rule_source`,
			`ALTER TABLE workbench.resource_access_rules ADD CONSTRAINT ck_workbench_resource_access_rule_source CHECK (source_module = 'asset' AND source_identity ~ '^[1-9][0-9]*$')`,
			`ALTER TABLE workbench.resource_access_rules DROP CONSTRAINT IF EXISTS ck_workbench_resource_access_rule_expiry`,
			`CREATE TABLE IF NOT EXISTS workbench.data_migrations (
				version BIGINT PRIMARY KEY,
				name TEXT NOT NULL,
				applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`,
			`DROP TABLE IF EXISTS workbench.views`,
			`INSERT INTO workbench.data_migrations (version, name)
			VALUES (2026082801, 'remove_workbench_view_intermediate_resource')
			ON CONFLICT (version) DO NOTHING`,
			`CREATE INDEX IF NOT EXISTS idx_workbench_catalog_changes_tenant_id
				ON workbench.catalog_resource_changes (tenant_id, id)`,
			`CREATE INDEX IF NOT EXISTS idx_workbench_catalog_changes_source
				ON workbench.catalog_resource_changes (tenant_id, source_type, source_identity, id DESC)`,
			`INSERT INTO workbench.catalog_resource_changes (
				tenant_id, source_type, source_identity, operation, snapshot, observed_at
			)
			SELECT
				application.tenant_id,
				'data_application',
				application.id,
				'upsert',
				jsonb_build_object(
					'name', revision.name,
					'description', revision.description,
					'object_kind', 'data_application',
					'publication_status', application.publication_status,
					'revision_number', application.current_revision_number,
					'runtime_path', '/data-apps/' || application.id::text
				),
				COALESCE(application.updated_at, revision.published_at, NOW())
			FROM workbench.data_applications AS application
			JOIN workbench.data_application_revisions AS revision
			  ON revision.tenant_id = application.tenant_id
			 AND revision.application_id = application.id
			 AND revision.revision_number = application.current_revision_number
			WHERE application.current_revision_number IS NOT NULL
			  AND NOT EXISTS (
				SELECT 1 FROM workbench.data_migrations WHERE version = 2026082701
			  )
			ORDER BY application.tenant_id, application.id`,
			`INSERT INTO workbench.data_migrations (version, name)
			VALUES (2026082701, 'catalog_data_application_change_feed_v1')
			ON CONFLICT (version) DO NOTHING`,
			`CREATE OR REPLACE FUNCTION workbench.capture_data_application_catalog_change()
			RETURNS TRIGGER
			LANGUAGE plpgsql
			AS $function$
			BEGIN
				IF NEW.current_revision_number IS NULL
				   OR (NEW.current_revision_number IS NOT DISTINCT FROM OLD.current_revision_number
				       AND NEW.publication_status = OLD.publication_status) THEN
					RETURN NEW;
				END IF;
				INSERT INTO workbench.catalog_resource_changes (
					tenant_id, source_type, source_identity, operation, snapshot, observed_at
				)
				SELECT
					NEW.tenant_id,
					'data_application',
					NEW.id,
					'upsert',
					jsonb_build_object(
						'name', revision.name,
						'description', revision.description,
						'object_kind', 'data_application',
						'publication_status', NEW.publication_status,
						'revision_number', NEW.current_revision_number,
						'runtime_path', '/data-apps/' || NEW.id::text
					),
					NOW()
				FROM workbench.data_application_revisions AS revision
				WHERE revision.tenant_id = NEW.tenant_id
				  AND revision.application_id = NEW.id
				  AND revision.revision_number = NEW.current_revision_number;
				IF NOT FOUND THEN
					RAISE EXCEPTION 'current Workbench application revision is missing';
				END IF;
				RETURN NEW;
			END;
			$function$`,
			`DROP TRIGGER IF EXISTS trg_workbench_data_application_catalog_change ON workbench.data_applications`,
			`CREATE TRIGGER trg_workbench_data_application_catalog_change
			AFTER UPDATE ON workbench.data_applications
			FOR EACH ROW EXECUTE FUNCTION workbench.capture_data_application_catalog_change()`,
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("apply workbench constraint: %w", err)
			}
		}
		return nil
	})
}
