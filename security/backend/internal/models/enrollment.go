package models

import (
	"time"

	"github.com/addp/common/dataprotection"
)

const (
	EnrollmentStateActivating = "activating"
	EnrollmentStateEnrolling  = "enrolling"
	EnrollmentStateActive     = "active"
	EnrollmentStateReleasing  = "releasing"
	EnrollmentStateReleased   = "released"

	EnrollmentListScopeCurrent  = "current"
	EnrollmentListScopeReleased = "released"
	EnrollmentListScopeAll      = "all"

	ReleaseBasisManual              = "manual"
	ReleaseBasisNoSupportedFindings = "no_supported_findings"

	DiscoverySummaryStatusNotCompleted = "not_completed"
	DiscoverySummaryStatusCompleted    = "completed"
)

type ProtectionEnrollment struct {
	ID                        string     `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID                  int64      `gorm:"not null;index" json:"-"`
	TargetOwner               string     `gorm:"size:32;not null" json:"target_owner_module"`
	TargetType                string     `gorm:"size:64;not null" json:"target_resource_type"`
	TargetIdentity            string     `gorm:"type:text;not null" json:"target_resource_identity"`
	TargetEngineID            uint       `gorm:"not null;default:0" json:"target_engine_id"`
	TargetItemType            string     `gorm:"size:64;not null;default:''" json:"target_item_type"`
	TargetFullName            string     `gorm:"type:text;not null;default:''" json:"target_full_name"`
	State                     string     `gorm:"size:16;not null;index" json:"state"`
	Version                   int64      `gorm:"not null;default:1" json:"version,string"`
	ReleaseReason             string     `gorm:"type:text;not null;default:''" json:"release_reason,omitempty"`
	ReleaseBasis              string     `gorm:"size:32;not null;default:''" json:"release_basis,omitempty"`
	ReleaseRequestedBy        *int64     `json:"release_requested_by,omitempty,string"`
	ReleaseRequestedAt        *time.Time `json:"release_requested_at,omitempty"`
	ReleaseSourceSnapshotHash string     `gorm:"size:80;not null;default:''" json:"release_source_snapshot_hash,omitempty"`
	LatestSourceSnapshotHash  string     `gorm:"size:80;not null;default:''" json:"latest_source_snapshot_hash,omitempty"`
	LastDiscoveredAt          *time.Time `json:"last_discovered_at,omitempty"`
	CreatedBy                 int64      `gorm:"not null" json:"created_by,string"`
	ReleasedAt                *time.Time `json:"released_at,omitempty"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

func (ProtectionEnrollment) TableName() string { return "security.protection_enrollments" }

func (e ProtectionEnrollment) Target() dataprotection.ResourceReference {
	return dataprotection.ResourceReference{
		OwnerModule: e.TargetOwner, ResourceType: e.TargetType,
		ResourceIdentity: e.TargetIdentity,
	}
}

func (e ProtectionEnrollment) TargetSnapshot() ProtectionTargetSnapshot {
	return ProtectionTargetSnapshot{EngineID: e.TargetEngineID, ItemType: e.TargetItemType, FullName: e.TargetFullName}
}

type ProtectionProjectionRecord struct {
	ID                string    `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID          int64     `gorm:"not null;index;uniqueIndex:uq_security_projection_consumer" json:"-"`
	EnrollmentID      string    `gorm:"type:uuid;not null;index;uniqueIndex:uq_security_projection_consumer" json:"enrollment_id"`
	ConsumerOwner     string    `gorm:"size:32;not null;uniqueIndex:uq_security_projection_consumer" json:"consumer_owner"`
	Revision          string    `gorm:"size:20;not null" json:"revision"`
	State             string    `gorm:"size:16;not null" json:"state"`
	ProjectionPayload string    `gorm:"type:jsonb;not null" json:"-"`
	PublishedSequence int64     `gorm:"not null" json:"-"`
	ReleaseSequence   *int64    `json:"-"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (ProtectionProjectionRecord) TableName() string { return "security.protection_projections" }

type ProtectionProjectionChange struct {
	Sequence          int64     `gorm:"primaryKey;autoIncrement" json:"-"`
	ChangeID          string    `gorm:"type:uuid;not null;uniqueIndex" json:"change_id"`
	TenantID          int64     `gorm:"not null;index" json:"-"`
	EnrollmentID      string    `gorm:"type:uuid;not null;index" json:"-"`
	ConsumerOwner     string    `gorm:"size:32;not null;index" json:"-"`
	Operation         string    `gorm:"size:16;not null" json:"operation"`
	ProjectionID      string    `gorm:"type:uuid;not null" json:"-"`
	Revision          string    `gorm:"size:20;not null" json:"-"`
	TargetOwner       string    `gorm:"size:32;not null" json:"-"`
	TargetType        string    `gorm:"size:64;not null" json:"-"`
	TargetIdentity    string    `gorm:"type:text;not null" json:"-"`
	TargetComponent   string    `gorm:"type:text;not null;default:''" json:"-"`
	ProjectionPayload *string   `gorm:"type:jsonb" json:"-"`
	CreatedAt         time.Time `json:"-"`
}

func (ProtectionProjectionChange) TableName() string {
	return "security.protection_projection_changes"
}

type ProtectionProjectionAcknowledgement struct {
	TenantID      int64     `gorm:"primaryKey" json:"-"`
	ConsumerOwner string    `gorm:"size:32;primaryKey" json:"consumer_owner"`
	Sequence      int64     `gorm:"not null" json:"-"`
	AppliedCursor string    `gorm:"type:text;not null" json:"applied_cursor"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (ProtectionProjectionAcknowledgement) TableName() string {
	return "security.protection_projection_acknowledgements"
}

type CreateProtectionEnrollmentRequest struct {
	Locator string `json:"locator" binding:"required"`
}

type ProtectionTargetSnapshot struct {
	EngineID uint   `json:"engine_id"`
	ItemType string `json:"item_type"`
	FullName string `json:"full_name"`
}

type ReleaseProtectionEnrollmentRequest struct {
	Version int64  `json:"version" binding:"required"`
	Basis   string `json:"basis" binding:"required,oneof=manual no_supported_findings" enums:"manual,no_supported_findings"`
	Reason  string `json:"reason" binding:"required"`
}

type CreateProtectionDiscoveryExecutionRequest struct {
	Version int64 `json:"version" binding:"required"`
}

type ProtectionDiscoveryExecutionResponse struct {
	ExecutionID  string    `json:"execution_id"`
	EnrollmentID string    `json:"enrollment_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

type ProtectionOwnerProgress struct {
	ConsumerOwner   string     `json:"consumer_owner"`
	ProjectionID    string     `json:"projection_id"`
	Revision        string     `json:"revision"`
	ProjectionState string     `json:"projection_state"`
	Effects         []string   `json:"effects"`
	PublishedCursor string     `json:"published_cursor"`
	Acknowledged    bool       `json:"acknowledged"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`
}

type ProtectionDiscoverySummary struct {
	Status             string `json:"status"`
	FindingCount       int64  `json:"finding_count"`
	PendingReviewCount int64  `json:"pending_review_count"`
	ReviewedCount      int64  `json:"reviewed_count"`
}

type ProtectionEnrollmentResponse struct {
	ID                        string                           `json:"id"`
	Target                    dataprotection.ResourceReference `json:"target"`
	TargetSnapshot            ProtectionTargetSnapshot         `json:"target_snapshot"`
	State                     string                           `json:"state"`
	Version                   int64                            `json:"version,string"`
	ReleaseReason             string                           `json:"release_reason,omitempty"`
	ReleaseBasis              string                           `json:"release_basis,omitempty"`
	ReleaseRequestedBy        *int64                           `json:"release_requested_by,omitempty,string"`
	ReleaseRequestedAt        *time.Time                       `json:"release_requested_at,omitempty"`
	ReleaseSourceSnapshotHash string                           `json:"release_source_snapshot_hash,omitempty"`
	LatestSourceSnapshotHash  string                           `json:"latest_source_snapshot_hash,omitempty"`
	LastDiscoveredAt          *time.Time                       `json:"last_discovered_at,omitempty"`
	DiscoverySummary          ProtectionDiscoverySummary       `json:"discovery_summary"`
	OwnerProgress             []ProtectionOwnerProgress        `json:"owner_progress"`
	CreatedBy                 int64                            `json:"created_by,string"`
	ReleasedAt                *time.Time                       `json:"released_at,omitempty"`
	CreatedAt                 time.Time                        `json:"created_at"`
	UpdatedAt                 time.Time                        `json:"updated_at"`
}

type ProtectionEnrollmentListResponse struct {
	Data       []ProtectionEnrollmentResponse `json:"data"`
	Total      int64                          `json:"total"`
	Page       int                            `json:"page"`
	PageSize   int                            `json:"page_size"`
	TotalPages int                            `json:"total_pages"`
}
