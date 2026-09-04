package models

import "time"

const (
	MetricImplementationActive   = "active"
	MetricImplementationDisabled = "disabled"
)

// MetricImplementation 是事实模型内对已发布指标定义修订的可执行实现。
type MetricImplementation struct {
	ID                         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID                   int64     `gorm:"not null;index" json:"tenant_id"`
	FactTableID                int64     `gorm:"not null;index" json:"fact_table_id"`
	MetricDefinitionID         int64     `gorm:"not null;index" json:"metric_definition_id"`
	MetricDefinitionRevisionID int64     `gorm:"not null;index" json:"metric_definition_revision_id"`
	Name                       string    `gorm:"size:200;not null" json:"name"`
	Grain                      string    `gorm:"type:text;not null" json:"grain"`
	SourceConfig               JSONB     `gorm:"type:jsonb;serializer:json;not null" json:"source_config"`
	DimensionConfig            JSONB     `gorm:"type:jsonb;serializer:json;not null" json:"dimension_config"`
	FilterConfig               JSONB     `gorm:"type:jsonb;serializer:json;not null" json:"filter_config"`
	ExpressionConfig           JSONB     `gorm:"type:jsonb;serializer:json;not null" json:"expression_config"`
	Status                     string    `gorm:"size:20;not null;default:'active'" json:"status" enums:"active,disabled"`
	Note                       string    `gorm:"type:text" json:"note"`
	CreatedBy                  int64     `gorm:"not null" json:"created_by"`
	UpdatedBy                  *int64    `json:"updated_by,omitempty"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

func (MetricImplementation) TableName() string { return "model.metric_implementations" }

type CreateMetricImplementationRequest struct {
	Version                    int64                  `json:"version" binding:"required,gt=0" minimum:"1"`
	MetricDefinitionID         int64                  `json:"metric_definition_id" binding:"required,gt=0" minimum:"1"`
	MetricDefinitionRevisionID int64                  `json:"metric_definition_revision_id" binding:"required,gt=0" minimum:"1"`
	Name                       string                 `json:"name" binding:"required,max=200" maxLength:"200"`
	Grain                      string                 `json:"grain" binding:"required"`
	SourceConfig               map[string]interface{} `json:"source_config" binding:"required"`
	DimensionConfig            map[string]interface{} `json:"dimension_config" binding:"required"`
	FilterConfig               map[string]interface{} `json:"filter_config" binding:"required"`
	ExpressionConfig           map[string]interface{} `json:"expression_config" binding:"required"`
	Status                     string                 `json:"status" binding:"required,oneof=active disabled" enums:"active,disabled"`
	Note                       string                 `json:"note"`
}

type UpdateMetricImplementationRequest = CreateMetricImplementationRequest

type MetricImplementationMutationResponse struct {
	Implementation MetricImplementation `json:"implementation"`
	Version        int64                `json:"version"`
}
