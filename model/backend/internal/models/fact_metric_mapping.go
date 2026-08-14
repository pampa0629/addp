package models

import "time"

// FactMetricMapping 事实表与 Standard.Metric 的显式关联
// 回答：这张事实表能支撑哪些指标的计算？
// 回答：这个指标的计算基础在哪张表？（指标溯源/血缘）
type FactMetricMapping struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64     `gorm:"not null;index" json:"tenant_id"`
	FactTableID int64     `gorm:"not null;index" json:"fact_table_id"` // → model.logical_tables (table_type='fact')
	MetricID    int64     `gorm:"not null" json:"metric_id"`           // → standard.metrics（无 DB FK 约束，跨 schema）
	FieldID     *int64    `json:"field_id,omitempty"`                  // → model.logical_fields（可选，哪个字段是该指标的计算基础）
	Note        string    `gorm:"type:text" json:"note"`
	CreatedBy   int64     `gorm:"not null" json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

func (FactMetricMapping) TableName() string {
	return "model.fact_metric_mappings"
}

// CreateFactMetricMappingRequest 关联指标请求
type CreateFactMetricMappingRequest struct {
	Version  int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	MetricID int64  `json:"metric_id" binding:"required,gt=0" minimum:"1"`
	FieldID  *int64 `json:"field_id,omitempty" binding:"omitempty,gt=0" minimum:"1"`
	Note     string `json:"note"`
}

type FactMetricMutationResponse struct {
	Mapping FactMetricMapping `json:"mapping"`
	Version int64             `json:"version"`
}
