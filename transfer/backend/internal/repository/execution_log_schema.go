package repository

import (
	"fmt"

	"gorm.io/gorm"
)

// MigrateExecutionLogs performs the one-way move of Transfer progress logs out
// of error_details. error_details is reserved for actual terminal errors.
func MigrateExecutionLogs(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("execution log migration database is not configured")
	}
	if err := db.Exec(`
		UPDATE common.task_executions
		SET metadata = jsonb_set(
			COALESCE(metadata, '{}'::jsonb),
			'{execution_logs}',
			error_details -> 'logs',
			true
		),
		error_details = NULLIF(error_details - 'logs', '{}'::jsonb),
		updated_at = NOW()
		WHERE module = 'transfer'
		  AND error_details ? 'logs'
	`).Error; err != nil {
		return fmt.Errorf("migrate transfer execution logs: %w", err)
	}
	return nil
}
