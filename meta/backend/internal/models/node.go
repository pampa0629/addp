package models

import (
	"time"

	"gorm.io/gorm"
)

// MetaNode 表示引擎下的层级节点，兼容数据库 schema 与对象存储 prefix/bucket
type MetaNode struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	TenantID       uint           `gorm:"not null;index:idx_meta_node_engine_tenant,priority:1" json:"tenant_id"`
	EngineID       uint           `gorm:"column:engine_id;not null;index:idx_meta_node_engine_tenant,priority:2" json:"engine_id"`
	ParentNodeID   *uint          `gorm:"index" json:"parent_node_id,omitempty"`
	NodeType       string         `gorm:"size:64;not null;index:idx_meta_node_type" json:"node_type"`
	Name           string         `gorm:"size:255;not null" json:"name"`
	Depth          int            `gorm:"not null" json:"depth"`
	Path           string         `gorm:"type:text" json:"path,omitempty"`
	FullName       string         `gorm:"type:text" json:"full_name,omitempty"`
	ScanStatus     string         `gorm:"size:20;default:'pending';index" json:"scan_status" comment:"扫描状态：pending/running/completed/failed"`
	ScannedAt      *time.Time     `gorm:"index" json:"scanned_at,omitempty" comment:"节点的最后扫描时间"`
	ScanError      string         `gorm:"type:text;column:scan_error" json:"scan_error,omitempty" comment:"扫描错误信息"`
	ItemCount      int            `gorm:"default:0" json:"item_count"`
	TotalSizeBytes int64          `gorm:"default:0" json:"total_size_bytes"`
	Attributes     JSONMap        `gorm:"type:jsonb" json:"attributes,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MetaNode) TableName() string {
	return "metadata.meta_node"
}
