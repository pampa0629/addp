package models

import (
	"github.com/google/uuid"
	"time"
)

// RunApproval is Transfer's one-shot approval, not a task or an execution.
// It contains no credential or mutable authorization cache.
type RunApproval struct {
	ID                   uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID             uint       `gorm:"not null;index" json:"-"`
	TaskID               uint       `gorm:"not null;index" json:"-"`
	PrincipalID          string     `gorm:"not null" json:"-"`
	MembershipID         string     `gorm:"not null" json:"-"`
	AuthorizationVersion string     `gorm:"not null" json:"-"`
	AgentRunID           string     `gorm:"not null" json:"-"`
	RequestFingerprint   string     `gorm:"type:char(64);not null" json:"request_fingerprint"`
	RequestSummary       JSONMap    `gorm:"type:jsonb;not null" json:"request_summary"`
	Status               string     `gorm:"not null" json:"status"`
	RequestedAt          time.Time  `gorm:"not null" json:"requested_at"`
	ExpiresAt            time.Time  `gorm:"not null;index" json:"expires_at"`
	DecidedAt            *time.Time `json:"decided_at,omitempty"`
	ConsumedAt           *time.Time `json:"consumed_at,omitempty"`
	ExecutionID          *string    `json:"execution_id,omitempty"`
}

func (RunApproval) TableName() string { return "transfer.run_approvals" }

// RunApprovalDecision binds the user's click to exactly the displayed review.
type RunApprovalDecision struct {
	Decision           string `json:"decision" binding:"required,oneof=approved rejected"`
	RequestFingerprint string `json:"request_fingerprint" binding:"required"`
}

// StartTaskRequest is empty for the original user start. A delegated first
// request is also empty; a resume contains only the owner reference and digest.
type StartTaskRequest struct {
	ApprovalID         string `json:"approval_id,omitempty"`
	RequestFingerprint string `json:"request_fingerprint,omitempty"`
}

type RunApprovalRequired struct {
	Status             string    `json:"status"`
	InteractionID      string    `json:"interaction_id"`
	RequestFingerprint string    `json:"request_fingerprint"`
	RequestSummary     JSONMap   `json:"request_summary"`
	ExpiresAt          time.Time `json:"expires_at"`
}
