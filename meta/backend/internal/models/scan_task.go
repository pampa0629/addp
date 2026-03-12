package models

import (
	"time"

	"gorm.io/gorm"
)

// TriggerType 常量
const (
	TriggerTypeManual    = "manual"    // 手动触发
	TriggerTypeScheduled = "scheduled" // 定时触发
	TriggerTypeAuto      = "auto"      // 自动触发（如 Transfer 完成后）
)

// ScanTask 定义可复用的元数据扫描任务（手动或定时）
type ScanTask struct {
	ID                  uint           `gorm:"primaryKey" json:"id"`
	TenantID            uint           `gorm:"index;not null" json:"tenant_id"`
	EngineID            uint           `gorm:"index;not null" json:"engine_id"`
	Name                string         `gorm:"size:128;not null" json:"name"`
	Description         string         `gorm:"size:512" json:"description,omitempty"`
	Schedule            string         `gorm:"size:128;column:schedule" json:"schedule,omitempty"`
	Enabled             bool           `gorm:"default:false" json:"enabled"`
	Parameters          JSONMap        `gorm:"type:jsonb" json:"parameters,omitempty"` // 扫描目标配置
	LastRunAt           *time.Time     `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time     `json:"next_run_at,omitempty"`
	LastExecutionID     *string        `gorm:"size:36" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string        `gorm:"size:20" json:"last_execution_status,omitempty"`
	CreatedBy           uint           `json:"created_by,omitempty"`
	UpdatedBy           uint           `json:"updated_by,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ScanTask) TableName() string {
	return "metadata.scan_tasks"
}
