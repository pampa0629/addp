package models

import (
	"time"

	"github.com/addp/common/dataprotection"
)

const (
	FindingReviewDecisionConfirm = "confirm"
	FindingReviewDecisionAdjust  = "adjust"
	FindingReviewDecisionReject  = "reject"
)

// SensitiveFindingReview is the immutable first governance decision for one Finding.
type SensitiveFindingReview struct {
	ID                  string    `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID            int64     `gorm:"not null;index;uniqueIndex:uq_security_finding_review" json:"-"`
	FindingID           string    `gorm:"type:uuid;not null;uniqueIndex:uq_security_finding_review" json:"finding_id"`
	Decision            string    `gorm:"size:16;not null" json:"decision"`
	SensitiveDataTypeID *int64    `json:"sensitive_data_type_id,omitempty,string"`
	SecurityGradeID     *int64    `json:"security_grade_id,omitempty,string"`
	Rationale           string    `gorm:"type:text;not null" json:"rationale"`
	ReviewedBy          int64     `gorm:"not null" json:"reviewed_by,string"`
	CreatedAt           time.Time `gorm:"not null" json:"created_at"`
}

func (SensitiveFindingReview) TableName() string { return "security.sensitive_finding_reviews" }

// ResourceSecurityAssessment is the mutable aggregate pointer for one enrolled component.
type ResourceSecurityAssessment struct {
	ID              string    `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID        int64     `gorm:"not null;index;uniqueIndex:uq_security_assessment_component" json:"-"`
	EnrollmentID    string    `gorm:"type:uuid;not null;index;uniqueIndex:uq_security_assessment_component" json:"enrollment_id"`
	ComponentKey    string    `gorm:"type:text;not null;uniqueIndex:uq_security_assessment_component" json:"component_key"`
	Version         int64     `gorm:"not null;default:1" json:"version,string"`
	CurrentRevision int64     `gorm:"not null" json:"current_revision"`
	CreatedBy       int64     `gorm:"not null" json:"created_by,string"`
	CreatedAt       time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt       time.Time `gorm:"not null" json:"updated_at"`
}

func (ResourceSecurityAssessment) TableName() string {
	return "security.resource_security_assessments"
}

// ResourceSecurityAssessmentRevision is an immutable formal security conclusion.
type ResourceSecurityAssessmentRevision struct {
	ID                       string                   `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID                 int64                    `gorm:"not null;index" json:"-"`
	AssessmentID             string                   `gorm:"type:uuid;not null;index;uniqueIndex:uq_security_assessment_revision" json:"assessment_id"`
	Revision                 int64                    `gorm:"not null;uniqueIndex:uq_security_assessment_revision" json:"revision"`
	SourceFindingID          string                   `gorm:"type:uuid;not null" json:"source_finding_id"`
	SourceReviewID           string                   `gorm:"type:uuid;not null" json:"source_review_id"`
	SensitiveDataTypeID      int64                    `gorm:"not null" json:"sensitive_data_type_id,string"`
	SecurityClassificationID int64                    `gorm:"not null" json:"security_classification_id,string"`
	SecurityGradeID          int64                    `gorm:"not null" json:"security_grade_id,string"`
	SourceSnapshotHash       string                   `gorm:"size:80;not null" json:"source_snapshot_hash"`
	Component                dataprotection.Component `gorm:"type:jsonb;serializer:json;not null" json:"component"`
	Rationale                string                   `gorm:"type:text;not null" json:"rationale"`
	CreatedBy                int64                    `gorm:"not null" json:"created_by,string"`
	CreatedAt                time.Time                `gorm:"not null" json:"created_at"`
}

func (ResourceSecurityAssessmentRevision) TableName() string {
	return "security.resource_security_assessment_revisions"
}

type FindingReviewRequest struct {
	Decision            string `json:"decision" binding:"required"`
	SensitiveDataTypeID *int64 `json:"sensitive_data_type_id,omitempty,string"`
	SecurityGradeID     *int64 `json:"security_grade_id,omitempty,string"`
	Rationale           string `json:"rationale" binding:"required"`
}

type AssessmentRevisionRequest struct {
	Version             int64  `json:"version,string" binding:"required"`
	SensitiveDataTypeID int64  `json:"sensitive_data_type_id,string" binding:"required"`
	SecurityGradeID     int64  `json:"security_grade_id,string" binding:"required"`
	Rationale           string `json:"rationale" binding:"required"`
}

type FindingReviewResponse struct {
	Review     SensitiveFindingReview              `json:"review"`
	Assessment *ResourceSecurityAssessmentResponse `json:"assessment,omitempty"`
}

type ResourceSecurityAssessmentResponse struct {
	ResourceSecurityAssessment
	Current ResourceSecurityAssessmentRevision   `json:"current"`
	History []ResourceSecurityAssessmentRevision `json:"history,omitempty"`
}

type ResourceSecurityAssessmentListResponse struct {
	Data       []ResourceSecurityAssessmentResponse `json:"data"`
	Total      int64                                `json:"total"`
	Page       int                                  `json:"page"`
	PageSize   int                                  `json:"page_size"`
	TotalPages int                                  `json:"total_pages"`
}
