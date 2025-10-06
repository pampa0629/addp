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

// SchemaInfo Schema信息
type SchemaInfo struct {
	Name   string   `json:"name"`
	Tables []string `json:"tables"`
}

// ScanSchemaRequest 扫描Schema请求
type ScanSchemaRequest struct {
	Name          string   `json:"name"`
	ScanMode      string   `json:"scan_mode"`       // all, select
	SelectedTables []string `json:"tables,omitempty"` // 当scan_mode=select时使用
}

// ScanRequest 元数据扫描请求
type ScanRequest struct {
	ResourceID  uint                 `json:"resource_id" binding:"required"`
	Depth       string               `json:"depth"`       // basic, deep, full
	Trigger     string               `json:"trigger"`     // manual, scheduled, both
	Cron        string               `json:"cron"`        // Cron表达式
	Incremental bool                 `json:"incremental"` // 是否增量更新
	Schemas     []ScanSchemaRequest  `json:"schemas" binding:"required"`
}

// ScanResult 扫描结果
type ScanResult struct {
	Status          string    `json:"status"`
	Message         string    `json:"message"`
	SchemasScanned  int       `json:"schemas_scanned"`
	TablesScanned   int       `json:"tables_scanned"`
	FieldsScanned   int       `json:"fields_scanned"`
	DurationSeconds int       `json:"duration_seconds"`
	StartedAt       string    `json:"started_at"`
}
