package repository

import (
	"time"

	"github.com/addp/common/execution"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func pendingUploads(db *gorm.DB) *gorm.DB {
	return db.Model(&execution.TaskExecution{}).
		Where("module = ? AND task_type = ?", execution.ModuleManager, execution.TaskTypeUpload).
		Where(previewJSONText(db, "metadata", "upload_initial.status")+" = ?", "pending")
}

func PendingUploadContinuations(db *gorm.DB, limit int) ([]execution.TaskExecution, error) {
	var runs []execution.TaskExecution
	err := pendingUploads(db).Where("status IN ?", []string{execution.ExecutionStatusSuccess, execution.ExecutionStatusFailed}).
		Order("updated_at,id").Limit(limit).Find(&runs).Error
	return runs, err
}

func RotateUploadContinuation(db *gorm.DB, run *execution.TaskExecution, now time.Time) error {
	return pendingUploads(db).Where("tenant_id = ? AND execution_id = ?", run.TenantID, run.ExecutionID).
		UpdateColumn("updated_at", now).Error
}

func SaveUploadContinuation(db *gorm.DB, run *execution.TaskExecution, value map[string]any) error {
	path := "upload_initial"
	if db.Dialector.Name() == "postgres" {
		path = "{upload_initial}"
	}
	// Only advance the pending state. Keep the original receipt and any other concurrent metadata.
	return pendingUploads(db).Where("tenant_id = ? AND execution_id = ?", run.TenantID, run.ExecutionID).
		Update("metadata", datatypes.JSONSet("metadata").Set(path, value)).Error
}
