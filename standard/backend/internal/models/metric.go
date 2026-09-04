package models

import "time"

const (
	MetricTypeAtomic          = "atomic"
	MetricTypeDerived         = "derived"
	MetricTypeComposite       = "composite"
	MetricDependencyBase      = "base"
	MetricDependencyComponent = "component"
)

type MetricCategory struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64     `gorm:"not null;index;uniqueIndex:uq_standard_metric_categories_tenant_code" json:"tenant_id"`
	Name        string    `gorm:"size:100;not null" json:"name"`
	Code        string    `gorm:"size:50;not null;uniqueIndex:uq_standard_metric_categories_tenant_code" json:"code"`
	Description string    `gorm:"type:text" json:"description"`
	ParentID    *int64    `gorm:"index" json:"parent_id,omitempty"`
	SortOrder   int       `gorm:"default:0" json:"sort_order"`
	CreatedBy   int64     `gorm:"not null" json:"created_by"`
	UpdatedBy   *int64    `json:"updated_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int64     `gorm:"not null;default:1" json:"version"`
}

func (MetricCategory) TableName() string { return "standard.metric_categories" }

type CreateMetricCategoryRequest struct {
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	SortOrder   int    `json:"sort_order"`
}

type UpdateMetricCategoryRequest struct {
	Version     int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	SortOrder   int    `json:"sort_order"`
}

// MetricDefinition 是指标定义的稳定身份；业务定义只存在于修订中。
type MetricDefinition struct {
	ID              int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID        int64       `gorm:"not null;index;uniqueIndex:uq_standard_metric_definitions_tenant_code" json:"tenant_id"`
	CategoryID      *int64      `gorm:"index" json:"category_id,omitempty"`
	ScopeType       string      `gorm:"size:20;not null;default:'tenant_common';index" json:"scope_type" enums:"platform,tenant_common,domain"`
	OwnerDomainID   *int64      `gorm:"index" json:"owner_domain_id,omitempty"`
	Code            string      `gorm:"size:100;not null;uniqueIndex:uq_standard_metric_definitions_tenant_code" json:"code"`
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

func (MetricDefinition) TableName() string { return "standard.metric_definitions" }

type MetricDefinitionRevision struct {
	ID                 int64                                `gorm:"primaryKey;autoIncrement" json:"id"`
	MetricDefinitionID int64                                `gorm:"not null;index;uniqueIndex:uq_standard_metric_definition_revisions_definition_no" json:"metric_definition_id"`
	RevisionNo         int64                                `gorm:"not null;uniqueIndex:uq_standard_metric_definition_revisions_definition_no" json:"revision_no"`
	Status             string                               `gorm:"size:20;not null" json:"status"`
	MetricType         string                               `gorm:"size:20;not null" json:"metric_type" enums:"atomic,derived,composite"`
	Name               string                               `gorm:"size:200;not null" json:"name"`
	Definition         string                               `gorm:"type:text;not null" json:"definition"`
	StatisticalCaliber string                               `gorm:"type:text;not null" json:"statistical_caliber"`
	SemanticFormula    string                               `gorm:"type:text" json:"semantic_formula"`
	UnitID             *int64                               `gorm:"index" json:"unit_id,omitempty"`
	ChangeSummary      string                               `gorm:"type:text;not null" json:"change_summary"`
	EffectiveFrom      *time.Time                           `json:"effective_from,omitempty"`
	EffectiveTo        *time.Time                           `json:"effective_to,omitempty"`
	SubmittedBy        *int64                               `json:"submitted_by,omitempty"`
	SubmittedAt        *time.Time                           `json:"submitted_at,omitempty"`
	PublishedBy        *int64                               `json:"published_by,omitempty"`
	PublishedAt        *time.Time                           `json:"published_at,omitempty"`
	CreatedBy          int64                                `gorm:"not null" json:"created_by"`
	UpdatedBy          *int64                               `json:"updated_by,omitempty"`
	CreatedAt          time.Time                            `json:"created_at"`
	UpdatedAt          time.Time                            `json:"updated_at"`
	Dependencies       []MetricDefinitionRevisionDependency `gorm:"foreignKey:MetricDefinitionRevisionID" json:"dependencies"`
}

func (MetricDefinitionRevision) TableName() string { return "standard.metric_definition_revisions" }

type MetricDefinitionRevisionDependency struct {
	ID                         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	MetricDefinitionRevisionID int64     `gorm:"not null;index;uniqueIndex:uq_standard_metric_revision_dependencies" json:"metric_definition_revision_id"`
	DependencyDefinitionID     int64     `gorm:"not null;index;uniqueIndex:uq_standard_metric_revision_dependencies" json:"dependency_definition_id"`
	DependencyRevisionID       *int64    `gorm:"index" json:"dependency_revision_id,omitempty"`
	RelationKind               string    `gorm:"size:20;not null;uniqueIndex:uq_standard_metric_revision_dependencies" json:"relation_kind" enums:"base,component"`
	Coefficient                *float64  `json:"coefficient,omitempty"`
	Note                       string    `gorm:"type:text" json:"note"`
	CreatedAt                  time.Time `json:"created_at"`
}

func (MetricDefinitionRevisionDependency) TableName() string {
	return "standard.metric_definition_revision_dependencies"
}

type MetricDefinitionAggregate struct {
	MetricDefinition
	CurrentRevision *MetricDefinitionRevision `json:"current_revision,omitempty"`
	DraftRevision   *MetricDefinitionRevision `json:"draft_revision,omitempty"`
}

type PublishedMetricDefinitionReference struct {
	ID             int64  `json:"id"`
	TenantID       int64  `json:"tenant_id"`
	ScopeType      string `json:"scope_type"`
	OwnerDomainID  *int64 `json:"owner_domain_id,omitempty"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	MetricType     string `json:"metric_type"`
	Status         string `json:"status"`
	LifecycleState string `json:"lifecycle_state"`
	Version        int64  `json:"version"`
	RevisionID     int64  `json:"revision_id"`
	RevisionNo     int64  `json:"revision_no"`
}

