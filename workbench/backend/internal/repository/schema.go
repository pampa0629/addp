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
		if err := tx.AutoMigrate(&models.View{}, &models.DataApplication{}, &models.DataApplicationRevision{}); err != nil {
			return fmt.Errorf("auto migrate workbench schema: %w", err)
		}
		if tx.Dialector.Name() != "postgres" {
			return nil
		}
		statements := []string{
			`ALTER TABLE workbench.views DROP CONSTRAINT IF EXISTS ck_workbench_views_service_ref`,
			`ALTER TABLE workbench.views ADD CONSTRAINT ck_workbench_views_service_ref CHECK (service_type = 'query' AND service_id > 0)`,
			`ALTER TABLE workbench.views DROP CONSTRAINT IF EXISTS ck_workbench_views_renderer_type`,
			`ALTER TABLE workbench.views ADD CONSTRAINT ck_workbench_views_renderer_type CHECK (renderer_type IN ('table', 'chart', 'map'))`,
			`ALTER TABLE workbench.views DROP CONSTRAINT IF EXISTS ck_workbench_views_version`,
			`ALTER TABLE workbench.views ADD CONSTRAINT ck_workbench_views_version CHECK (version > 0)`,
			`CREATE INDEX IF NOT EXISTS idx_workbench_views_owner_updated ON workbench.views (tenant_id, owner_user_id, updated_at DESC, id)`,
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
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("apply workbench constraint: %w", err)
			}
		}
		return nil
	})
}
