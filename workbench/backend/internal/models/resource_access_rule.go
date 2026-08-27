package models

import "time"

const (
	ResourceTypeDataApplication           = "data_application"
	ResourceAccessSubjectUser             = "user"
	ResourceAccessEffectAllow             = "allow"
	ResourceAccessEffectDeny              = "deny"
	ResourceAccessSourceAsset             = "asset"
	DataApplicationExecutePermission      = "workbench.data_application.execute"
	ResourceGrantFulfillmentStatusActive  = "effective"
	ResourceGrantFulfillmentStatusRevoked = "revoked"
)

// ResourceAccessRule is the Workbench-owned final resource authorization fact.
// Asset is only a source of allow rules; runtime decisions remain in Workbench.
type ResourceAccessRule struct {
	ID             string     `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID       int64      `gorm:"not null;index:idx_workbench_resource_rule_runtime,priority:1;uniqueIndex:ux_workbench_resource_rule_source,priority:1" json:"-"`
	ResourceType   string     `gorm:"type:varchar(64);not null;index:idx_workbench_resource_rule_runtime,priority:2" json:"resource_type"`
	ResourceID     string     `gorm:"type:uuid;not null;index:idx_workbench_resource_rule_runtime,priority:3" json:"resource_id"`
	SubjectType    string     `gorm:"type:varchar(32);not null;index:idx_workbench_resource_rule_runtime,priority:4" json:"subject_type"`
	SubjectID      int64      `gorm:"not null;index:idx_workbench_resource_rule_runtime,priority:5" json:"-"`
	Permission     string     `gorm:"type:varchar(128);not null;index:idx_workbench_resource_rule_runtime,priority:6" json:"permission"`
	Effect         string     `gorm:"type:varchar(16);not null;index:idx_workbench_resource_rule_runtime,priority:7" json:"effect"`
	SourceModule   string     `gorm:"type:varchar(64);not null;uniqueIndex:ux_workbench_resource_rule_source,priority:2" json:"source_module"`
	SourceIdentity string     `gorm:"type:varchar(128);not null;uniqueIndex:ux_workbench_resource_rule_source,priority:3" json:"source_identity"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (ResourceAccessRule) TableName() string { return "workbench.resource_access_rules" }

type AssetResourceGrantRequest struct {
	ResourceType string     `json:"resource_type" binding:"required" enums:"data_application"`
	ResourceID   string     `json:"resource_id" binding:"required"`
	SubjectType  string     `json:"subject_type" binding:"required" enums:"user"`
	SubjectID    string     `json:"subject_id" binding:"required"`
	Permission   string     `json:"permission" binding:"required" enums:"workbench.data_application.execute"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type AssetResourceGrantResponse struct {
	ID             string     `json:"id"`
	SourceIdentity string     `json:"source_identity"`
	Status         string     `json:"status" enums:"effective,revoked"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
}
