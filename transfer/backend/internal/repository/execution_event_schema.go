package repository

import (
	"fmt"
	"gorm.io/gorm"
)

// RemoveExecutionLogs deletes the retired free-text process log storage.
// Historical text is not promoted to structured evidence.
func RemoveExecutionLogs(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("execution event migration database is not configured")
	}
	if err := db.Exec(`UPDATE common.task_executions
 SET metadata = COALESCE(metadata, '{}'::jsonb) - 'execution_logs',
 error_details = NULLIF(error_details - 'logs', '{}'::jsonb), updated_at = NOW()
 WHERE module = 'transfer' AND (metadata ? 'execution_logs' OR error_details ? 'logs')`).Error; err != nil {
		return fmt.Errorf("remove retired transfer execution logs: %w", err)
	}
	return nil
}
