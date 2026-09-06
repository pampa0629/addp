package models

import (
	"time"

	"github.com/addp/common/dataprotection"
)

const (
	ProtectionAccessRequestStatePending  = "pending"
	ProtectionAccessRequestStateApproved = "approved"
	ProtectionAccessRequestStateRejected = "rejected"
)

type ProtectionAccessRequest struct {
	ID                 string     `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID           int64      `gorm:"not null;index" json:"-"`
	AssessmentID       string     `gorm:"type:uuid;not null;index" json:"assessment_id"`
	AssessmentRevision int64      `gorm:"not null" json:"assessment_revision"`
	ConsumerOwner      string     `gorm:"size:32;not null" json:"consumer_owner"`
	Action             string     `gorm:"size:32;not null" json:"action"`
	SubjectType        string     `gorm:"size:16;not null" json:"subject_type"`
	SubjectID          string     `gorm:"size:64;not null;index" json:"subject_id"`
	RequestedExpiresAt time.Time  `gorm:"not null" json:"requested_expires_at"`
	Rationale          string     `gorm:"type:text;not null" json:"rationale"`
	State              string     `gorm:"size:16;not null;index" json:"state"`
	Version            int64      `gorm:"not null;default:1" json:"version,string"`
	DecidedBy          *int64     `json:"decided_by,omitempty,string"`
	DecidedAt          *time.Time `json:"decided_at,omitempty"`
	DecisionRationale  string     `gorm:"type:text;not null;default:''" json:"decision_rationale"`
	CreatedAt          time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"not null" json:"updated_at"`
}

func (ProtectionAccessRequest) TableName() string {
	return "security.protection_access_requests"
}

type CreateProtectionAccessRequest struct {
	AssessmentID       string    `json:"assessment_id" binding:"required"`
	ConsumerOwner      string    `json:"consumer_owner" binding:"required"`
	Action             string    `json:"action" binding:"required"`
	RequestedExpiresAt time.Time `json:"requested_expires_at" binding:"required"`
	Rationale          string    `json:"rationale" binding:"required"`
}

type DecideProtectionAccessRequest struct {
	Version   int64     `json:"version" binding:"required"`
	Decision  string    `json:"decision" binding:"required"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Rationale string    `json:"rationale" binding:"required"`
}

type ProtectionAccessRequestResponse struct {
	ProtectionAccessRequest
	Component      dataprotection.Component `json:"component"`
	TargetFullName string                   `json:"target_full_name"`
	ExemptionID    string                   `json:"exemption_id,omitempty"`
}

type ProtectionAccessRequestListResponse struct {
	Data       []ProtectionAccessRequestResponse `json:"data"`
	Total      int64                             `json:"total"`
	Page       int                               `json:"page"`
	PageSize   int                               `json:"page_size"`
	TotalPages int                               `json:"total_pages"`
}

type ProtectionAccessTarget struct {
	AssessmentID       string                   `json:"assessment_id"`
	AssessmentRevision int64                    `json:"assessment_revision,string"`
	Component          dataprotection.Component `json:"component"`
	Requestable        bool                     `json:"requestable"`
	UnavailableReason  string                   `json:"unavailable_reason,omitempty"`
	PendingRequestID   string                   `json:"pending_request_id,omitempty"`
	ActiveExemptionID  string                   `json:"active_exemption_id,omitempty"`
	AuthorizedUntil    *time.Time               `json:"authorized_until,omitempty"`
}

type ProtectionAccessTargetListResponse struct {
	Data []ProtectionAccessTarget `json:"data"`
}
