package oauth

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/addp/system/internal/iam"
	"github.com/ory/fosite"
	"gorm.io/gorm"
)

const serviceAccessTokenLifespan = 5 * time.Minute

// ServiceCredentialResponseLifespan projects the remaining lifetime using the
// same clock authority as issuance. The transaction clock is intentionally not
// used here: time spent issuing the token must reduce the response lifetime.
func (s *Storage) ServiceCredentialResponseLifespan(ctx context.Context, requester fosite.AccessRequester) (time.Duration, error) {
	if requester == nil || requester.GetSession() == nil ||
		!requester.GetGrantTypes().ExactOne(string(fosite.GrantTypeClientCredentials)) {
		return 0, fosite.ErrInvalidRequest
	}
	var now time.Time
	db := s.dbFromContext(ctx)
	clock := "clock_timestamp()"
	if db.Dialector.Name() == "mysql" {
		clock = "CURRENT_TIMESTAMP(6)"
	}
	if err := db.Raw("SELECT " + clock).Scan(&now).Error; err != nil {
		return 0, fosite.ErrTemporarilyUnavailable
	}
	remaining := requester.GetSession().GetExpiresAt(fosite.AccessToken).Sub(now)
	if remaining < time.Second || remaining > serviceAccessTokenLifespan {
		return 0, fosite.ErrTemporarilyUnavailable
	}
	return remaining, nil
}

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
	db := s.dbFromContext(ctx)
	issuedAt := issuanceTimeExpression(db)
	// Grant validity uses the transaction clock independently of the issuance projection.
	eligibilityTime := "now()"
	serviceGrant := "oauth_client.grant_types = ARRAY['client_credentials']::text[]"
	serviceRole := "'service_principal' = ANY(role.allowed_principal_types)"
	if db.Dialector.Name() == "mysql" {
		eligibilityTime = issuedAt
		serviceGrant = "oauth_client.grant_types = JSON_ARRAY('client_credentials')"
		serviceRole = `JSON_CONTAINS(role.allowed_principal_types, '"service_principal"')`
	}
	switch {
	case contextTypeValue == "" && tenantIDValue != "":
		tenantID, err := strconv.ParseInt(tenantIDValue, 10, 64)
		if err != nil || tenantID <= 0 {
			return fosite.ErrInvalidRequest
		}
		result = db.Raw(fmt.Sprintf(`
		SELECT principal.id AS principal_id,
		       principal.authorization_version,
		       membership.id AS tenant_membership_id,
		       membership.tenant_id,
		       %[1]s AS database_time
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
		 AND (membership.expires_at IS NULL OR membership.expires_at > %[3]s)
		JOIN system.tenants tenant
		  ON tenant.id = membership.tenant_id
		 AND tenant.status = 'active'
		WHERE oauth_client.client_id = ?
		  AND oauth_client.status = 'active'
		  AND %[2]s
	`, issuedAt, serviceGrant, eligibilityTime), tenantID, requester.GetClient().GetID()).Scan(&row)
	case contextTypeValue == string(iam.ContextTypePlatform) && tenantIDValue == "":
		contextType = iam.ContextTypePlatform
		result = db.Raw(fmt.Sprintf(`
		SELECT principal.id AS principal_id,
		       principal.authorization_version,
		       NULL AS tenant_membership_id,
		       NULL AS tenant_id,
		       %[1]s AS database_time
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
		  AND %[2]s
		  AND EXISTS (
		      SELECT 1
		      FROM system.role_assignments assignment
		      JOIN system.roles role ON role.id = assignment.role_id
		      WHERE assignment.principal_id = principal.id
		        AND assignment.scope_type = 'platform'
		        AND assignment.status = 'active'
		        AND assignment.valid_from <= %[3]s
		        AND (assignment.valid_until IS NULL OR assignment.valid_until > %[3]s)
		        AND role.role_type = 'platform_builtin'
		        AND role.status = 'active'
		        AND %[4]s
		  )
	`, issuedAt, serviceGrant, eligibilityTime, serviceRole), requester.GetClient().GetID()).Scan(&row)
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
