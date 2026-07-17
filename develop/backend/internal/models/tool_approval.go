package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	ToolApprovalStatusPending  = "pending"
	ToolApprovalStatusApproved = "approved"
	ToolApprovalStatusRejected = "rejected"
	ToolApprovalStatusExpired  = "expired"
	ToolApprovalStatusConsumed = "consumed"
)

type ToolApproval struct {
	ID                 uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	UserID             uint           `gorm:"not null;index" json:"user_id"`
	TenantID           uint           `gorm:"not null;index" json:"tenant_id"`
	AgentRunID         string         `gorm:"type:varchar(100);not null;index" json:"agent_run_id"`
	ToolCallID         string         `gorm:"type:varchar(100);not null" json:"tool_call_id"`
	ToolName           string         `gorm:"type:varchar(100);not null" json:"tool_name"`
	RequestFingerprint string         `gorm:"type:char(64);not null;index" json:"request_fingerprint"`
	RequestPayload     DevTaskContent `gorm:"type:jsonb;not null" json:"-"`
	RequestSummary     DevTaskContent `gorm:"type:jsonb;not null" json:"request_summary"`
	Status             string         `gorm:"type:varchar(20);not null;index" json:"status"`
	RequestedAt        time.Time      `gorm:"not null" json:"requested_at"`
	ExpiresAt          time.Time      `gorm:"not null;index" json:"expires_at"`
	DecidedAt          *time.Time     `json:"decided_at,omitempty"`
	DecidedByUserID    *uint          `json:"decided_by_user_id,omitempty"`
	ConsumedAt         *time.Time     `json:"consumed_at,omitempty"`
	ExecutionID        *string        `gorm:"type:varchar(100)" json:"execution_id,omitempty"`
}

func (ToolApproval) TableName() string {
	return "develop.tool_approvals"
}

func (approval *ToolApproval) BeforeCreate(_ *gorm.DB) error {
	if approval.ID == uuid.Nil {
		approval.ID = uuid.New()
	}
	return nil
}

type ToolApprovalDecisionRequest struct {
	Decision string `json:"decision" binding:"required,oneof=approved rejected"`
}

type ToolApprovalResponse struct {
	ID                 uuid.UUID      `json:"id"`
	ToolName           string         `json:"tool_name"`
	Status             string         `json:"status"`
	RequestFingerprint string         `json:"request_fingerprint"`
	RequestSummary     DevTaskContent `json:"request_summary"`
	RequestedAt        time.Time      `json:"requested_at"`
	ExpiresAt          time.Time      `json:"expires_at"`
	DecidedAt          *time.Time     `json:"decided_at,omitempty"`
	ConsumedAt         *time.Time     `json:"consumed_at,omitempty"`
	ExecutionID        *string        `json:"execution_id,omitempty"`
}

func NewToolApprovalResponse(approval *ToolApproval) ToolApprovalResponse {
	return ToolApprovalResponse{
		ID:                 approval.ID,
		ToolName:           approval.ToolName,
		Status:             approval.Status,
		RequestFingerprint: approval.RequestFingerprint,
		RequestSummary:     approval.RequestSummary,
		RequestedAt:        approval.RequestedAt,
		ExpiresAt:          approval.ExpiresAt,
		DecidedAt:          approval.DecidedAt,
		ConsumedAt:         approval.ConsumedAt,
		ExecutionID:        approval.ExecutionID,
	}
}

type ApprovalRequiredResponse struct {
	Status             string         `json:"status"`
	InteractionID      string         `json:"interaction_id"`
	OpenURL            string         `json:"open_url"`
	RequestFingerprint string         `json:"request_fingerprint"`
	RequestSummary     DevTaskContent `json:"request_summary"`
	ExpiresAt          time.Time      `json:"expires_at"`
}

type ExecutionStartedResponse struct {
	Message     string `json:"message"`
	ExecutionID string `json:"execution_id"`
}

type ToolApprovalErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ToolApprovalErrorResponse struct {
	Error ToolApprovalErrorBody `json:"error"`
}
