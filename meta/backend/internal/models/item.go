package models

import (
	"time"

	"gorm.io/gorm"
)

// MetaItem 表示资源下的最终数据项，例如表或对象
type MetaItem struct {
	ID                uint           `gorm:"primaryKey" json:"id"`
	TenantID          uint           `gorm:"not null;index:idx_meta_item_res_tenant,priority:1" json:"tenant_id"`
	ResID             uint           `gorm:"not null;index:idx_meta_item_res_tenant,priority:2" json:"res_id"`
	NodeID            uint           `gorm:"not null;index" json:"node_id"`
	ItemType          string         `gorm:"size:64;not null;index:idx_meta_item_type" json:"item_type"`
	Name              string         `gorm:"size:255;not null" json:"name"`
	FullName          string         `gorm:"type:text" json:"full_name,omitempty"`
	Status            string         `gorm:"size:20;default:'active'" json:"status"`
	MetaSchemaVersion int            `gorm:"default:1" json:"meta_schema_version"`
	RowCount          *int64         `json:"row_count,omitempty"`
	SizeBytes         *int64         `json:"size_bytes,omitempty"`
	ObjectSizeBytes   *int64         `json:"object_size_bytes,omitempty"`
	LastModifiedAt    *time.Time     `json:"last_modified_at,omitempty"`
	Attributes        JSONMap        `gorm:"type:jsonb" json:"attributes,omitempty"`
	SyncVersion       int64          `gorm:"default:0" json:"sync_version"`
	Source            string         `gorm:"size:64" json:"source,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MetaItem) TableName() string {
	return "meta_item"
}
