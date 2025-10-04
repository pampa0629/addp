package models

import (
	"time"
)

// MetadataSyncLog 同步日志
type MetadataSyncLog struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	DatasourceID     uint       `gorm:"not null;index" json:"datasource_id"`
	TenantID         uint       `gorm:"not null;index" json:"tenant_id"`
	SyncType         string     `gorm:"size:20" json:"sync_type"`                  // auto, manual, deep
	SyncLevel        string     `gorm:"size:20" json:"sync_level"`                 // database, table, field
	TargetDatabase   string     `gorm:"size:255" json:"target_database,omitempty"` // 目标数据库（深度扫描时）
	Status           string     `gorm:"size:50" json:"status"`                     // running, success, failed
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	DurationSeconds  int        `json:"duration_seconds,omitempty"`
	DatabasesScanned int        `json:"databases_scanned"`
	TablesScanned    int        `json:"tables_scanned"`
	FieldsScanned    int        `json:"fields_scanned"`
	ErrorMessage     string     `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt        time.Time  `gorm:"index:idx_sync_logs_created" json:"created_at"`
}

func (MetadataSyncLog) TableName() string {
	return "sync_logs"
}
