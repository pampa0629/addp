package service

import (
	execution "github.com/addp/common/execution"
	"gorm.io/gorm"
	"time"
)

// ReconcileLegacyExecutions is a one-way module startup migration, never a runtime fallback.
func ReconcileLegacyExecutions(db *gorm.DB) error {
	base := db.Model(&execution.TaskExecution{}).Where("module = ? AND task_type = ?", execution.ModuleOrchestrator, execution.TaskTypeOrchestration)
	if err := base.Session(&gorm.Session{}).Where("status = ? AND COALESCE(execution_config->>'schema_version','') <> ?", execution.ExecutionStatusPending, executionPlanVersion).Updates(map[string]interface{}{
		"status": execution.ExecutionStatusFailed, "completed_at": time.Now().UTC(), "error_details": executionFailureDetails("orchestrator.execution.plan_missing"),
	}).Error; err != nil {
		return err
	}
	return base.Session(&gorm.Session{}).Where("status = ? AND (lease_token IS NULL OR lease_owner IS NULL OR lease_owner = '' OR lease_expires_at IS NULL OR attempt < 1)", execution.ExecutionStatusRunning).Updates(map[string]interface{}{
		"status": execution.ExecutionStatusFailed, "completed_at": time.Now().UTC(), "current_step": nil, "lease_token": nil, "lease_owner": nil, "lease_expires_at": nil, "error_details": executionFailureDetails("orchestrator.execution.lease_missing"),
	}).Error
}
