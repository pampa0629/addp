package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	TileCacheStatusGenerating = "generating"
	TileCacheStatusReady      = "ready"
	TileCacheStatusFailed     = "failed"
	TileCacheStatusCancelled  = "cancelled"
	TileCacheStatusDeleted    = "deleted"
)

// TileCacheTask 瓦片缓存生成任务定义。
// 任务私有策略统一保存在 Config 中；执行记录写入 common.task_executions。
type TileCacheTask struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	TenantID uint `gorm:"not null;index:idx_tile_cache_tasks_tenant" json:"tenant_id"`

	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`

	Schedule            string     `gorm:"size:255;index:idx_tile_cache_tasks_schedule,priority:2" json:"schedule,omitempty"`
	NextRunAt           *time.Time `gorm:"index:idx_tile_cache_tasks_schedule,priority:1" json:"next_run_at,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	LastExecutionID     *string    `gorm:"size:36;index:idx_tile_cache_tasks_last_execution" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string    `gorm:"size:50" json:"last_execution_status,omitempty"`

	Config    commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	CreatedBy *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (TileCacheTask) TableName() string {
	return "manager.tile_cache_tasks"
}

// TileCache 瓦片缓存结果状态。
// 该表是原始 data item 与瓦片缓存结果之间的最小事实源。
type TileCache struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	TenantID        uint   `gorm:"not null;index:idx_tile_cache_tenant_item_fingerprint,priority:1;index:idx_tile_cache_tenant_item,priority:1" json:"tenant_id"`
	ItemFingerprint string `gorm:"size:64;not null;index:idx_tile_cache_tenant_item_fingerprint,priority:2" json:"item_fingerprint"`
	ItemID          *uint  `gorm:"index:idx_tile_cache_tenant_item,priority:2" json:"item_id,omitempty"`
	Locator         string `gorm:"type:text" json:"locator,omitempty"`

	TaskID          *uint   `gorm:"index:idx_tile_cache_task" json:"task_id,omitempty"`
	LastExecutionID *string `gorm:"size:36;index:idx_tile_cache_execution" json:"last_execution_id,omitempty"`

	TileFormat string         `gorm:"size:32;not null" json:"tile_format"`
	StorageRef string         `gorm:"type:text" json:"storage_ref,omitempty"`
	Extent     datatypes.JSON `gorm:"type:jsonb" json:"extent,omitempty"`
	ExtentSRID *int           `gorm:"column:extent_srid" json:"extent_srid,omitempty"`
	MinZoom    *int           `json:"min_zoom,omitempty"`
	MaxZoom    *int           `json:"max_zoom,omitempty"`

	Status       string `gorm:"size:32;not null;index:idx_tile_cache_status" json:"status"`
	ErrorMessage string `gorm:"type:text" json:"error_message,omitempty"`
	CreatedBy    *uint  `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (TileCache) TableName() string {
	return "manager.tile_cache"
}
