package models

import (
	"time"
)

// MetadataDatasource 数据源元数据
type MetadataDatasource struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ResourceID     uint      `gorm:"not null;index" json:"resource_id"`           // 关联 system.resources
	TenantID       uint      `gorm:"not null;index" json:"tenant_id"`             // 租户隔离
	DatasourceName string     `gorm:"size:255" json:"datasource_name"`              // 数据源名称
	DatasourceType string     `gorm:"size:50" json:"datasource_type"`               // mysql, postgresql, mongodb, etc.
	SyncStatus     string     `gorm:"size:50" json:"sync_status"` // pending, syncing, success, failed
	LastSyncAt     *time.Time `json:"last_sync_at"`                                 // 最后同步时间
	SyncLevel      string     `gorm:"size:20" json:"sync_level"` // database, table, field
	ErrorMessage   string    `gorm:"type:text" json:"error_message,omitempty"`    // 同步错误信息
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (MetadataDatasource) TableName() string {
	return "datasources"
}
