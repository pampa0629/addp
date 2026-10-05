package main

import (
	"context"
	"fmt"

	"github.com/addp/system/internal/iam"
)

// Only the disposable Online fixture may create these separate management identities.
// The raster consumer retains its exact minimal scene permissions.
func rasterPolicyIdentities(ctx context.Context, repository *iam.Repository, identity *iam.IdentityService, selection *iam.ContextSelectionService, tenantAdministratorID int64) (map[string]string, error) {
	tenant, err := issueRasterPolicySession(ctx, repository, selection, tenantAdministratorID, iam.ContextTypeTenant, iam.SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: iam.AssuranceLevelAAL1}, "raster-policy-tenant-session")
	if err != nil {
		return nil, err
	}
	user, err := createUser(ctx, identity, "external-online-raster-platform")
	if err != nil {
		return nil, err
	}
	now, err := repository.CurrentDatabaseTime(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = repository.CreateBootstrapRoleAssignment(ctx, user.PrincipalID, "platform.system_administrator", "Disposable raster resource policy management", now); err != nil {
		return nil, err
	}
	platform, err := issueRasterPolicySession(ctx, repository, selection, user.PrincipalID, iam.ContextTypePlatform, iam.SessionAuthentication{Methods: []string{"password", "totp"}, AssuranceLevel: iam.AssuranceLevelAAL2}, "raster-policy-platform-session")
	if err != nil {
		return nil, err
	}
	return map[string]string{"ADDP_ONLINE_RASTER_POLICY_PLATFORM_TOKEN": platform.AccessToken, "ADDP_ONLINE_RASTER_POLICY_TENANT_TOKEN": tenant.AccessToken}, nil
}

// Use the same authority clock as IAM instead of the fixture host's clock.
func issueRasterPolicySession(ctx context.Context, repository *iam.Repository, selection *iam.ContextSelectionService, principalID int64, expectedContext iam.ContextType, authentication iam.SessionAuthentication, requestID string) (*iam.IssuedBrowserSession, error) {
	now, err := repository.CurrentDatabaseTime(ctx)
	if err != nil {
		return nil, err
	}
	authentication.AuthenticatedAt = now
	result, err := selection.BeginContextSelection(ctx, iam.BeginContextSelectionInput{PrincipalID: principalID, Authentication: authentication, Audit: audit(requestID)})
	if err != nil {
		return nil, err
	}
	if result.NextAction != iam.ContextSelectionNextActionSessionIssued || result.Session == nil || result.Session.Context.Type != expectedContext {
		return nil, fmt.Errorf("raster policy fixture did not resolve to one %s Context", expectedContext)
	}
	return result.Session, nil
}
