package models

import (
	"time"

	"gorm.io/gorm"
)

// MetaItem 表示引擎下的最终数据项，例如表或对象
type MetaItem struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	TenantID      uint           `gorm:"not null;index:idx_meta_item_engine_tenant,priority:1" json:"tenant_id"`
	EngineID      uint           `gorm:"column:engine_id;not null;index:idx_meta_item_engine_tenant,priority:2" json:"engine_id"`
	NodeID        uint           `gorm:"not null;index" json:"node_id"`
	ItemType      string         `gorm:"size:64;not null;index:idx_meta_item_type" json:"item_type"`
	Name          string         `gorm:"size:255;not null" json:"name"`
	FullName      string         `gorm:"type:text" json:"full_name,omitempty"`
	Fingerprint   string         `gorm:"size:64;not null;uniqueIndex:idx_meta_item_fingerprint" json:"fingerprint" comment:"数据指纹：基于engine_id+路径的SHA256哈希，用于去重和数据血缘追踪"`
	RowCount      *int64         `json:"row_count,omitempty"`
	SizeBytes     *int64         `json:"size_bytes,omitempty"`
	DataUpdatedAt *time.Time     `json:"data_updated_at,omitempty" comment:"被扫描数据的最后更新时间"`
	ScannedAt     *time.Time     `json:"scanned_at,omitempty" comment:"数据项的扫描时间"`
	ScannedDepth  string         `gorm:"size:10;default:'none';index" json:"scanned_depth" comment:"已完成扫描深度：none/basic/deep"`
	Attributes    JSONMap        `gorm:"type:jsonb" json:"attributes,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MetaItem) TableName() string {
	return "meta.meta_item"
}
