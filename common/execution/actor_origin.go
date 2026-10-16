package execution

import (
	"context"
	"fmt"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
)

// actorOrigin is only the immutable identity part of an execution. It does not
// retain credentials, permissions, grants, or the complete AuthContext.
type actorOrigin struct {
	tenant                         int
	principal, membership, version int64
}

type actorOriginKey struct{}

// WithUserOrigin accepts only a middleware-verified current User AuthContext.
// Invalid input shadows any earlier context value, so capture fails closed.
func WithUserOrigin(ctx context.Context, facts authorization.AuthContext) context.Context {
	origin := actorOrigin{}
	if facts.Principal.Type == "user" && facts.Context.Type == "tenant" && facts.Context.TenantID != nil && facts.Context.TenantMembershipID != nil {
		switch facts.Token.Type {
		case "first_party_access_token", "oauth_access_token", "delegated_access_token":
			tenant, tenantErr := strconv.Atoi(*facts.Context.TenantID)
			principal, principalErr := strconv.ParseInt(facts.Principal.ID, 10, 64)
			membership, membershipErr := strconv.ParseInt(*facts.Context.TenantMembershipID, 10, 64)
			version, versionErr := strconv.ParseInt(facts.Authorization.AuthorizationVersion, 10, 64)
			if tenantErr == nil && principalErr == nil && membershipErr == nil && versionErr == nil {
				origin = actorOrigin{tenant: tenant, principal: principal, membership: membership, version: version}
			}
		}
	}
	return context.WithValue(ctx, actorOriginKey{}, origin)
}

// ApplyUserOrigin freezes origin on a root execution during the request only.
func ApplyUserOrigin(ctx context.Context, target *TaskExecution) error {
	origin, _ := ctx.Value(actorOriginKey{}).(actorOrigin)
	if target == nil || target.ParentExecutionID != nil || target.TenantID != origin.tenant || origin.tenant <= 0 || origin.principal <= 0 || origin.membership <= 0 || origin.version <= 0 ||
		(target.TriggeredBy != nil && int64(*target.TriggeredBy) != origin.principal) {
		return fmt.Errorf("%w: verified current User origin is required", commonapi.ErrForbidden)
	}
	by := int(origin.principal)
	target.TriggeredBy, target.ActorPrincipalID, target.ActorTenantMembershipID, target.IssuedAuthorizationVersion = &by, &origin.principal, &origin.membership, &origin.version
	return nil
}

// CopyActorOrigin requires complete, consistent, same-tenant origin. The caller
// verifies and locks its domain source; no execution authorization is copied.
func CopyActorOrigin(target, source *TaskExecution) error {
	if target == nil || source == nil || target.TenantID <= 0 || target.TenantID != source.TenantID ||
		source.TriggeredBy == nil || source.ActorPrincipalID == nil || *source.ActorPrincipalID <= 0 || int64(*source.TriggeredBy) != *source.ActorPrincipalID ||
		source.ActorTenantMembershipID == nil || *source.ActorTenantMembershipID <= 0 || source.IssuedAuthorizationVersion == nil || *source.IssuedAuthorizationVersion <= 0 {
		return fmt.Errorf("%w: complete and consistent execution origin is required", commonapi.ErrForbidden)
	}
	by, principal, membership, version := *source.TriggeredBy, *source.ActorPrincipalID, *source.ActorTenantMembershipID, *source.IssuedAuthorizationVersion
	target.TriggeredBy, target.ActorPrincipalID, target.ActorTenantMembershipID, target.IssuedAuthorizationVersion = &by, &principal, &membership, &version
	return nil
}
