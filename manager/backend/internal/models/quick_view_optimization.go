package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	QuickViewOptimizationStatusBuilding = "building"
	QuickViewOptimizationStatusReady    = "ready"
	QuickViewOptimizationStatusStale    = "stale"
	QuickViewOptimizationStatusFailed   = "failed"
	QuickViewOptimizationStatusDeleted  = "deleted"

	QuickViewOptimizationTargetKindSourceSchemaMaterializedView = "source_schema_materialized_view"
	QuickViewOptimizationTargetGeometryColumn                   = "geom_3857"
)

// QuickViewOptimizationTask 快显性能优化任务定义。
// 任务配置只保存稳定目标和策略；每次执行统计写入 common.task_executions。
type QuickViewOptimizationTask struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	TenantID uint `gorm:"not null;index:idx_qvo_tasks_tenant" json:"tenant_id"`

	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Enabled     bool   `gorm:"not null" json:"enabled"`

	Schedule            string     `gorm:"size:255;index:idx_qvo_tasks_schedule,priority:2" json:"schedule,omitempty"`
	NextRunAt           *time.Time `gorm:"index:idx_qvo_tasks_schedule,priority:1" json:"next_run_at,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	LastExecutionID     *string    `gorm:"size:36;index:idx_qvo_tasks_last_execution" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string    `gorm:"size:50" json:"last_execution_status,omitempty"`

	Config    commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	CreatedBy *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_qvo_tasks_deleted_at" json:"-"`
}

func (QuickViewOptimizationTask) TableName() string {
	return "manager.quick_view_optimization_tasks"
}

// QuickViewOptimization 快显性能优化结果状态。
// 只登记 Manager 创建并拥有生命周期的优化目标。
type QuickViewOptimization struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	TenantID        uint   `gorm:"not null;index:idx_qvo_tenant_item_fingerprint,priority:1;index:idx_qvo_tenant_item,priority:1" json:"tenant_id"`
	ItemFingerprint string `gorm:"size:64;not null;index:idx_qvo_tenant_item_fingerprint,priority:2" json:"item_fingerprint"`
	ItemID          *uint  `gorm:"index:idx_qvo_tenant_item,priority:2" json:"item_id,omitempty"`
	Locator         string `gorm:"type:text" json:"locator,omitempty"`

	TaskID          *uint   `gorm:"index:idx_qvo_task" json:"task_id,omitempty"`
	LastExecutionID *string `gorm:"size:36;index:idx_qvo_execution" json:"last_execution_id,omitempty"`

	SourceEngineID       uint   `gorm:"not null" json:"source_engine_id"`
	SourceSchema         string `gorm:"size:255;not null" json:"source_schema"`
	SourceTable          string `gorm:"size:255;not null" json:"source_table"`
	SourceGeometryColumn string `gorm:"size:255;not null" json:"source_geometry_column"`
	SourceSRID           int    `gorm:"column:source_srid;not null" json:"source_srid"`
	TargetSRID           int    `gorm:"column:target_srid;not null" json:"target_srid"`
	TargetKind           string `gorm:"size:64;not null" json:"target_kind"`
	TargetSchema         string `gorm:"size:255;not null" json:"target_schema"`
	TargetTable          string `gorm:"size:255;not null" json:"target_table"`
	TargetGeometryColumn string `gorm:"size:255;not null" json:"target_geometry_column"`

	Status           string         `gorm:"size:32;not null;index:idx_qvo_status" json:"status"`
	RenderExtent     datatypes.JSON `gorm:"type:jsonb" json:"render_extent,omitempty"`
	RenderExtentSRID *int           `gorm:"column:render_extent_srid" json:"render_extent_srid,omitempty"`
	RowCountEstimate *int64         `json:"row_count_estimate,omitempty"`

	SourceFingerprintSnapshot commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"source_fingerprint_snapshot"`
	Metadata                  commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	ErrorMessage              string               `gorm:"type:text" json:"error_message,omitempty"`
	CreatedBy                 *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_qvo_deleted_at" json:"-"`
}

func (QuickViewOptimization) TableName() string {
	return "manager.quick_view_optimization"
}
