package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

const (
	Model3DQuickViewStatusBuilding = "building"
	Model3DQuickViewStatusReady    = "ready"
	Model3DQuickViewStatusFailed   = "failed"
	Model3DQuickViewStatusDeleted  = "deleted"
)

// Model3DQuickViewTask 单 OSGB 快显 GLB 生成任务定义。
// 任务结果是 Manager infra MinIO 中的 GLB artifact。
type Model3DQuickViewTask struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	TenantID uint `gorm:"not null;index:idx_model_3d_quick_view_tasks_tenant" json:"tenant_id"`

	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Enabled     bool   `gorm:"not null" json:"enabled"`

	Schedule            string     `gorm:"size:255" json:"schedule,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	LastExecutionID     *string    `gorm:"size:36;index:idx_model_3d_quick_view_tasks_last_execution" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string    `gorm:"size:50" json:"last_execution_status,omitempty"`

	Config    commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	CreatedBy *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_model_3d_quick_view_tasks_deleted_at" json:"-"`
}

func (Model3DQuickViewTask) TableName() string {
	return "manager.model_3d_quick_view_tasks"
}

// Model3DQuickView 记录 Manager 拥有生命周期的单 OSGB 快显 GLB。
type Model3DQuickView struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	TenantID        uint   `gorm:"not null;index:idx_model_3d_quick_view_tenant_item_fingerprint,priority:1;index:idx_model_3d_quick_view_tenant_item,priority:1" json:"tenant_id"`
	ItemFingerprint string `gorm:"size:64;not null;index:idx_model_3d_quick_view_tenant_item_fingerprint,priority:2" json:"item_fingerprint"`
	ItemID          *uint  `gorm:"index:idx_model_3d_quick_view_tenant_item,priority:2" json:"item_id,omitempty"`
	Locator         string `gorm:"type:text" json:"locator,omitempty"`

	TaskID          *uint   `gorm:"index:idx_model_3d_quick_view_task" json:"task_id,omitempty"`
	LastExecutionID *string `gorm:"size:36;index:idx_model_3d_quick_view_execution" json:"last_execution_id,omitempty"`

	SourceEngineID  uint   `gorm:"not null" json:"source_engine_id"`
	SourceFormat    string `gorm:"size:64;not null" json:"source_format"`
	SourceSizeBytes int64  `json:"source_size_bytes,omitempty"`

	StorageRef string `gorm:"type:text;not null" json:"storage_ref"`
	FileName   string `gorm:"size:512" json:"file_name,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	ContentURL string `gorm:"type:text" json:"content_url,omitempty"`

	Status       string               `gorm:"size:32;not null;index:idx_model_3d_quick_view_status" json:"status"`
	Metadata     commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	ErrorMessage string               `gorm:"type:text" json:"error_message,omitempty"`
	CreatedBy    *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_model_3d_quick_view_deleted_at" json:"-"`
}

func (Model3DQuickView) TableName() string {
	return "manager.model_3d_quick_view"
}
