package models

import "time"

const (
	CodeSetOriginPlatform    = "platform"
	CodeSetOriginTenant      = "tenant"
	CodeItemStatusActive     = "active"
	CodeItemStatusDeprecated = "deprecated"
)

type CodeSet struct {
	ID              int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID        int64       `gorm:"not null;index;uniqueIndex:uq_standard_code_sets_tenant_code" json:"tenant_id"`
	ScopeType       string      `gorm:"size:20;not null;default:'tenant_common';index" json:"scope_type" enums:"platform,tenant_common,domain"`
	OwnerDomainID   *int64      `gorm:"index" json:"owner_domain_id,omitempty"`
	Code            string      `gorm:"size:100;not null;uniqueIndex:uq_standard_code_sets_tenant_code" json:"code"`
	Origin          string      `gorm:"size:20;not null;default:'tenant'" json:"origin"`
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

func (CodeSet) TableName() string { return "standard.code_sets" }

type CodeSetRevision struct {
	ID            int64                 `gorm:"primaryKey;autoIncrement" json:"id"`
	CodeSetID     int64                 `gorm:"not null;index;uniqueIndex:uq_standard_code_set_revisions_set_no" json:"code_set_id"`
	RevisionNo    int64                 `gorm:"not null;uniqueIndex:uq_standard_code_set_revisions_set_no" json:"revision_no"`
	Status        string                `gorm:"size:20;not null" json:"status"`
	Name          string                `gorm:"size:200;not null" json:"name"`
	Description   string                `gorm:"type:text;not null" json:"description"`
	ValueType     string                `gorm:"size:20;not null" json:"value_type"`
	ChangeSummary string                `gorm:"type:text;not null" json:"change_summary"`
	EffectiveFrom *time.Time            `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time            `json:"effective_to,omitempty"`
	SubmittedBy   *int64                `json:"submitted_by,omitempty"`
	SubmittedAt   *time.Time            `json:"submitted_at,omitempty"`
	PublishedBy   *int64                `json:"published_by,omitempty"`
	PublishedAt   *time.Time            `json:"published_at,omitempty"`
	CreatedBy     int64                 `gorm:"not null" json:"created_by"`
	UpdatedBy     *int64                `json:"updated_by,omitempty"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
	Items         []CodeSetRevisionItem `gorm:"-" json:"items"`
}

func (CodeSetRevision) TableName() string { return "standard.code_set_revisions" }

type CodeSetRevisionItem struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	CodeSetRevisionID int64     `gorm:"not null;index;uniqueIndex:uq_standard_code_set_revision_items_revision_code" json:"code_set_revision_id"`
	Code              string    `gorm:"size:100;not null;uniqueIndex:uq_standard_code_set_revision_items_revision_code" json:"code"`
	Label             string    `gorm:"size:200;not null" json:"label"`
	Definition        string    `gorm:"type:text" json:"definition"`
	SortOrder         int       `gorm:"not null;default:0" json:"sort_order"`
	Status            string    `gorm:"size:20;not null;default:'active'" json:"status"`
	ReplacementItemID *int64    `json:"replacement_item_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (CodeSetRevisionItem) TableName() string { return "standard.code_set_revision_items" }

type CodeSetAggregate struct {
	CodeSet
	CurrentRevision *CodeSetRevision `json:"current_revision,omitempty"`
	DraftRevision   *CodeSetRevision `json:"draft_revision,omitempty"`
}

type CreateCodeSetRequest struct {
	ScopeType     string     `json:"scope_type" binding:"required" enums:"tenant_common,domain"`
	OwnerDomainID *int64     `json:"owner_domain_id,omitempty"`
	Code          string     `json:"code" binding:"required"`
	StewardID     *int64     `json:"steward_id,omitempty"`
	Tags          []string   `json:"tags"`
	Name          string     `json:"name" binding:"required"`
	Description   string     `json:"description" binding:"required"`
	ValueType     string     `json:"value_type" binding:"required"`
	ChangeSummary string     `json:"change_summary" binding:"required"`
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
}

type UpdateCodeSetRequest struct {
	Version       int64    `json:"version" binding:"required,gt=0" minimum:"1"`
	ScopeType     string   `json:"scope_type" binding:"required" enums:"tenant_common,domain"`
	OwnerDomainID *int64   `json:"owner_domain_id,omitempty"`
	StewardID     *int64   `json:"steward_id,omitempty"`
	Tags          []string `json:"tags"`
}

type CreateCodeSetRevisionRequest struct {
	Version       int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	ChangeSummary string `json:"change_summary" binding:"required"`
}

type UpdateCodeSetRevisionRequest struct {
	Version       int64      `json:"version" binding:"required,gt=0" minimum:"1"`
	Name          string     `json:"name" binding:"required"`
	Description   string     `json:"description" binding:"required"`
	ValueType     string     `json:"value_type" binding:"required"`
	ChangeSummary string     `json:"change_summary" binding:"required"`
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
}

type CreateCodeItemRequest struct {
	Version    int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	Code       string `json:"code" binding:"required"`
	Label      string `json:"label" binding:"required"`
	Definition string `json:"definition"`
	SortOrder  int    `json:"sort_order"`
	Status     string `json:"status" binding:"required"`
}

type UpdateCodeItemRequest struct {
	Version           int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	Label             string `json:"label" binding:"required"`
	Definition        string `json:"definition"`
	SortOrder         int    `json:"sort_order"`
	Status            string `json:"status" binding:"required"`
	ReplacementItemID *int64 `json:"replacement_item_id,omitempty"`
}

type CodeItemMutationResponse struct {
	Item    *CodeSetRevisionItem `json:"item"`
	Version int64                `json:"version"`
}
