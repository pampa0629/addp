package models

import (
	"encoding/json"
	"time"
)

type QualityPlan struct {
	ID                  int64           `gorm:"primaryKey" json:"id"`
	TenantID            int64           `gorm:"not null;uniqueIndex:uq_quality_plan_code" json:"tenant_id"`
	Code                string          `gorm:"size:100;not null;uniqueIndex:uq_quality_plan_code" json:"code"`
	OwnerDomainID       *int64          `gorm:"index" json:"owner_domain_id,omitempty"`
	Name                string          `gorm:"size:200;not null" json:"name"`
	Description         string          `gorm:"type:text;not null;default:''" json:"description"`
	Version             int64           `gorm:"not null;default:1" json:"version"`
	TableBindings       json.RawMessage `gorm:"type:jsonb;not null" json:"table_bindings"`
	CheckItems          []PlanCheckItem `gorm:"-" json:"check_items"`
	Rules               json.RawMessage `gorm:"-" json:"-"`
	CreatedBy           int64           `gorm:"not null" json:"created_by"`
	UpdatedBy           int64           `gorm:"not null" json:"updated_by"`
	CreatedAt           time.Time       `gorm:"not null" json:"created_at"`
	UpdatedAt           time.Time       `gorm:"not null" json:"updated_at"`
	LastRunAt           *time.Time      `json:"last_run_at,omitempty"`
	LastExecutionID     string          `gorm:"size:64;not null;default:''" json:"last_execution_id,omitempty"`
	LastExecutionStatus string          `gorm:"size:20;not null;default:''" json:"last_execution_status,omitempty"`
}

func (QualityPlan) TableName() string {
	return "quality.plans"
}
