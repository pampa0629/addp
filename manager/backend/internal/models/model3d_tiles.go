package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

const (
	Model3DTilesTargetFormat3DTiles = "3d_tiles"
	Model3DTilesTargetFormatS3M     = "s3m"

	Model3DTilesStatusBuilding = "building"
	Model3DTilesStatusReady    = "ready"
	Model3DTilesStatusFailed   = "failed"
	Model3DTilesStatusStale    = "stale"
	Model3DTilesStatusDeleted  = "deleted"
)

// Model3DTilesTask 分块三维模型瓦片任务定义。
// target_format 区分 3D Tiles 与 S3M，结果统一写入 Manager infra MinIO。
type Model3DTilesTask struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	TenantID uint `gorm:"not null;index:idx_model3d_tiles_tasks_tenant" json:"tenant_id"`

	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Enabled     bool   `gorm:"not null" json:"enabled"`

	Schedule            string     `gorm:"size:255" json:"schedule,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	LastExecutionID     *string    `gorm:"size:36;index:idx_model3d_tiles_tasks_last_execution" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string    `gorm:"size:50" json:"last_execution_status,omitempty"`

	Config    commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	CreatedBy *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_model3d_tiles_tasks_deleted_at" json:"-"`
}

func (Model3DTilesTask) TableName() string {
	return "manager.model3d_tiles_tasks"
}

// Model3DTiles 记录 Manager infra MinIO 中的分块三维模型瓦片结果。
// target_format 区分 Cesium 3D Tiles 与 SuperMap S3M，两种格式分别形成独立结果。
type Model3DTiles struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	TenantID        uint   `gorm:"not null;index:idx_model3d_tiles_tenant_fingerprint_format,priority:1" json:"tenant_id"`
	ItemFingerprint string `gorm:"size:64;not null;index:idx_model3d_tiles_tenant_fingerprint_format,priority:2" json:"item_fingerprint"`
	ItemID          *uint  `gorm:"index:idx_model3d_tiles_item" json:"item_id,omitempty"`
	Locator         string `gorm:"type:text" json:"locator,omitempty"`

	TaskID          *uint   `gorm:"index:idx_model3d_tiles_task" json:"task_id,omitempty"`
	LastExecutionID *string `gorm:"size:36;index:idx_model3d_tiles_execution" json:"last_execution_id,omitempty"`
	SourceEngineID  uint    `gorm:"not null" json:"source_engine_id"`
	SourceFormat    string  `gorm:"size:64;not null" json:"source_format"`
	SourceSizeBytes int64   `json:"source_size_bytes,omitempty"`
	TargetFormat    string  `gorm:"size:32;not null;index:idx_model3d_tiles_tenant_fingerprint_format,priority:3" json:"target_format"`

	StorageRef  string `gorm:"type:text;not null" json:"storage_ref"`
	ManifestRef string `gorm:"size:512;not null" json:"manifest_ref"`
	FileCount   int64  `json:"file_count,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`

	Status       string               `gorm:"size:32;not null;index:idx_model3d_tiles_status" json:"status"`
	Metadata     commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	ErrorMessage string               `gorm:"type:text" json:"error_message,omitempty"`
	CreatedBy    *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_model3d_tiles_deleted_at" json:"-"`
}

func (Model3DTiles) TableName() string { return "manager.model3d_tiles" }
