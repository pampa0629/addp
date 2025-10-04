package models

import (
	"time"
)

// MetadataTable 表级元数据 (Level 2 - 深度扫描)
type MetadataTable struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	DatabaseID      uint       `gorm:"not null;index" json:"database_id"`
	TenantID        uint       `gorm:"not null;index" json:"tenant_id"`
	Name            string     `gorm:"column:table_name;size:255;not null;index" json:"table_name"`
	TableType       string     `gorm:"size:50" json:"table_type,omitempty"`        // TABLE, VIEW, MATERIALIZED VIEW
	TableSchema     string     `gorm:"size:255" json:"table_schema,omitempty"`     // Schema 名称（PostgreSQL）
	Engine          string     `gorm:"size:50" json:"engine,omitempty"`            // 存储引擎（MySQL）
	RowCount        int64      `json:"row_count"`                 // 行数（预估）
	DataSizeBytes   int64      `json:"data_size_bytes"`           // 数据大小
	IndexSizeBytes  int64      `json:"index_size_bytes"`          // 索引大小
	TableComment    string     `gorm:"type:text" json:"table_comment,omitempty"`   // 表注释
	IsScanned       bool       `json:"is_scanned"`            // 是否已扫描字段
	LastScanAt      *time.Time `json:"last_scan_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (MetadataTable) TableName() string {
	return "tables"
}
