package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

const (
	PointCloudCOPCStatusBuilding = "building"
	PointCloudCOPCStatusReady    = "ready"
	PointCloudCOPCStatusFailed   = "failed"
	PointCloudCOPCStatusDeleted  = "deleted"
)

type PointCloudCOPCTask = TaskDefinition

// PointCloudCOPC 记录 Manager 拥有生命周期的点云 COPC 快显。
type PointCloudCOPC struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	TenantID        uint   `gorm:"not null;index:idx_point_cloud_copc_tenant_item_fingerprint,priority:1;index:idx_point_cloud_copc_tenant_item,priority:1" json:"tenant_id"`
	ItemFingerprint string `gorm:"size:64;not null;index:idx_point_cloud_copc_tenant_item_fingerprint,priority:2" json:"item_fingerprint"`
	ItemID          *uint  `gorm:"index:idx_point_cloud_copc_tenant_item,priority:2" json:"item_id,omitempty"`
	Locator         string `gorm:"type:text" json:"locator,omitempty"`

	TaskID          *uint   `gorm:"index:idx_point_cloud_copc_task" json:"task_id,omitempty"`
	LastExecutionID *string `gorm:"size:36;index:idx_point_cloud_copc_execution" json:"last_execution_id,omitempty"`

	SourceEngineID  uint   `gorm:"not null" json:"source_engine_id"`
	SourceFormat    string `gorm:"size:64;not null" json:"source_format"`
	SourceSizeBytes int64  `json:"source_size_bytes,omitempty"`

	StorageRef string `gorm:"type:text;not null" json:"storage_ref"`
	FileName   string `gorm:"size:512" json:"file_name,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	ContentURL string `gorm:"type:text" json:"content_url,omitempty"`

	Status       string               `gorm:"size:32;not null;index:idx_point_cloud_copc_status" json:"status"`
	Metadata     commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	ErrorMessage string               `gorm:"type:text" json:"error_message,omitempty"`
	CreatedBy    *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_point_cloud_copc_deleted_at" json:"-"`
}

func (PointCloudCOPC) TableName() string {
	return "manager.point_cloud_copc"
}
