package models

import "time"

// DatasourceWithResource 数据源响应（包含关联的资源信息）
type DatasourceWithResource struct {
	ID           uint       `json:"id"`
	ResourceID   uint       `json:"resource_id"`
	TenantID     uint       `json:"tenant_id"`
	SyncStatus   string     `json:"sync_status"`
	LastSyncAt   *time.Time `json:"last_sync_at"`
	SyncLevel    string     `json:"sync_level"`
	ErrorMessage string     `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	// 从 system.resources 关联查询的字段
	DatasourceName string `json:"datasource_name"` // resource.name
	DatasourceType string `json:"datasource_type"` // resource.resource_type
}
