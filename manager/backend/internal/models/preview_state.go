package models

import (
	"time"

	commonModels "github.com/addp/common/models"
)

const (
	PreviewModeBasicPreview = "basic_preview"
	PreviewModeMapQuickView = "map_quick_view"
)

// PreviewState 记录数据项预览模式偏好和交互视角状态。
// 快显能力由查询时根据数据项元信息和快显结果动态合成。
type PreviewState struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	TenantID        uint   `gorm:"not null;uniqueIndex:idx_preview_state_tenant_fingerprint,priority:1" json:"tenant_id"`
	ItemFingerprint string `gorm:"size:64;not null;uniqueIndex:idx_preview_state_tenant_fingerprint,priority:2" json:"item_fingerprint"`
	Locator         string `gorm:"type:text;not null" json:"locator"`

	PreferredMode string               `gorm:"default:'basic_preview';size:32" json:"preferred_mode"`
	ViewState     commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"view_state,omitempty"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (PreviewState) TableName() string {
	return "manager.preview_state"
}
