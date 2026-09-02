package models

import (
	"time"

	"github.com/addp/common/dataprotection"
	commonmodels "github.com/addp/common/models"
)

const (
	FindingDetectorPhoneMetadataV1 = "addp.detector.phone_metadata/v1"
	FindingDetectorPhoneDocumentV1 = "addp.detector.phone_document/v1"
	FindingEvidenceSchemaV1        = "addp.sensitive_finding_evidence/v1"
)

// SensitiveFinding is an immutable, value-free detector observation.
type SensitiveFinding struct {
	ID                  string                   `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID            int64                    `gorm:"not null;index;uniqueIndex:uq_security_finding_observation" json:"-"`
	EnrollmentID        string                   `gorm:"type:uuid;not null;index;uniqueIndex:uq_security_finding_observation" json:"enrollment_id"`
	ComponentKey        string                   `gorm:"type:text;not null;uniqueIndex:uq_security_finding_observation" json:"component_key"`
	SensitiveDataTypeID int64                    `gorm:"not null;index" json:"sensitive_data_type_id,string"`
	DetectorCode        string                   `gorm:"size:100;not null" json:"detector_code"`
	DetectorVersion     string                   `gorm:"size:100;not null;uniqueIndex:uq_security_finding_observation" json:"detector_version"`
	Confidence          float64                  `gorm:"not null" json:"confidence"`
	Evidence            commonmodels.JSONMap     `gorm:"type:jsonb;not null" json:"evidence"`
	Component           dataprotection.Component `gorm:"type:jsonb;serializer:json;not null" json:"component"`
	SourceSnapshotHash  string                   `gorm:"size:80;not null;uniqueIndex:uq_security_finding_observation" json:"source_snapshot_hash"`
	ObservedAt          time.Time                `gorm:"not null" json:"observed_at"`
	CreatedAt           time.Time                `gorm:"not null" json:"created_at"`
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
	Review *SensitiveFindingReview `json:"review,omitempty"`
}
