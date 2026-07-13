package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

const (
	CADPreviewStatusBuilding = "building"
	CADPreviewStatusReady    = "ready"
	CADPreviewStatusFailed   = "failed"
	CADPreviewStatusDeleted  = "deleted"
)

type CADPreviewTask struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	TenantID uint `gorm:"not null;index:idx_cad_preview_tasks_tenant" json:"tenant_id"`

	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Enabled     bool   `gorm:"not null" json:"enabled"`

	Schedule            string     `gorm:"size:255" json:"schedule,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	LastExecutionID     *string    `gorm:"size:36;index:idx_cad_preview_tasks_last_execution" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string    `gorm:"size:50" json:"last_execution_status,omitempty"`

	Config    commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	CreatedBy *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_cad_preview_tasks_deleted_at" json:"-"`
}

func (CADPreviewTask) TableName() string { return "manager.cad_preview_tasks" }

type CADPreview struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	TenantID        uint   `gorm:"not null;index:idx_cad_previews_tenant_fingerprint,priority:1" json:"tenant_id"`
	ItemFingerprint string `gorm:"size:64;not null;index:idx_cad_previews_tenant_fingerprint,priority:2" json:"item_fingerprint"`
	ItemID          *uint  `json:"item_id,omitempty"`
	Locator         string `gorm:"type:text" json:"locator,omitempty"`

	TaskID          *uint   `json:"task_id,omitempty"`
	LastExecutionID *string `gorm:"size:36" json:"last_execution_id,omitempty"`
	SourceEngineID  uint    `gorm:"not null" json:"source_engine_id"`
	SourceFormat    string  `gorm:"size:64;not null" json:"source_format"`
	SourceSizeBytes int64   `json:"source_size_bytes,omitempty"`

	StorageRef   string               `gorm:"type:text;not null" json:"storage_ref"`
	ManifestRef  string               `gorm:"size:512;not null" json:"manifest_ref"`
	ThumbnailRef string               `gorm:"size:512" json:"thumbnail_ref,omitempty"`
	TileCount    int64                `json:"tile_count,omitempty"`
	TileSize     int                  `json:"tile_size,omitempty"`
	MinZoom      int                  `json:"min_zoom,omitempty"`
	MaxZoom      int                  `json:"max_zoom,omitempty"`
	Bounds       commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"bounds"`

	Status       string               `gorm:"size:32;not null;index:idx_cad_previews_status" json:"status"`
	Metadata     commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	ErrorMessage string               `gorm:"type:text" json:"error_message,omitempty"`
	CreatedBy    *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_cad_previews_deleted_at" json:"-"`
}

func (CADPreview) TableName() string { return "manager.cad_previews" }
