package models

import (
	"time"

	"gorm.io/gorm"
)

// MetaNode 表示资源下的层级节点，兼容数据库 schema 与对象存储 prefix/bucket
type MetaNode struct {
	ID              uint           `gorm:"primaryKey" json:"id"`
	TenantID        uint           `gorm:"not null;index:idx_meta_node_res_tenant,priority:1" json:"tenant_id"`
	ResID           uint           `gorm:"not null;index:idx_meta_node_res_tenant,priority:2" json:"res_id"`
	ParentNodeID    *uint          `gorm:"index" json:"parent_node_id,omitempty"`
	NodeType        string         `gorm:"size:64;not null;index:idx_meta_node_type" json:"node_type"`
	Name            string         `gorm:"size:255;not null" json:"name"`
	Depth           int            `gorm:"not null" json:"depth"`
	Path            string         `gorm:"type:text" json:"path,omitempty"`
	FullName        string         `gorm:"type:text" json:"full_name,omitempty"`
	Status          string         `gorm:"size:20;default:'active'" json:"status"`
	ScanStatus      string         `gorm:"size:20;default:'未扫描'" json:"scan_status"`
	LastScanAt      *time.Time     `json:"last_scan_at,omitempty"`
	AutoScanEnabled bool           `gorm:"default:false" json:"auto_scan_enabled"`
	AutoScanCron    string         `gorm:"size:128" json:"auto_scan_cron,omitempty"`
	NextScanAt      *time.Time     `json:"next_scan_at,omitempty"`
	ItemCount       int            `gorm:"default:0" json:"item_count"`
	TotalSizeBytes  int64          `gorm:"default:0" json:"total_size_bytes"`
	ErrorMessage    string         `gorm:"type:text" json:"error_message,omitempty"`
	Attributes      JSONMap        `gorm:"type:jsonb" json:"attributes,omitempty"`
	SyncVersion     int64          `gorm:"default:0" json:"sync_version"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MetaNode) TableName() string {
	return "meta_node"
}
