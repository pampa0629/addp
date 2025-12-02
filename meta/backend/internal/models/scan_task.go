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
	ID             uint           `gorm:"primaryKey" json:"id"`
	TenantID       uint           `gorm:"index;not null" json:"tenant_id"`
	ResourceID     uint           `gorm:"index;not null" json:"resource_id"`
	Name           string         `gorm:"size:128;not null" json:"name"`
	Description    string         `gorm:"size:512" json:"description,omitempty"`
	ScheduleType   string         `gorm:"size:32;not null" json:"schedule_type"` // manual/daily/weekly/monthly/cron
	CronExpression string         `gorm:"size:128" json:"cron_expression,omitempty"`
	Enabled        bool           `gorm:"default:false" json:"enabled"`
	Parameters     JSONMap        `gorm:"type:jsonb" json:"parameters,omitempty"`      // 扫描目标配置
	ScheduleConfig JSONMap        `gorm:"type:jsonb" json:"schedule_config,omitempty"` // 原始调度配置（便于前端回显）
	LastRunAt      *time.Time     `json:"last_run_at,omitempty"`
	NextRunAt      *time.Time     `json:"next_run_at,omitempty"`
	CreatedBy      uint           `json:"created_by,omitempty"`
	UpdatedBy      uint           `json:"updated_by,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ScanTask) TableName() string {
	return "metadata.scan_tasks"
}

// ScanTaskRun 表示一次具体的扫描执行
type ScanTaskRun struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	TaskID          *uint      `gorm:"index" json:"task_id,omitempty"`
	TenantID        uint       `gorm:"index;not null" json:"tenant_id"`
	ResourceID      uint       `gorm:"index;not null" json:"resource_id"`
	Name            string     `gorm:"size:180" json:"name"`
	StorageType     string     `gorm:"size:64" json:"storage_type"`
	TriggerType     string     `gorm:"size:32;not null" json:"trigger_type"` // manual/scheduled/system
	Status          string     `gorm:"size:32;not null" json:"status"`       // pending/running/success/failed/canceled
	ErrorMessage    string     `gorm:"type:text" json:"error_message,omitempty"`
	Parameters      JSONMap    `gorm:"type:jsonb" json:"parameters,omitempty"`
	ResultSummary   JSONMap    `gorm:"type:jsonb" json:"result_summary,omitempty"`
	ProgressTotal   int        `gorm:"default:0" json:"progress_total"`
	ProgressCurrent int        `gorm:"default:0" json:"progress_current"`
	ProgressMessage string     `gorm:"size:255" json:"progress_message,omitempty"`
	ProgressPercent float64    `gorm:"default:0" json:"progress_percent"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	TriggerUserID   *uint      `json:"trigger_user_id,omitempty"`

	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (ScanTaskRun) TableName() string {
	return "metadata.scan_task_runs"
}
