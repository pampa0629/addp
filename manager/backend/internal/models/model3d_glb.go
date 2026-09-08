package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

const (
	Model3DGLBStatusBuilding = "building"
	Model3DGLBStatusReady    = "ready"
	Model3DGLBStatusFailed   = "failed"
	Model3DGLBStatusDeleted  = "deleted"
)

type Model3DGLBTask = TaskDefinition

// Model3DGLB 记录 Manager 拥有生命周期的单体三维模型 GLB 快显。
type Model3DGLB struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	TenantID        uint   `gorm:"not null;index:idx_model_3d_glb_tenant_item_fingerprint,priority:1;index:idx_model_3d_glb_tenant_item,priority:1" json:"tenant_id"`
	ItemFingerprint string `gorm:"size:64;not null;index:idx_model_3d_glb_tenant_item_fingerprint,priority:2" json:"item_fingerprint"`
	ItemID          *uint  `gorm:"index:idx_model_3d_glb_tenant_item,priority:2" json:"item_id,omitempty"`
	Locator         string `gorm:"type:text" json:"locator,omitempty"`

	TaskID          *uint   `gorm:"index:idx_model_3d_glb_task" json:"task_id,omitempty"`
	LastExecutionID *string `gorm:"size:36;index:idx_model_3d_glb_execution" json:"last_execution_id,omitempty"`

	SourceEngineID  uint   `gorm:"not null" json:"source_engine_id"`
	SourceFormat    string `gorm:"size:64;not null" json:"source_format"`
	SourceSizeBytes int64  `json:"source_size_bytes,omitempty"`

	StorageRef string `gorm:"type:text;not null" json:"storage_ref"`
	FileName   string `gorm:"size:512" json:"file_name,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	ContentURL string `gorm:"type:text" json:"content_url,omitempty"`

	Status       string               `gorm:"size:32;not null;index:idx_model_3d_glb_status" json:"status"`
	Metadata     commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	ErrorMessage string               `gorm:"type:text" json:"error_message,omitempty"`
	CreatedBy    *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_model_3d_glb_deleted_at" json:"-"`
}

func (Model3DGLB) TableName() string {
	return "manager.model_3d_glb"
}
