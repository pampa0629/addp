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

// Glossary 是业务术语的稳定身份。业务定义只存在于 GlossaryRevision。
type Glossary struct {
	ID              int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID        int64       `gorm:"not null;index;uniqueIndex:uq_standard_glossaries_tenant_code" json:"tenant_id"`
	ScopeType       string      `gorm:"size:20;not null;default:'tenant_common';index" json:"scope_type" enums:"platform,tenant_common,domain"`
	OwnerDomainID   *int64      `gorm:"index" json:"owner_domain_id,omitempty"`
	Code            string      `gorm:"size:100;not null;uniqueIndex:uq_standard_glossaries_tenant_code" json:"code"`
	StewardID       *int64      `json:"steward_id,omitempty"`
	Tags            StringArray `gorm:"type:jsonb;serializer:json" json:"tags"`
	DraftRevisionID *int64      `gorm:"index" json:"draft_revision_id,omitempty"`
	CreatedBy       int64       `gorm:"not null" json:"created_by"`
	UpdatedBy       *int64      `json:"updated_by,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
	Version         int64       `gorm:"not null;default:1" json:"version"`
	LifecycleState  string      `gorm:"size:16;not null;default:'active'" json:"lifecycle_state"`
}

func (Glossary) TableName() string {
	return "standard.glossaries"
}

// GlossaryRevision 是业务术语的一次完整业务定义快照。
type GlossaryRevision struct {
	ID            int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	GlossaryID    int64       `gorm:"not null;index;uniqueIndex:uq_standard_glossary_revisions_glossary_no" json:"glossary_id"`
	RevisionNo    int64       `gorm:"not null;uniqueIndex:uq_standard_glossary_revisions_glossary_no" json:"revision_no"`
	Status        string      `gorm:"size:20;not null" json:"status" enums:"draft,in_review,published,withdrawn"`
	Name          string      `gorm:"size:200;not null" json:"name"`
	Alias         StringArray `gorm:"type:jsonb;serializer:json" json:"alias"`
	Definition    string      `gorm:"type:text;not null" json:"definition"`
	Example       string      `gorm:"type:text" json:"example"`
	Note          string      `gorm:"type:text" json:"note"`
	RelatedIDs    Int64Array  `gorm:"type:jsonb;serializer:json" json:"related_ids"`
	ChangeSummary string      `gorm:"type:text;not null" json:"change_summary"`
	EffectiveFrom *time.Time  `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time  `json:"effective_to,omitempty"`
	SubmittedBy   *int64      `json:"submitted_by,omitempty"`
	SubmittedAt   *time.Time  `json:"submitted_at,omitempty"`
	PublishedBy   *int64      `json:"published_by,omitempty"`
	PublishedAt   *time.Time  `json:"published_at,omitempty"`
	CreatedBy     int64       `gorm:"not null" json:"created_by"`
	UpdatedBy     *int64      `json:"updated_by,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

func (GlossaryRevision) TableName() string { return "standard.glossary_revisions" }

type GlossaryAggregate struct {
	Glossary
	CurrentRevision       *GlossaryRevision `json:"current_revision,omitempty"`
	DraftRevision         *GlossaryRevision `json:"draft_revision,omitempty"`
	HasPublicationHistory bool              `json:"has_publication_history"`
}

// PublishedGlossaryReference 是跨模块解析当前生效业务术语时使用的只读投影。
type PublishedGlossaryReference struct {
	ID             int64  `json:"id"`
	TenantID       int64  `json:"tenant_id"`
	ScopeType      string `json:"scope_type" enums:"platform,tenant_common,domain"`
	OwnerDomainID  *int64 `json:"owner_domain_id,omitempty"`
	Name           string `json:"name"`
	Code           string `json:"code"`
	Status         string `json:"status"`
	LifecycleState string `json:"lifecycle_state"`
	Version        int64  `json:"version"`
	RevisionID     int64  `json:"revision_id"`
	RevisionNo     int64  `json:"revision_no"`
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
	ScopeType     string     `json:"scope_type" binding:"required" enums:"tenant_common,domain"`
	OwnerDomainID *int64     `json:"owner_domain_id,omitempty"`
	Code          string     `json:"code" binding:"required"`
	StewardID     *int64     `json:"steward_id,omitempty"`
	Tags          []string   `json:"tags"`
	Name          string     `json:"name" binding:"required"`
	Alias         []string   `json:"alias"`
	Definition    string     `json:"definition" binding:"required"`
	Example       string     `json:"example"`
	Note          string     `json:"note"`
	RelatedIDs    []int64    `json:"related_ids"`
	ChangeSummary string     `json:"change_summary" binding:"required"`
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
}

// UpdateGlossaryRequest 更新业务术语请求
type UpdateGlossaryRequest struct {
	Version       int64    `json:"version" binding:"required,gt=0" minimum:"1"`
	ScopeType     string   `json:"scope_type" binding:"required" enums:"tenant_common,domain"`
	OwnerDomainID *int64   `json:"owner_domain_id,omitempty"`
	StewardID     *int64   `json:"steward_id,omitempty"`
	Tags          []string `json:"tags"`
}

type CreateGlossaryRevisionRequest struct {
	Version       int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	ChangeSummary string `json:"change_summary" binding:"required"`
}

type UpdateGlossaryRevisionRequest struct {
	Version       int64      `json:"version" binding:"required,gt=0" minimum:"1"`
	Name          string     `json:"name" binding:"required"`
	Alias         []string   `json:"alias"`
	Definition    string     `json:"definition" binding:"required"`
	Example       string     `json:"example"`
	Note          string     `json:"note"`
	RelatedIDs    []int64    `json:"related_ids"`
	ChangeSummary string     `json:"change_summary" binding:"required"`
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
}

type UpdateGlossaryElementsRequest struct {
	Version    int64   `json:"version" binding:"required,gt=0" minimum:"1"`
	ElementIDs []int64 `json:"element_ids"`
}

// VersionRequest 是只需要资源乐观锁版本的通用写请求。
type VersionRequest struct {
	Version int64 `json:"version" binding:"required,gt=0" minimum:"1"`
}
