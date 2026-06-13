package models

import "time"

// QuickView 记录空间预览用户显示偏好。
// 快显能力由查询时根据空间元数据和 TileCache 动态合成。
type QuickView struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	TenantID        uint   `gorm:"not null;uniqueIndex:idx_quick_view_tenant_fingerprint,priority:1" json:"tenant_id"`
	ItemFingerprint string `gorm:"size:64;not null;uniqueIndex:idx_quick_view_tenant_fingerprint,priority:2" json:"item_fingerprint"`
	Locator         string `gorm:"type:text;not null" json:"locator"`

	PreferredMode string `gorm:"default:'table_geojson';size:32" json:"preferred_mode"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (QuickView) TableName() string {
	return "manager.quick_view"
}
