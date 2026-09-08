package models

import (
	"time"

	"github.com/addp/common/dataprotection"
)

const (
	ProtectionAccessRequestStatePending                    = "pending"
	ProtectionAccessRequestStateApproved                   = "approved"
	ProtectionAccessRequestStateRejected                   = "rejected"
	ProtectionAccessRequestStateExpired                    = "expired"
	ProtectionAccessRequestReviewScopePending              = "pending"
	ProtectionAccessRequestReviewScopeHistory              = "history"
	ProtectionAccessRequestDecisionUnavailableSelfApproval = "self_approval_forbidden"
)

type ProtectionAccessRequest struct {
	ID                   string     `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID             int64      `gorm:"not null;index" json:"-"`
	AssessmentID         string     `gorm:"type:uuid;not null;index" json:"assessment_id"`
	AssessmentRevision   int64      `gorm:"not null" json:"assessment_revision"`
	ConsumerOwner        string     `gorm:"size:32;not null" json:"consumer_owner"`
	Action               string     `gorm:"size:32;not null" json:"action"`
	SubjectType          string     `gorm:"size:16;not null" json:"-"`
	SubjectID            string     `gorm:"size:64;not null;index" json:"-"`
	SubjectDisplayName   string     `gorm:"size:255;not null;default:''" json:"-"`
	RequestedExpiresAt   time.Time  `gorm:"not null" json:"requested_expires_at"`
	Rationale            string     `gorm:"type:text;not null" json:"rationale"`
	State                string     `gorm:"size:16;not null;index" json:"state"`
	Version              int64      `gorm:"not null;default:1" json:"version,string"`
	DecidedBy            *int64     `json:"-"`
	DecidedByDisplayName string     `gorm:"size:255;not null;default:''" json:"-"`
	DecidedAt            *time.Time `json:"decided_at,omitempty"`
	DecisionRationale    string     `gorm:"type:text;not null;default:''" json:"decision_rationale"`
	CreatedAt            time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt            time.Time  `gorm:"not null" json:"updated_at"`
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

// ProtectionAccessRequestReviewFilter is the domain-level filter for the
// tenant approval workspace. HTTP parsing stays in the API layer.
type ProtectionAccessRequestReviewFilter struct {
	Scope           string
	State           string
	RequesterSearch string
	ResourceSearch  string
	CreatedFrom     *time.Time
	CreatedTo       *time.Time
}

type ProtectionAccessRequestResponse struct {
	ProtectionAccessRequest
	Requester                 ProtectionAccessActor    `json:"requester"`
	Reviewer                  *ProtectionAccessActor   `json:"reviewer,omitempty"`
	Component                 dataprotection.Component `json:"component"`
	TargetFullName            string                   `json:"target_full_name"`
	ExemptionID               string                   `json:"exemption_id,omitempty"`
	CanDecide                 bool                     `json:"can_decide"`
	DecisionUnavailableReason string                   `json:"decision_unavailable_reason,omitempty"`
}

type ProtectionAccessActor struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type ProtectionAccessRequestListResponse struct {
	Data       []ProtectionAccessRequestResponse `json:"data"`
	Total      int64                             `json:"total"`
	Page       int                               `json:"page"`
	PageSize   int                               `json:"page_size"`
	TotalPages int                               `json:"total_pages"`
}

type ProtectionAccessTarget struct {
	AssessmentID       string                          `json:"assessment_id"`
	AssessmentRevision int64                           `json:"assessment_revision,string"`
	Component          dataprotection.Component        `json:"component"`
	Requestable        bool                            `json:"requestable"`
	UnavailableReason  string                          `json:"unavailable_reason,omitempty"`
	AccessRequest      *ProtectionAccessRequestSummary `json:"access_request,omitempty"`
	ActiveExemptionID  string                          `json:"active_exemption_id,omitempty"`
	AuthorizedUntil    *time.Time                      `json:"authorized_until,omitempty"`
}

type ProtectionAccessRequestSummary struct {
	ID                 string    `json:"id"`
	State              string    `json:"state"`
	RequestedExpiresAt time.Time `json:"requested_expires_at"`
}

type ProtectionAccessTargetListResponse struct {
	Data []ProtectionAccessTarget `json:"data"`
}
