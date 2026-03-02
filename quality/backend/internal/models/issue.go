package models

import (
	"encoding/json"
	"time"
)

// Issue 质量问题工单
type Issue struct {
	ID                int64           `gorm:"primaryKey" json:"id"`
	TenantID          int64           `gorm:"not null;index" json:"tenant_id"`
	ExecutionID       string          `gorm:"size:255;not null;index" json:"execution_id"` // common.task_executions.execution_id
	RuleApplicationID int64           `gorm:"not null" json:"rule_application_id"`
	RuleType          string          `gorm:"size:100;not null" json:"rule_type"`
	ColumnName        string          `gorm:"size:200;not null" json:"column_name"`
	Table             string          `gorm:"size:200;not null;column:table_name" json:"table_name"`
	SchemaName        string          `gorm:"size:200" json:"schema_name"`
	EngineID          int64           `gorm:"not null" json:"engine_id"`
	FailedCount       int64           `gorm:"not null" json:"failed_count"`
	TotalCount        int64           `gorm:"not null" json:"total_count"`
	PassRate          float64         `gorm:"not null" json:"pass_rate"`
	Detail            json.RawMessage `gorm:"type:jsonb" json:"detail,omitempty"`
	Status            string          `gorm:"size:50;not null;default:'open'" json:"status"` // open/resolved/ignored
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

func (Issue) TableName() string { return "quality.issues" }
