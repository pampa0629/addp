package execution

import "time"

// SubjectRoles is a non-cacheable observation bound to one running attempt.
// It grants neither data access nor access to unmasked values.
type SubjectRoles struct {
	AuthorizationID      string    `json:"authorization_id"`
	ExecutionID          string    `json:"execution_id"`
	Attempt              int       `json:"attempt"`
	Owner                string    `json:"owner"`
	TenantID             string    `json:"tenant_id"`
	ActorPrincipalID     string    `json:"actor_principal_id"`
	TenantMembershipID   string    `json:"tenant_membership_id"`
	AuthorizationVersion string    `json:"authorization_version"`
	RoleKeys             []string  `json:"role_keys"`
	ObservedAt           time.Time `json:"observed_at"`
	ExpiresAt            time.Time `json:"expires_at"`
}