type MetricDefinitionDependencyInput struct {
	MetricDefinitionID int64    `json:"metric_definition_id" binding:"required,gt=0"`
	RelationKind       string   `json:"relation_kind" binding:"required" enums:"base,component"`
	Coefficient        *float64 `json:"coefficient,omitempty"`
	Note               string   `json:"note"`
}

type CreateMetricRequest struct {
	CategoryID         *int64                            `json:"category_id,omitempty"`
	ScopeType          string                            `json:"scope_type" binding:"required" enums:"tenant_common,domain"`
	OwnerDomainID      *int64                            `json:"owner_domain_id,omitempty"`
	Code               string                            `json:"code" binding:"required"`
	StewardID          *int64                            `json:"steward_id,omitempty"`
	Tags               []string                          `json:"tags"`
	MetricType         string                            `json:"metric_type" binding:"required" enums:"atomic,derived,composite"`
	Name               string                            `json:"name" binding:"required"`
	Definition         string                            `json:"definition" binding:"required"`
	StatisticalCaliber string                            `json:"statistical_caliber" binding:"required"`
	SemanticFormula    string                            `json:"semantic_formula"`
	UnitID             *int64                            `json:"unit_id,omitempty"`
	Dependencies       []MetricDefinitionDependencyInput `json:"dependencies"`
	ChangeSummary      string                            `json:"change_summary" binding:"required"`
	EffectiveFrom      *time.Time                        `json:"effective_from,omitempty"`
	EffectiveTo        *time.Time                        `json:"effective_to,omitempty"`
}

type UpdateMetricRequest struct {
	Version       int64    `json:"version" binding:"required,gt=0" minimum:"1"`
	CategoryID    *int64   `json:"category_id,omitempty"`
	ScopeType     string   `json:"scope_type" binding:"required" enums:"tenant_common,domain"`
	OwnerDomainID *int64   `json:"owner_domain_id,omitempty"`
	StewardID     *int64   `json:"steward_id,omitempty"`
	Tags          []string `json:"tags"`
}

type CreateMetricRevisionRequest struct {
	Version       int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	ChangeSummary string `json:"change_summary" binding:"required"`
}

type UpdateMetricRevisionRequest struct {
	Version            int64                             `json:"version" binding:"required,gt=0" minimum:"1"`
	MetricType         string                            `json:"metric_type" binding:"required" enums:"atomic,derived,composite"`
	Name               string                            `json:"name" binding:"required"`
	Definition         string                            `json:"definition" binding:"required"`
	StatisticalCaliber string                            `json:"statistical_caliber" binding:"required"`
	SemanticFormula    string                            `json:"semantic_formula"`
	UnitID             *int64                            `json:"unit_id,omitempty"`
	Dependencies       []MetricDefinitionDependencyInput `json:"dependencies"`
	ChangeSummary      string                            `json:"change_summary" binding:"required"`
	EffectiveFrom      *time.Time                        `json:"effective_from,omitempty"`
	EffectiveTo        *time.Time                        `json:"effective_to,omitempty"`
}
