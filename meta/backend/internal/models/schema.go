package models

import (
	"time"
)

// MetadataSchema Schema级元数据（核心扫描单元）
// 对应数据库中的 Schema (PostgreSQL) 或 Database (MySQL)
type MetadataSchema struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	ResourceID   uint       `gorm:"not null;index:idx_resource_tenant" json:"resource_id"` // 关联 system.resources
	TenantID     uint       `gorm:"not null;index:idx_resource_tenant" json:"tenant_id"`   // 租户隔离
	SchemaName   string     `gorm:"size:255;not null" json:"schema_name"`                  // Schema名称

	// 扫描状态
	ScanStatus   string     `gorm:"size:20;default:'未扫描'" json:"scan_status"` // 未扫描/扫描中/已扫描
	LastScanAt   *time.Time `json:"last_scan_at,omitempty"`                     // 最后扫描时间

	// 统计信息
	TableCount   int        `gorm:"default:0" json:"table_count"`      // 表数量
	TotalSize    int64      `gorm:"column:total_size;default:0" json:"total_size_bytes"`    // 总大小(字节)

	// 定时扫描配置
	AutoScanEnabled bool   `gorm:"default:false" json:"auto_scan_enabled"` // 是否启用自动扫描
	AutoScanCron    string `gorm:"size:100" json:"auto_scan_cron"`         // Cron表达式
	NextScanAt      *time.Time `json:"next_scan_at,omitempty"`             // 下次扫描时间

	// 扫描配置
	ScanDepth    string `gorm:"size:20;default:'deep'" json:"scan_depth"` // basic/deep/full

	// 错误信息
	ErrorMessage string `gorm:"type:text" json:"error_message,omitempty"`

	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (MetadataSchema) TableName() string {
	return "schemas"
}
