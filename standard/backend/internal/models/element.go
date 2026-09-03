package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

const (
	RevisionStatusDraft     = "draft"
	RevisionStatusInReview  = "in_review"
	RevisionStatusPublished = "published"
	RevisionStatusWithdrawn = "withdrawn"
	ValueDomainUnrestricted = "unrestricted"
	ValueDomainRange        = "range"
	ValueDomainEnumeration  = "enumeration"
)

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

// Element 是数据元的稳定身份。业务定义只存在于 ElementRevision。
type Element struct {
	ID              int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID        int64       `gorm:"not null;index;uniqueIndex:uq_standard_elements_tenant_code" json:"tenant_id"`
	ScopeType       string      `gorm:"size:20;not null;default:'tenant_common';index" json:"scope_type" enums:"platform,tenant_common,domain"`
	OwnerDomainID   *int64      `gorm:"index" json:"owner_domain_id,omitempty"`
	Code            string      `gorm:"size:100;not null;uniqueIndex:uq_standard_elements_tenant_code" json:"code"`
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

func (Element) TableName() string { return "standard.elements" }

type RangeConstraint struct {
	Min          *json.Number `json:"min,omitempty"`
	Max          *json.Number `json:"max,omitempty"`
	MinInclusive *bool        `json:"min_inclusive,omitempty"`
	MaxInclusive *bool        `json:"max_inclusive,omitempty"`
}

// ElementRevision 是数据元的一次完整业务定义快照。
type ElementRevision struct {
	ID                   int64            `gorm:"primaryKey;autoIncrement" json:"id"`
	ElementID            int64            `gorm:"not null;index;uniqueIndex:uq_standard_element_revisions_element_no" json:"element_id"`
	RevisionNo           int64            `gorm:"not null;uniqueIndex:uq_standard_element_revisions_element_no" json:"revision_no"`
	Status               string           `gorm:"size:20;not null" json:"status"`
	Name                 string           `gorm:"size:200;not null" json:"name"`
	Definition           string           `gorm:"type:text;not null" json:"definition"`
	DataType             string           `gorm:"size:50;not null" json:"data_type"`
	Length               *int             `json:"length,omitempty"`
	PrecisionNum         *int             `json:"precision_num,omitempty"`
	Scale                *int             `json:"scale,omitempty"`
	Nullable             bool             `gorm:"not null;default:true" json:"nullable"`
	DefaultValue         string           `gorm:"type:text" json:"default_value"`
	Format               string           `gorm:"size:200" json:"format"`
	ValueDomainKind      string           `gorm:"size:20;not null;default:'unrestricted'" json:"value_domain_kind"`
	RangeConstraint      *RangeConstraint `gorm:"type:jsonb;serializer:json" json:"range_constraint,omitempty"`
	CodeSetRevisionID    *int64           `gorm:"index" json:"code_set_revision_id,omitempty"`
	UnitID               *int64           `gorm:"index" json:"unit_id,omitempty"`
	ExampleValues        StringArray      `gorm:"type:jsonb;serializer:json" json:"example_values"`
	ExtraQualityRules    JSONB            `gorm:"type:jsonb;serializer:json" json:"extra_quality_rules"`
	CompiledQualityRules JSONB            `gorm:"type:jsonb;serializer:json" json:"compiled_quality_rules"`
	ChangeSummary        string           `gorm:"type:text;not null" json:"change_summary"`
	EffectiveFrom        *time.Time       `json:"effective_from,omitempty"`
	EffectiveTo          *time.Time       `json:"effective_to,omitempty"`
	SubmittedBy          *int64           `json:"submitted_by,omitempty"`
	SubmittedAt          *time.Time       `json:"submitted_at,omitempty"`
	PublishedBy          *int64           `json:"published_by,omitempty"`
	PublishedAt          *time.Time       `json:"published_at,omitempty"`
	CreatedBy            int64            `gorm:"not null" json:"created_by"`
	UpdatedBy            *int64           `json:"updated_by,omitempty"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
}

func (ElementRevision) TableName() string { return "standard.element_revisions" }

type ElementAggregate struct {
	Element
	CurrentRevision *ElementRevision `json:"current_revision,omitempty"`
	DraftRevision   *ElementRevision `json:"draft_revision,omitempty"`
}

// PublishedElementReference 是跨模块解析已发布数据元时使用的只读投影。
type PublishedElementReference struct {
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

type CreateElementRequest struct {
	ScopeType         string                 `json:"scope_type" binding:"required" enums:"tenant_common,domain"`
	OwnerDomainID     *int64                 `json:"owner_domain_id,omitempty"`
	Code              string                 `json:"code" binding:"required"`
	StewardID         *int64                 `json:"steward_id,omitempty"`
	Tags              []string               `json:"tags"`
	Name              string                 `json:"name" binding:"required"`
	Definition        string                 `json:"definition" binding:"required"`
	DataType          string                 `json:"data_type" binding:"required"`
	Length            *int                   `json:"length,omitempty"`
	PrecisionNum      *int                   `json:"precision_num,omitempty"`
	Scale             *int                   `json:"scale,omitempty"`
	Nullable          bool                   `json:"nullable"`
	DefaultValue      string                 `json:"default_value"`
	Format            string                 `json:"format"`
	ValueDomainKind   string                 `json:"value_domain_kind" binding:"required"`
	RangeConstraint   *RangeConstraint       `json:"range_constraint,omitempty"`
	CodeSetRevisionID *int64                 `json:"code_set_revision_id,omitempty"`
	UnitID            *int64                 `json:"unit_id,omitempty"`
	ExampleValues     []string               `json:"example_values"`
	ExtraQualityRules map[string]interface{} `json:"extra_quality_rules"`
	ChangeSummary     string                 `json:"change_summary" binding:"required"`
	EffectiveFrom     *time.Time             `json:"effective_from,omitempty"`
	EffectiveTo       *time.Time             `json:"effective_to,omitempty"`
}

type UpdateElementRequest struct {
	Version       int64    `json:"version" binding:"required,gt=0" minimum:"1"`
	ScopeType     string   `json:"scope_type" binding:"required" enums:"tenant_common,domain"`
	OwnerDomainID *int64   `json:"owner_domain_id,omitempty"`
	StewardID     *int64   `json:"steward_id,omitempty"`
	Tags          []string `json:"tags"`
}

type CreateElementRevisionRequest struct {
	Version       int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	ChangeSummary string `json:"change_summary" binding:"required"`
}

type UpdateElementRevisionRequest struct {
	Version           int64                  `json:"version" binding:"required,gt=0" minimum:"1"`
	Name              string                 `json:"name" binding:"required"`
	Definition        string                 `json:"definition" binding:"required"`
	DataType          string                 `json:"data_type" binding:"required"`
	Length            *int                   `json:"length,omitempty"`
	PrecisionNum      *int                   `json:"precision_num,omitempty"`
	Scale             *int                   `json:"scale,omitempty"`
	Nullable          bool                   `json:"nullable"`
	DefaultValue      string                 `json:"default_value"`
	Format            string                 `json:"format"`
	ValueDomainKind   string                 `json:"value_domain_kind" binding:"required"`
	RangeConstraint   *RangeConstraint       `json:"range_constraint,omitempty"`
	CodeSetRevisionID *int64                 `json:"code_set_revision_id,omitempty"`
	UnitID            *int64                 `json:"unit_id,omitempty"`
	ExampleValues     []string               `json:"example_values"`
	ExtraQualityRules map[string]interface{} `json:"extra_quality_rules"`
	ChangeSummary     string                 `json:"change_summary" binding:"required"`
	EffectiveFrom     *time.Time             `json:"effective_from,omitempty"`
	EffectiveTo       *time.Time             `json:"effective_to,omitempty"`
}

type RevisionActionRequest struct {
	Version int64 `json:"version" binding:"required,gt=0" minimum:"1"`
}

type PublishedElementQualityRulesResponse struct {
	ElementID         int64 `json:"element_id"`
	ElementRevisionID int64 `json:"element_revision_id"`
	RevisionNo        int64 `json:"revision_no"`
	QualityRules      JSONB `json:"quality_rules"`
}
