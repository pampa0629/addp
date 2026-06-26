package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	RasterCOGStatusBuilding  = "building"
	RasterCOGStatusReady     = "ready"
	RasterCOGStatusFailed    = "failed"
	RasterCOGStatusStale     = "stale"
	RasterCOGStatusDeleted   = "deleted"
	RasterCOGTargetKindMinIO = "infra_minio_object"
)

// RasterCOGTask 栅格快显 COG 生成任务定义。
// 任务配置保存源 item、栅格 facts 与 COG 策略；执行记录写入 common.task_executions。
type RasterCOGTask struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	TenantID uint `gorm:"not null;index:idx_raster_cog_tasks_tenant" json:"tenant_id"`

	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Enabled     bool   `gorm:"not null" json:"enabled"`

	Schedule            string     `gorm:"size:255" json:"schedule,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	LastExecutionID     *string    `gorm:"size:36;index:idx_raster_cog_tasks_last_execution" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string    `gorm:"size:50" json:"last_execution_status,omitempty"`

	Config    commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	CreatedBy *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_raster_cog_tasks_deleted_at" json:"-"`
}

func (RasterCOGTask) TableName() string {
	return "manager.raster_cog_tasks"
}

// RasterCOG 记录 Manager 拥有生命周期的栅格快显 COG。
// 源 TIFF/COG 可以来自 NFS、业务 MinIO 或其他存储；前端消费的 COG 统一登记为 infra MinIO 结果对象。
type RasterCOG struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	TenantID        uint   `gorm:"not null;index:idx_raster_cog_tenant_item_fingerprint,priority:1;index:idx_raster_cog_tenant_item,priority:1" json:"tenant_id"`
	ItemFingerprint string `gorm:"size:64;not null;index:idx_raster_cog_tenant_item_fingerprint,priority:2" json:"item_fingerprint"`
	ItemID          *uint  `gorm:"index:idx_raster_cog_tenant_item,priority:2" json:"item_id,omitempty"`
	Locator         string `gorm:"type:text" json:"locator,omitempty"`

	TaskID          *uint   `gorm:"index:idx_raster_cog_task" json:"task_id,omitempty"`
	LastExecutionID *string `gorm:"size:36;index:idx_raster_cog_execution" json:"last_execution_id,omitempty"`

	SourceEngineID  uint   `gorm:"not null" json:"source_engine_id"`
	SourceProfile   string `gorm:"size:32" json:"source_profile,omitempty"`
	SourceSizeBytes int64  `json:"source_size_bytes,omitempty"`

	TargetKind string `gorm:"size:64;not null" json:"target_kind"`
	StorageRef string `gorm:"type:text;not null" json:"storage_ref"`
	FileName   string `gorm:"size:512" json:"file_name,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`

	Width      int64  `json:"width,omitempty"`
	Height     int64  `json:"height,omitempty"`
	BandCount  int64  `json:"band_count,omitempty"`
	SourceSRID int    `gorm:"column:source_srid" json:"source_srid,omitempty"`
	SourceCRS  string `gorm:"type:text" json:"source_crs,omitempty"`

	Extent     datatypes.JSON `gorm:"type:jsonb" json:"extent,omitempty"`
	ExtentSRID *int           `gorm:"column:extent_srid" json:"extent_srid,omitempty"`

	Status       string               `gorm:"size:32;not null;index:idx_raster_cog_status" json:"status"`
	Metadata     commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	ErrorMessage string               `gorm:"type:text" json:"error_message,omitempty"`
	CreatedBy    *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_raster_cog_deleted_at" json:"-"`
}

func (RasterCOG) TableName() string {
	return "manager.raster_cog"
}
