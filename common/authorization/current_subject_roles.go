package authorization

import (
	"errors"
	"strings"
	"time"
)

// CurrentSubjectRoles is an observation of this request's own Tenant User.
// It is neither a permission grant nor an asynchronous execution credential.
type CurrentSubjectRoles struct {
	TenantID             string    `json:"tenant_id"`
	ActorPrincipalID     string    `json:"actor_principal_id"`
	TenantMembershipID   string    `json:"tenant_membership_id"`
	AuthorizationVersion string    `json:"authorization_version"`
	RoleKeys             []string  `json:"role_keys"`
	ObservedAt           time.Time `json:"observed_at"`
	ExpiresAt            time.Time `json:"expires_at"`
}

func (roles *CurrentSubjectRoles) ValidateFor(current *AuthContext, now time.Time) error {
	invalid := errors.New("current subject role observation is invalid or stale")
	if roles == nil || current == nil || ValidateAuthContext(*current) != nil || current.Principal.Type != "user" || current.Context.Type != "tenant" || current.Token.Type != "first_party_access_token" || current.Delegation != nil || !current.Token.ExpiresAt.After(now) {
		return invalid
	}
	if roles.TenantID != *current.Context.TenantID || roles.ActorPrincipalID != current.Principal.ID || roles.TenantMembershipID != *current.Context.TenantMembershipID || roles.AuthorizationVersion != current.Authorization.AuthorizationVersion || roles.RoleKeys == nil || roles.ObservedAt.IsZero() || roles.ObservedAt.After(now.Add(time.Minute)) || !roles.ExpiresAt.After(now) || !roles.ExpiresAt.After(roles.ObservedAt) || roles.ExpiresAt.After(current.Token.ExpiresAt) {
		return invalid
	}
	for i, key := range roles.RoleKeys {
		if key != strings.TrimSpace(key) || !authContextRoleKeyPattern.MatchString(key) || i > 0 && key <= roles.RoleKeys[i-1] {
			return invalid
		}
	}
	return nil
}
