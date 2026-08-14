package models

import "time"

const (
	StandardResourceDomain             = "domain"
	StandardResourceElement            = "element"
	StandardResourceDimensionHierarchy = "dimension_hierarchy"
	StandardResourceMetric             = "metric"

	StandardReferenceGuardOpen    = "open"
	StandardReferenceGuardFrozen  = "frozen"
	StandardReferenceGuardDeleted = "deleted"
)

type StandardReferenceGuard struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	TenantID     int64     `gorm:"not null;uniqueIndex:uq_model_standard_reference_guard" json:"tenant_id"`
	ResourceType string    `gorm:"size:32;not null;uniqueIndex:uq_model_standard_reference_guard" json:"resource_type"`
	ResourceID   int64     `gorm:"not null;uniqueIndex:uq_model_standard_reference_guard" json:"resource_id"`
	State        string    `gorm:"size:16;not null" json:"state"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (StandardReferenceGuard) TableName() string {
	return "model.standard_reference_guards"
}

type StandardReference struct {
	ResourceType string
	ResourceID   int64
}

type SetStandardReferenceGuardRequest struct {
	State string `json:"state" binding:"required,oneof=open frozen deleted" enums:"open,frozen,deleted"`
}

type StandardReferenceImpact struct {
	OwnerType string `json:"owner_type"`
	OwnerID   int64  `json:"owner_id"`
	Field     string `json:"field"`
}

type StandardReferenceImpactSummary struct {
	OwnerType string `json:"owner_type"`
	Field     string `json:"field"`
	Count     int64  `json:"count"`
}

type StandardReferenceGuardResponse struct {
	ResourceType    string                           `json:"resource_type"`
	ResourceID      int64                            `json:"resource_id"`
	State           string                           `json:"state"`
	ReferenceCount  int64                            `json:"reference_count"`
	Summary         []StandardReferenceImpactSummary `json:"summary"`
	Sample          []StandardReferenceImpact        `json:"sample"`
	SampleTruncated bool                             `json:"sample_truncated"`
}
