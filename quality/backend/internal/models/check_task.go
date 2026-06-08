package models

import (
	"time"
)

// CheckTask 数据质量检查任务
type CheckTask struct {
	ID                  int64      `gorm:"primaryKey" json:"id"`
	TenantID            int64      `gorm:"not null;index" json:"tenant_id"`
	Name                string     `gorm:"size:200;not null" json:"name"`
	Description         string     `gorm:"size:500" json:"description"`
	EngineID            int64      `gorm:"not null" json:"engine_id"` // 目标引擎
	SchemaName          string     `gorm:"size:200" json:"schema_name"`
	Table               string     `gorm:"size:200;column:table_name" json:"table_name"` // 可选：限定表
	Enabled             bool       `gorm:"default:true" json:"enabled"`
	CreatedBy           int64      `gorm:"not null" json:"created_by"`
	UpdatedBy           *int64     `json:"updated_by,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastExecutionID     string     `gorm:"size:64" json:"last_execution_id,omitempty"`
	LastExecutionStatus string     `gorm:"size:20" json:"last_execution_status,omitempty"`
}

func (CheckTask) TableName() string { return "quality.check_tasks" }
