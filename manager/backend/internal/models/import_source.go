package models

import "time"

// ImportSource is an immutable private upload fact, bound to one Transfer task.
type ImportSource struct {
	UploadUUID     string    `gorm:"type:uuid;primaryKey"`
	TenantID       uint      `gorm:"not null;index;check:chk_manager_import_source_tenant,tenant_id > 0"`
	UploaderID     int64     `gorm:"not null;check:chk_manager_import_source_uploader,uploader_id > 0"`
	SourceLocator  string    `gorm:"type:text;not null;check:chk_manager_import_source_locator,source_locator <> ''"`
	TransferTaskID *uint     `gorm:"uniqueIndex;check:chk_manager_import_source_task,transfer_task_id IS NULL OR transfer_task_id > 0"`
	CreatedAt      time.Time `gorm:"not null"`
}

func (ImportSource) TableName() string { return "manager.import_sources" }
