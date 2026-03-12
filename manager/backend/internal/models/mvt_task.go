package models

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MvtTask MVT 瓦片生成任务（任务定义型）
// 定义一个可反复执行的 MVT 瓦片生成任务，执行记录写入 common.task_executions
type MvtTask struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	TenantID uint `gorm:"not null;index" json:"tenant_id"`

	// 任务基本信息
	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Enabled     bool   `gorm:"default:true" json:"enabled"`

	// 最近一次执行信息（回写）
	LastExecutionID     *string    `gorm:"size:36" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string    `gorm:"size:50" json:"last_execution_status,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`

	CreatedBy *uint `json:"created_by,omitempty"`

	// MvtTask 特有字段
	EngineID   uint   `gorm:"not null;index" json:"engine_id"`
	SchemaName string `gorm:"size:255;not null" json:"schema_name"`
	Table      string `gorm:"column:table_name;size:255;not null" json:"table_name"`
	MinZoom    int    `gorm:"default:0" json:"min_zoom"`
	MaxZoom    int    `gorm:"default:18" json:"max_zoom"`

	// 优化配置（JSONB）
	OptimizationConfig datatypes.JSON `gorm:"type:jsonb" json:"optimization_config,omitempty"`

	// 时间戳
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (MvtTask) TableName() string {
	return "manager.mvt_tasks"
}
