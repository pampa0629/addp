package models

import "time"

const (
	ProtectionExemptionStateActive     = "active"
	ProtectionExemptionStateRevoked    = "revoked"
	ProtectionExemptionStateExpired    = "expired"
	ProtectionExemptionStateSuperseded = "superseded"
)

// ProtectionExemption is the mutable pointer for one time-bounded plaintext
// exception at a concrete Assessment and owner action.
type ProtectionExemption struct {
	ID              string    `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID        int64     `gorm:"not null;index;uniqueIndex:uq_security_exemption_binding" json:"-"`
	AssessmentID    string    `gorm:"type:uuid;not null;index;uniqueIndex:uq_security_exemption_binding" json:"assessment_id"`
	ConsumerOwner   string    `gorm:"size:32;not null;uniqueIndex:uq_security_exemption_binding" json:"consumer_owner"`
	Action          string    `gorm:"size:32;not null;uniqueIndex:uq_security_exemption_binding" json:"action"`
	SubjectType     string    `gorm:"size:16;not null;uniqueIndex:uq_security_exemption_binding" json:"subject_type"`
	SubjectID       string    `gorm:"size:64;not null;uniqueIndex:uq_security_exemption_binding" json:"subject_id"`
	State           string    `gorm:"size:16;not null" json:"state"`
	Version         int64     `gorm:"not null;default:1" json:"version,string"`
	CurrentRevision int64     `gorm:"not null" json:"current_revision"`
	CreatedBy       int64     `gorm:"not null" json:"created_by,string"`
	CreatedAt       time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt       time.Time `gorm:"not null" json:"updated_at"`
}

func (ProtectionExemption) TableName() string { return "security.protection_exemptions" }

// ProtectionExemptionRevision is an immutable grant, renewal, or revocation.
type ProtectionExemptionRevision struct {
	ID                 string    `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID           int64     `gorm:"not null;index" json:"-"`
	ExemptionID        string    `gorm:"type:uuid;not null;index;uniqueIndex:uq_security_exemption_revision" json:"exemption_id"`
	Revision           int64     `gorm:"not null;uniqueIndex:uq_security_exemption_revision" json:"revision"`
	AssessmentRevision int64     `gorm:"not null" json:"assessment_revision"`
	SourceRequestID    string    `gorm:"type:uuid;not null;index" json:"source_request_id"`
	State              string    `gorm:"size:16;not null" json:"state"`
	ExpiresAt          time.Time `gorm:"not null" json:"expires_at"`
	Rationale          string    `gorm:"type:text;not null" json:"rationale"`
	CreatedBy          int64     `gorm:"not null" json:"created_by,string"`
	CreatedAt          time.Time `gorm:"not null" json:"created_at"`
}

func (ProtectionExemptionRevision) TableName() string {
	return "security.protection_exemption_revisions"
}

type RevokeProtectionExemptionRequest struct {
	Version   int64  `json:"version" binding:"required"`
	Rationale string `json:"rationale" binding:"required"`
}

type ProtectionExemptionResponse struct {
	ProtectionExemption
	EffectiveState string                        `json:"effective_state"`
	Current        ProtectionExemptionRevision   `json:"current"`
	History        []ProtectionExemptionRevision `json:"history,omitempty"`
}

type ProtectionExemptionListResponse struct {
	Data       []ProtectionExemptionResponse `json:"data"`
	Total      int64                         `json:"total"`
	Page       int                           `json:"page"`
	PageSize   int                           `json:"page_size"`
	TotalPages int                           `json:"total_pages"`
}
