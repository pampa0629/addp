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
		if err := tx.AutoMigrate(&models.View{}); err != nil {
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
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("apply workbench constraint: %w", err)
			}
		}
		return nil
	})
}
