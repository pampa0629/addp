package models

import (
	"time"
)

// MetadataTable 表级元数据
type MetadataTable struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	SchemaID     uint       `gorm:"not null;index:idx_schema_tenant" json:"schema_id"`
	TenantID     uint       `gorm:"not null;index:idx_schema_tenant" json:"tenant_id"`
	Name         string     `gorm:"size:255;not null;column:table_name" json:"table_name"`

	// 表属性
	TableType    string     `gorm:"size:50" json:"table_type"`           // BASE TABLE/VIEW/MATERIALIZED VIEW
	TableComment string     `gorm:"type:text" json:"table_comment"`      // 表注释

	// 统计信息
	RowCount     int64      `json:"row_count"`                           // 行数（估算值）
	SizeBytes    int64      `json:"size_bytes"`                          // 表大小（字节）

	// 扫描信息
	LastScanAt   *time.Time `json:"last_scan_at,omitempty"`

	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (MetadataTable) TableName() string {
	return "tables"
}
