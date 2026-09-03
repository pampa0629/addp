package models

import (
	"time"

	"github.com/addp/common/dataprotection"
	commonmodels "github.com/addp/common/models"
)

const (
	FindingDetectorPhoneMetadataV2 = "addp.detector.phone_metadata/v2"
	FindingDetectorEmailMetadataV1 = "addp.detector.email_metadata/v1"
	FindingDetectorPhoneDocumentV1 = "addp.detector.phone_document/v1"
	FindingEvidenceSchemaV1        = "addp.sensitive_finding_evidence/v1"

	FindingDecisionAutomatic        = "automatic"
	FindingDecisionFormal           = "formal"
	FindingDecisionAwaitingReview   = "awaiting_review"
	FindingDecisionDetectorInactive = "detector_inactive"
	FindingDecisionBaselineMissing  = "baseline_missing"
	FindingDecisionRejected         = "rejected"
	FindingDecisionRevoked          = "revoked"
	FindingDecisionSuperseded       = "superseded"

	FindingGovernanceDetectorDefault = "detector_default"
	FindingGovernanceAssessment      = "assessment"

	FindingSnapshotScopeAll     = "all"
	FindingSnapshotScopeCurrent = "current"
	FindingReviewStateAll       = "all"
	FindingReviewStatePending   = "pending"
	FindingReviewStateReviewed  = "reviewed"
)

// SensitiveFinding is an immutable, value-free detector observation.
type SensitiveFinding struct {
	ID                   string                   `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID             int64                    `gorm:"not null;index;uniqueIndex:uq_security_finding_observation" json:"-"`
	EnrollmentID         string                   `gorm:"type:uuid;not null;index;uniqueIndex:uq_security_finding_observation" json:"enrollment_id"`
	DiscoveryExecutionID string                   `gorm:"size:255;not null;default:'';index;uniqueIndex:uq_security_finding_observation" json:"discovery_execution_id"`
	ComponentKey         string                   `gorm:"type:text;not null;uniqueIndex:uq_security_finding_observation" json:"component_key"`
	SensitiveDataTypeID  int64                    `gorm:"not null;index" json:"sensitive_data_type_id,string"`
	DetectorCode         string                   `gorm:"size:100;not null" json:"detector_code"`
	DetectorVersion      string                   `gorm:"size:100;not null;uniqueIndex:uq_security_finding_observation" json:"detector_version"`
	Confidence           float64                  `gorm:"not null" json:"confidence"`
	Evidence             commonmodels.JSONMap     `gorm:"type:jsonb;not null" json:"evidence"`
	Component            dataprotection.Component `gorm:"type:jsonb;serializer:json;not null" json:"component"`
	SourceSnapshotHash   string                   `gorm:"size:80;not null;uniqueIndex:uq_security_finding_observation" json:"source_snapshot_hash"`
	ObservedAt           time.Time                `gorm:"not null" json:"observed_at"`
	CreatedAt            time.Time                `gorm:"not null" json:"created_at"`
}

func (SensitiveFinding) TableName() string { return "security.sensitive_findings" }

type SensitiveFindingListResponse struct {
	Data       []SensitiveFindingResponse `json:"data"`
	Total      int64                      `json:"total"`
	Page       int                        `json:"page"`
	PageSize   int                        `json:"page_size"`
	TotalPages int                        `json:"total_pages"`
}

// SensitiveFindingResponse keeps the immutable observation and its optional
// immutable first review together, without exposing any raw sensitive value.
type SensitiveFindingResponse struct {
	SensitiveFinding
	TargetSnapshot ProtectionTargetSnapshot    `json:"target_snapshot"`
	Review         *SensitiveFindingReview     `json:"review,omitempty"`
	Explanation    SensitiveFindingExplanation `json:"explanation"`
}

// SensitiveFindingExplanation is a read-only, value-free projection of the
// control-plane facts that explain how one finding affects data outlets.
// It is assembled at query time and is never persisted or compiled back into
// a ProtectionProjection.
type SensitiveFindingExplanation struct {
	Capability                        *DetectorCapability        `json:"capability,omitempty"`
	AutomaticAdoptionThreshold        *float64                   `json:"automatic_adoption_threshold,omitempty"`
	MeetsAutomaticThreshold           bool                       `json:"meets_automatic_threshold"`
	DecisionState                     string                     `json:"decision_state"`
	GovernanceSource                  string                     `json:"governance_source,omitempty"`
	EffectiveSensitiveDataTypeID      *int64                     `json:"effective_sensitive_data_type_id,omitempty,string"`
	EffectiveSecurityClassificationID *int64                     `json:"effective_security_classification_id,omitempty,string"`
	EffectiveSecurityGradeID          *int64                     `json:"effective_security_grade_id,omitempty,string"`
	AssessmentID                      string                     `json:"assessment_id,omitempty"`
	Baseline                          *FindingProtectionBaseline `json:"baseline,omitempty"`
	Outlets                           []FindingOutletProtection  `json:"outlets"`
}

type FindingProtectionBaseline struct {
	ID                 int64  `json:"id,string"`
	Version            int64  `json:"version,string"`
	Effect             string `json:"effect"`
	Algorithm          string `json:"algorithm,omitempty"`
	KeepPrefix         int    `json:"keep_prefix"`
	KeepSuffix         int    `json:"keep_suffix"`
	InvalidValueEffect string `json:"invalid_value_effect"`
}

type FindingOutletProtection struct {
	ConsumerOwner   string                        `json:"consumer_owner"`
	ProjectionState string                        `json:"projection_state"`
	Acknowledged    bool                          `json:"acknowledged"`
	Rules           []FindingOutletProtectionRule `json:"rules"`
}

type FindingOutletProtectionRule struct {
	Action    string `json:"action"`
	Effect    string `json:"effect"`
	Algorithm string `json:"algorithm,omitempty"`
}
