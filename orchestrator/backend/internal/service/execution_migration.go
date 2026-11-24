package service

import (
	execution "github.com/addp/common/execution"
	"github.com/addp/common/schema"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"gorm.io/gorm"
	"time"
)

// PrepareSchema uses the selected database connection and the module-owned revision.
func PrepareSchema(db *gorm.DB) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		return schema.MigrateMySQL(db, "orchestrator", repository.StartupSchemaVersion, repository.ApplySQLMigrations)
	}
	return schema.Migrate(db, "orchestrator", repository.StartupSchemaVersion, func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&models.Orchestration{}); err != nil {
			return err
		}
		if err := ReconcileLegacyExecutions(tx); err != nil {
			return err
		}
		return repository.ApplySQLMigrations(tx)
	})
}

// ReconcileLegacyExecutions is a one-way PostgreSQL installation migration, never a runtime fallback.
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
