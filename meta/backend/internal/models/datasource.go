package models

import (
	"time"
)

// MetadataDatasource 数据源元数据
// 不存储冗余信息（name, type），通过 resource_id 关联 system.resources 查询
type MetadataDatasource struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	ResourceID   uint       `gorm:"not null;uniqueIndex:idx_resource_tenant" json:"resource_id"` // 关联 system.resources
	TenantID     uint       `gorm:"not null;uniqueIndex:idx_resource_tenant" json:"tenant_id"`   // 租户隔离
	SyncStatus   string     `gorm:"size:50" json:"sync_status"` // pending, syncing, success, failed
	LastSyncAt   *time.Time `json:"last_sync_at"`               // 最后同步时间
	SyncLevel    string     `gorm:"size:20" json:"sync_level"`  // database, table, field
	ErrorMessage string     `gorm:"type:text" json:"error_message,omitempty"` // 同步错误信息
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (MetadataDatasource) TableName() string {
	return "datasources"
}
