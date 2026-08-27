package models

import (
	"encoding/json"
	"time"
)

type MaterializationGateTask struct {
	ID                          int64           `gorm:"primaryKey" json:"id"`
	TenantID                    int64           `gorm:"not null;uniqueIndex:uq_quality_materialization_gate_code" json:"tenant_id"`
	Code                        string          `gorm:"size:100;not null;uniqueIndex:uq_quality_materialization_gate_code" json:"code"`
	Name                        string          `gorm:"size:200;not null" json:"name"`
	Description                 string          `gorm:"type:text;not null;default:''" json:"description"`
	Version                     int64           `gorm:"not null;default:1" json:"version"`
	MaterializationGroupID      int64           `gorm:"not null;index" json:"materialization_group_id"`
	MaterializationGroupVersion int64           `gorm:"not null" json:"materialization_group_version"`
	TableBindings               json.RawMessage `gorm:"type:jsonb;not null" json:"table_bindings"`
	Assertions                  json.RawMessage `gorm:"type:jsonb;not null" json:"assertions"`
	CreatedBy                   int64           `gorm:"not null" json:"created_by"`
	UpdatedBy                   int64           `gorm:"not null" json:"updated_by"`
	CreatedAt                   time.Time       `gorm:"not null" json:"created_at"`
	UpdatedAt                   time.Time       `gorm:"not null" json:"updated_at"`
	LastRunAt                   *time.Time      `json:"last_run_at,omitempty"`
	LastExecutionID             string          `gorm:"size:64;not null;default:''" json:"last_execution_id,omitempty"`
	LastExecutionStatus         string          `gorm:"size:20;not null;default:''" json:"last_execution_status,omitempty"`
}

func (MaterializationGateTask) TableName() string {
	return "quality.materialization_gate_tasks"
}
