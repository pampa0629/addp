package models

import "time"

const (
	ProtectionPolicyStateActive  = "active"
	ProtectionPolicyStateRevoked = "revoked"
)

// ProtectionPolicy is the mutable pointer for one explicit tightening of an
// Assessment at a concrete consumer action.
type ProtectionPolicy struct {
	ID              string    `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID        int64     `gorm:"not null;index;uniqueIndex:uq_security_policy_binding" json:"-"`
	AssessmentID    string    `gorm:"type:uuid;not null;index;uniqueIndex:uq_security_policy_binding" json:"assessment_id"`
	ConsumerOwner   string    `gorm:"size:32;not null;uniqueIndex:uq_security_policy_binding" json:"consumer_owner"`
	Action          string    `gorm:"size:32;not null;uniqueIndex:uq_security_policy_binding" json:"action"`
	State           string    `gorm:"size:16;not null" json:"state"`
	Version         int64     `gorm:"not null;default:1" json:"version,string"`
	CurrentRevision int64     `gorm:"not null" json:"current_revision"`
	CreatedBy       int64     `gorm:"not null" json:"created_by,string"`
	CreatedAt       time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt       time.Time `gorm:"not null" json:"updated_at"`
}

func (ProtectionPolicy) TableName() string { return "security.protection_policies" }

// ProtectionPolicyRevision is an immutable policy decision. Revoked revisions
// preserve the previous effect for audit while removing the explicit override.
type ProtectionPolicyRevision struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID  int64     `gorm:"not null;index" json:"-"`
	PolicyID  string    `gorm:"type:uuid;not null;index;uniqueIndex:uq_security_policy_revision" json:"policy_id"`
	Revision  int64     `gorm:"not null;uniqueIndex:uq_security_policy_revision" json:"revision"`
	State     string    `gorm:"size:16;not null" json:"state"`
	Effect    string    `gorm:"size:20;not null" json:"effect"`
	Rationale string    `gorm:"type:text;not null" json:"rationale"`
	CreatedBy int64     `gorm:"not null" json:"created_by,string"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
}

func (ProtectionPolicyRevision) TableName() string {
	return "security.protection_policy_revisions"
}

type CreateProtectionPolicyRequest struct {
	AssessmentID  string `json:"assessment_id" binding:"required"`
	ConsumerOwner string `json:"consumer_owner" binding:"required"`
	Action        string `json:"action" binding:"required"`
	Effect        string `json:"effect" binding:"required"`
	Rationale     string `json:"rationale" binding:"required"`
}

type UpdateProtectionPolicyRequest struct {
	Version   int64  `json:"version,string" binding:"required"`
	Effect    string `json:"effect" binding:"required"`
	Rationale string `json:"rationale" binding:"required"`
}

type RevokeProtectionPolicyRequest struct {
	Version   int64  `json:"version,string" binding:"required"`
	Rationale string `json:"rationale" binding:"required"`
}

type ProtectionPolicyResponse struct {
	ProtectionPolicy
	Current ProtectionPolicyRevision   `json:"current"`
	History []ProtectionPolicyRevision `json:"history,omitempty"`
}

type ProtectionPolicyListResponse struct {
	Data       []ProtectionPolicyResponse `json:"data"`
	Total      int64                      `json:"total"`
	Page       int                        `json:"page"`
	PageSize   int                        `json:"page_size"`
	TotalPages int                        `json:"total_pages"`
}
