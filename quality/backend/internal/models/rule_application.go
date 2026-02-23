package models

import (
	"encoding/json"
	"time"
)

// RuleApplication 质量规则应用——将数据元质量规则映射到具体的表字段
type RuleApplication struct {
	ID         int64           `gorm:"primaryKey" json:"id"`
	TenantID   int64           `gorm:"not null;index:idx_ra_tenant_engine" json:"tenant_id"`
	ElementID  int64           `gorm:"not null" json:"element_id"`               // standard.elements.id
	EngineID   int64           `gorm:"not null;index:idx_ra_tenant_engine" json:"engine_id"`
	SchemaName string          `gorm:"size:200" json:"schema_name"`
	Table      string          `gorm:"size:200;not null;column:table_name" json:"table_name"`
	ColumnName string          `gorm:"size:200;not null" json:"column_name"`
	RuleConfig json.RawMessage `gorm:"type:jsonb;not null" json:"rule_config"` // 质量规则快照
	Enabled    bool            `gorm:"default:true" json:"enabled"`
	CreatedBy  int64           `gorm:"not null" json:"created_by"`
	UpdatedBy  *int64          `json:"updated_by,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func (RuleApplication) TableName() string { return "quality.rule_applications" }
