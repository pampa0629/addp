package models

import (
	"time"

	"gorm.io/gorm"
)

// MetaResource 记录在元数据系统中纳管的存储资源
type MetaResource struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	TenantID     uint           `gorm:"not null;index:idx_meta_resource_unique,priority:1" json:"tenant_id"`
	ResourceID   uint           `gorm:"not null;index:idx_meta_resource_unique,priority:2" json:"resource_id"`
	ResourceType string         `gorm:"size:64;not null" json:"resource_type"`
	Name         string         `gorm:"size:255;not null" json:"name"`
	Engine       string         `gorm:"size:128" json:"engine"`
	Config       JSONMap        `gorm:"type:jsonb" json:"config,omitempty"`
	Status       string         `gorm:"size:20;default:'active'" json:"status"`
	Source       string         `gorm:"size:64" json:"source,omitempty"`
	SyncVersion  int64          `gorm:"default:0" json:"sync_version"`
	LastSyncedAt *time.Time     `json:"last_synced_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MetaResource) TableName() string {
	return "meta_resource"
}
