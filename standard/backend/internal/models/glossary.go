package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// StringArray PostgreSQL 文本数组
type StringArray []string

func (s StringArray) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	b, err := json.Marshal(s)
	return string(b), err
}

func (s *StringArray) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, s)
	case string:
		return json.Unmarshal([]byte(v), s)
	}
	return fmt.Errorf("unsupported type: %T", value)
}

// Int64Array PostgreSQL bigint 数组
type Int64Array []int64

func (s Int64Array) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	b, err := json.Marshal(s)
	return string(b), err
}

func (s *Int64Array) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, s)
	case string:
		return json.Unmarshal([]byte(v), s)
	}
	return fmt.Errorf("unsupported type: %T", value)
}

// Glossary 业务术语
type Glossary struct {
	ID         int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID   int64       `gorm:"not null;index" json:"tenant_id"`
	DomainID   *int64      `gorm:"index" json:"domain_id,omitempty"`
	Name       string      `gorm:"size:200;not null" json:"name"`
	Alias      StringArray `gorm:"type:jsonb;serializer:json" json:"alias"`
	Definition string      `gorm:"type:text;not null" json:"definition"`
	Example    string      `gorm:"type:text" json:"example"`
	Note       string      `gorm:"type:text" json:"note"`
	Status     string      `gorm:"size:20;default:'draft'" json:"status"` // draft/approved/deprecated
	StewardID  *int64      `json:"steward_id,omitempty"`
	RelatedIDs Int64Array  `gorm:"type:jsonb;serializer:json" json:"related_ids"`
	Tags       StringArray `gorm:"type:jsonb;serializer:json" json:"tags"`
	CreatedBy  int64       `gorm:"not null" json:"created_by"`
	UpdatedBy  *int64      `json:"updated_by,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

func (Glossary) TableName() string {
	return "standard.glossaries"
}

// GlossaryElementMapping 术语与数据元的映射
type GlossaryElementMapping struct {
	GlossaryID int64 `gorm:"primaryKey" json:"glossary_id"`
	ElementID  int64 `gorm:"primaryKey" json:"element_id"`
}

func (GlossaryElementMapping) TableName() string {
	return "standard.glossary_element_mappings"
}

// CreateGlossaryRequest 创建业务术语请求
type CreateGlossaryRequest struct {
	DomainID   *int64   `json:"domain_id,omitempty"`
	Name       string   `json:"name" binding:"required"`
	Alias      []string `json:"alias"`
	Definition string   `json:"definition" binding:"required"`
	Example    string   `json:"example"`
	Note       string   `json:"note"`
	StewardID  *int64   `json:"steward_id,omitempty"`
	Tags       []string `json:"tags"`
}

// UpdateGlossaryRequest 更新业务术语请求
type UpdateGlossaryRequest struct {
	DomainID   *int64   `json:"domain_id,omitempty"`
	Name       string   `json:"name"`
	Alias      []string `json:"alias"`
	Definition string   `json:"definition"`
	Example    string   `json:"example"`
	Note       string   `json:"note"`
	StewardID  *int64   `json:"steward_id,omitempty"`
	Tags       []string `json:"tags"`
}
