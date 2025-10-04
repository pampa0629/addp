package models

import (
	"time"
)

// MetadataDatabase 数据库级元数据 (Level 1 - 轻量级)
type MetadataDatabase struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	DatasourceID   uint       `gorm:"not null;index" json:"datasource_id"`
	TenantID       uint       `gorm:"not null;index" json:"tenant_id"`
	DatabaseName   string     `gorm:"size:255;not null" json:"database_name"`
	Charset        string     `gorm:"size:50" json:"charset,omitempty"`
	Collation      string     `gorm:"size:50" json:"collation,omitempty"`
	TableCount     int        `json:"table_count"`
	TotalSizeBytes int64      `json:"total_size_bytes"`
	IsScanned      bool       `json:"is_scanned"` // 是否已深度扫描
	LastScanAt     *time.Time `json:"last_scan_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (MetadataDatabase) TableName() string {
	return "databases"
}
