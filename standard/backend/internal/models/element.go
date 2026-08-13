package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// JSONB PostgreSQL JSONB 类型
type JSONB map[string]interface{}

func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	b, err := json.Marshal(j)
	return string(b), err
}

func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, j)
	case string:
		return json.Unmarshal([]byte(v), j)
	}
	return fmt.Errorf("unsupported type: %T", value)
}

// Element 数据元
type Element struct {
	ID               int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID         int64       `gorm:"not null;index;uniqueIndex:uq_standard_elements_tenant_code" json:"tenant_id"`
	DomainID         *int64      `gorm:"index" json:"domain_id,omitempty"`
	Name             string      `gorm:"size:200;not null" json:"name"`
	Code             string      `gorm:"size:100;not null;uniqueIndex:uq_standard_elements_tenant_code" json:"code"`
	DataType         string      `gorm:"size:50;not null" json:"data_type"` // string/int/float/date/bool/json
	Length           *int        `json:"length,omitempty"`
	PrecisionNum     *int        `json:"precision_num,omitempty"`
	Scale            *int        `json:"scale,omitempty"`
	Nullable         bool        `gorm:"default:true" json:"nullable"`
	DefaultValue     string      `gorm:"type:text" json:"default_value"`
	Format           string      `gorm:"size:200" json:"format"`
	ValueRange       JSONB       `gorm:"type:jsonb;serializer:json" json:"value_range"`
	UnitID           *int64      `gorm:"index" json:"unit_id,omitempty"`           // 关联计量单位
	SecurityLevel    string      `gorm:"size:10" json:"security_level"`            // L1/L2/L3/L4
	ClassificationID *int64      `gorm:"index" json:"classification_id,omitempty"` // 关联数据分类
	CodeSetID        *int64      `gorm:"index" json:"code_set_id,omitempty"`       // 关联码值集
	Definition       string      `gorm:"type:text" json:"definition"`
	ExampleValues    StringArray `gorm:"type:jsonb;serializer:json" json:"example_values"`
	QualityRules     JSONB       `gorm:"type:jsonb;serializer:json" json:"quality_rules"` // 质量规则
	Status           string      `gorm:"size:20;default:'draft'" json:"status"`           // draft/approved/deprecated
	StewardID        *int64      `json:"steward_id,omitempty"`
	Tags             StringArray `gorm:"type:jsonb;serializer:json" json:"tags"`
	CreatedBy        int64       `gorm:"not null" json:"created_by"`
	UpdatedBy        *int64      `json:"updated_by,omitempty"`
	CreatedAt        time.Time   `json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
	Version          int64       `gorm:"not null;default:1" json:"version"`
}

func (Element) TableName() string {
	return "standard.elements"
}

// CreateElementRequest 创建数据元请求
type CreateElementRequest struct {
	DomainID         *int64                 `json:"domain_id,omitempty"`
	Name             string                 `json:"name" binding:"required"`
	Code             string                 `json:"code" binding:"required"`
	DataType         string                 `json:"data_type" binding:"required"`
	Length           *int                   `json:"length,omitempty"`
	PrecisionNum     *int                   `json:"precision_num,omitempty"`
	Scale            *int                   `json:"scale,omitempty"`
	Nullable         bool                   `json:"nullable"`
	DefaultValue     string                 `json:"default_value"`
	Format           string                 `json:"format"`
	ValueRange       map[string]interface{} `json:"value_range"`
	UnitID           *int64                 `json:"unit_id,omitempty"`
	SecurityLevel    string                 `json:"security_level"`
	ClassificationID *int64                 `json:"classification_id,omitempty"`
	CodeSetID        *int64                 `json:"code_set_id,omitempty"`
	Definition       string                 `json:"definition"`
	ExampleValues    []string               `json:"example_values"`
	QualityRules     map[string]interface{} `json:"quality_rules"`
	StewardID        *int64                 `json:"steward_id,omitempty"`
	Tags             []string               `json:"tags"`
}

// UpdateElementRequest 更新数据元请求
type UpdateElementRequest struct {
	Version          int64                  `json:"version" binding:"required"`
	DomainID         *int64                 `json:"domain_id,omitempty"`
	Name             string                 `json:"name"`
	DataType         string                 `json:"data_type"`
	Length           *int                   `json:"length,omitempty"`
	PrecisionNum     *int                   `json:"precision_num,omitempty"`
	Scale            *int                   `json:"scale,omitempty"`
	Nullable         *bool                  `json:"nullable,omitempty"`
	DefaultValue     string                 `json:"default_value"`
	Format           string                 `json:"format"`
	ValueRange       map[string]interface{} `json:"value_range"`
	UnitID           *int64                 `json:"unit_id,omitempty"`
	SecurityLevel    string                 `json:"security_level"`
	ClassificationID *int64                 `json:"classification_id,omitempty"`
	CodeSetID        *int64                 `json:"code_set_id,omitempty"`
	Definition       string                 `json:"definition"`
	ExampleValues    []string               `json:"example_values"`
	QualityRules     map[string]interface{} `json:"quality_rules"`
	StewardID        *int64                 `json:"steward_id,omitempty"`
	Tags             []string               `json:"tags"`
}
