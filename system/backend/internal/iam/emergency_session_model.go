package iam

import (
	"time"

	"github.com/google/uuid"
)

const (
	EmergencyApprovalTTL      = 10 * time.Minute
	EmergencyExecutionTTL     = 15 * time.Minute
	emergencyPermissionPrefix = "iam.emergency_session_revocation."
)

// One request is also the identity of its single, precisely bound grant.
// No role, token or transferable secret is issued by this workflow.
type EmergencySessionRequest struct {
	ID                           uuid.UUID                  `gorm:"type:uuid;primaryKey" json:"id"`
	Version                      int64                      `json:"version,string" swaggertype:"string"`
	TenantID                     int64                      `json:"tenant_id,string" swaggertype:"string"`
	ExecutorPrincipalID          int64                      `json:"executor_principal_id,string" swaggertype:"string"`
	ExecutorMembershipID         int64                      `json:"executor_membership_id,string" swaggertype:"string"`
	ExecutorAuthorizationVersion int64                      `json:"-"`
	TargetPrincipalID            int64                      `json:"target_principal_id,string" swaggertype:"string"`
	TargetMembershipID           int64                      `json:"target_membership_id,string" swaggertype:"string"`
	TargetAuthorizationVersion   int64                      `json:"-"`
	TargetFamilyID               int64                      `json:"target_family_id,string" swaggertype:"string"`
	Reason                       string                     `json:"reason"`
	IncidentReference            string                     `json:"incident_reference"`
	Status                       string                     `json:"status"`
	RequestedAt                  time.Time                  `json:"requested_at"`
	ApprovalExpiresAt            time.Time                  `json:"approval_expires_at"`
	ApprovedAt                   *time.Time                 `json:"approved_at"`
	ExecuteBefore                *time.Time                 `json:"execute_before"`
	ExecutionID                  *uuid.UUID                 `gorm:"type:uuid" json:"execution_id"`
	ExecutedAt                   *time.Time                 `json:"executed_at"`
	Outcome                      *string                    `json:"outcome"`
	ClosedByPrincipalID          *int64                     `json:"closed_by_principal_id,omitempty,string" swaggertype:"string"`
	CloseReason                  *string                    `json:"close_reason,omitempty"`
	Approvals                    []EmergencySessionApproval `gorm:"-" json:"approvals"`
	EffectiveState               string                     `gorm:"-" json:"effective_state"`
	TenantName                   string                     `gorm:"-" json:"tenant_name"`
	ExecutorName                 string                     `gorm:"-" json:"executor_name"`
	TargetName                   string                     `gorm:"-" json:"target_name"`
}

func (EmergencySessionRequest) TableName() string { return "system.emergency_session_requests" }

type EmergencySessionApproval struct {
	RequestID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"-"`
	RoleKey              string    `gorm:"primaryKey" json:"role_key"`
	PrincipalID          int64     `json:"principal_id,string" swaggertype:"string"`
	AuthorizationVersion int64     `json:"-"`
	Reason               string    `json:"reason"`
	ApprovedAt           time.Time `json:"approved_at"`
	DisplayName          string    `gorm:"-" json:"display_name"`
}

func (EmergencySessionApproval) TableName() string { return "system.emergency_session_approvals" }

type CreateEmergencySessionInput struct {
	ID                uuid.UUID
	AccessToken       string
	TargetPrincipalID int64
	TargetFamilyID    int64
	Reason            string
	IncidentReference string
	Audit             AuditMetadata
}

type MutateEmergencySessionInput struct {
	ID          uuid.UUID
	Version     int64
	AccessToken string
	Reason      string
	ExecutionID uuid.UUID
	Audit       AuditMetadata
}

func (r *EmergencySessionRequest) effectiveState(now time.Time) string {
	if r.Status == "pending" && !r.ApprovalExpiresAt.After(now) ||
		r.Status == "approved" && (r.ExecuteBefore == nil || !r.ExecuteBefore.After(now)) {
		return "expired"
	}
	return r.Status
}
