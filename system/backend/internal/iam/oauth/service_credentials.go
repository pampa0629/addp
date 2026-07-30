package oauth

import (
	"context"
	"strconv"
	"time"

	"github.com/addp/system/internal/iam"
	"github.com/ory/fosite"
	"gorm.io/gorm"
)

const serviceAccessTokenLifespan = 5 * time.Minute

type serviceCredentialSessionRow struct {
	PrincipalID          int64     `gorm:"column:principal_id"`
	AuthorizationVersion int64     `gorm:"column:authorization_version"`
	TenantMembershipID   *int64    `gorm:"column:tenant_membership_id"`
	TenantID             *int64    `gorm:"column:tenant_id"`
	DatabaseTime         time.Time `gorm:"column:database_time"`
}

func (s *Storage) PopulateServiceCredentialSession(
	ctx context.Context,
	requester fosite.AccessRequester,
	contextTypeValue string,
	tenantIDValue string,
) error {
	if requester == nil || !requester.GetGrantTypes().ExactOne(string(fosite.GrantTypeClientCredentials)) {
		return fosite.ErrInvalidRequest
	}
	if len(requester.GetRequestedScopes()) != 1 || requester.GetRequestedScopes()[0] != "addp.api" ||
		len(requester.GetRequestedAudience()) != 1 || requester.GetRequestedAudience()[0] != "addp.api" {
		return fosite.ErrInvalidScope
	}

	contextType := iam.ContextTypeTenant
	var row serviceCredentialSessionRow
	var result *gorm.DB
	switch {
	case contextTypeValue == "" && tenantIDValue != "":
		tenantID, err := strconv.ParseInt(tenantIDValue, 10, 64)
		if err != nil || tenantID <= 0 {
			return fosite.ErrInvalidRequest
		}
		result = s.dbFromContext(ctx).Raw(`
		SELECT principal.id AS principal_id,
		       principal.authorization_version,
		       membership.id AS tenant_membership_id,
		       membership.tenant_id,
		       transaction_timestamp() AS database_time
		FROM system.oauth_clients oauth_client
		JOIN system.principals principal
		  ON principal.id = oauth_client.service_principal_id
		 AND principal.principal_type = 'service_principal'
		 AND principal.status = 'active'
		JOIN system.service_principals service_principal ON service_principal.id = principal.id
		JOIN system.tenant_memberships membership
		  ON membership.principal_id = principal.id
		 AND membership.tenant_id = ?
		 AND membership.status = 'active'
		 AND (membership.expires_at IS NULL OR membership.expires_at > now())
		JOIN system.tenants tenant
		  ON tenant.id = membership.tenant_id
		 AND tenant.status = 'active'
		WHERE oauth_client.client_id = ?
		  AND oauth_client.status = 'active'
		  AND oauth_client.grant_types = ARRAY['client_credentials']::text[]
	`, tenantID, requester.GetClient().GetID()).Scan(&row)
	case contextTypeValue == string(iam.ContextTypePlatform) && tenantIDValue == "":
		contextType = iam.ContextTypePlatform
		result = s.dbFromContext(ctx).Raw(`
		SELECT principal.id AS principal_id,
		       principal.authorization_version,
		       NULL::bigint AS tenant_membership_id,
		       NULL::bigint AS tenant_id,
		       transaction_timestamp() AS database_time
		FROM system.oauth_clients oauth_client
		JOIN system.principals principal
		  ON principal.id = oauth_client.service_principal_id
		 AND principal.principal_type = 'service_principal'
		 AND principal.status = 'active'
		JOIN system.service_principals service_principal
		  ON service_principal.id = principal.id
		 AND service_principal.owner_scope = 'platform'
		WHERE oauth_client.client_id = ?
		  AND oauth_client.status = 'active'
		  AND oauth_client.grant_types = ARRAY['client_credentials']::text[]
		  AND EXISTS (
		      SELECT 1
		      FROM system.role_assignments assignment
		      JOIN system.roles role ON role.id = assignment.role_id
		      WHERE assignment.principal_id = principal.id
		        AND assignment.scope_type = 'platform'
		        AND assignment.status = 'active'
		        AND assignment.valid_from <= now()
		        AND (assignment.valid_until IS NULL OR assignment.valid_until > now())
		        AND role.role_type = 'platform_builtin'
		        AND role.status = 'active'
		        AND 'service_principal' = ANY(role.allowed_principal_types)
		  )
	`, requester.GetClient().GetID()).Scan(&row)
	default:
		return fosite.ErrInvalidRequest
	}
	if result.Error != nil || result.RowsAffected != 1 {
		return fosite.ErrInvalidGrant
	}

	session, ok := requester.GetSession().(*IAMSession)
	if !ok {
		return fosite.ErrInvalidRequest
	}
	session.PrincipalID = row.PrincipalID
	session.Subject = strconv.FormatInt(row.PrincipalID, 10)
	session.ContextType = string(contextType)
	session.TenantMembershipID = row.TenantMembershipID
	session.IssuedAuthorizationVersion = row.AuthorizationVersion
	session.AuthenticationMethods = []string{"service_secret"}
	session.AssuranceLevel = string(iam.AssuranceLevelNotApplicable)
	session.AuthenticatedAt = row.DatabaseTime.UTC()
	session.RequestedAt = row.DatabaseTime.UTC()
	session.SetExpiresAt(fosite.AccessToken, row.DatabaseTime.UTC().Add(serviceAccessTokenLifespan))
	for _, scope := range requester.GetRequestedScopes() {
		requester.GrantScope(scope)
	}
	for _, audience := range requester.GetRequestedAudience() {
		requester.GrantAudience(audience)
	}

	principalID := row.PrincipalID
	principalType := iam.PrincipalTypeServicePrincipal
	updateTransactionAudit(ctx, func(event *iam.AuditEvent) {
		event.Metadata.PrincipalID = &principalID
		event.Metadata.PrincipalType = &principalType
		event.Metadata.ContextType = &contextType
		event.Metadata.TenantID = row.TenantID
	})
	return nil
}
