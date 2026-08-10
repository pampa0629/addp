package repository

import (
	"fmt"

	"gorm.io/gorm"
)

// EnsureSchema applies constraints that GORM AutoMigrate cannot tighten on
// tables created by older Quality versions. A failure is intentional: serving
// data that violates the current Quality contract is worse than refusing to
// start with an actionable migration error.
func EnsureSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("quality schema database is required")
	}
	statements := []string{
		"ALTER TABLE quality.rule_applications ALTER COLUMN schema_name SET NOT NULL",
		"ALTER TABLE quality.rule_applications ALTER COLUMN table_name SET NOT NULL",
		"ALTER TABLE quality.rule_applications ALTER COLUMN column_name SET NOT NULL",
		"ALTER TABLE quality.check_tasks ALTER COLUMN schema_name SET NOT NULL",
		"ALTER TABLE quality.check_tasks ALTER COLUMN table_name SET NOT NULL",
		"ALTER TABLE quality.issues ALTER COLUMN schema_name SET NOT NULL",
		"ALTER TABLE quality.issues DROP CONSTRAINT IF EXISTS quality_issues_status_check",
		"ALTER TABLE quality.issues ADD CONSTRAINT quality_issues_status_check CHECK (status IN ('open', 'resolved', 'ignored'))",
		"ALTER TABLE quality.check_tasks DROP COLUMN IF EXISTS next_run_at",
		"CREATE INDEX IF NOT EXISTS idx_quality_check_tasks_tenant_updated ON quality.check_tasks (tenant_id, updated_at DESC, id DESC)",
		"CREATE INDEX IF NOT EXISTS idx_quality_issues_tenant_status_updated ON quality.issues (tenant_id, status, updated_at DESC, id DESC)",
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("apply quality schema statement %q: %w", statement, err)
		}
	}
	return nil
}
