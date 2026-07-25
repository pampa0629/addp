package iam

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/lib/pq"
)

const (
	defaultDelegatedAccessTokenTTL = 2 * time.Minute
	maxDelegationBindingLength     = 100
)

var (
	ErrDelegationPermissionDenied = fmt.Errorf("%w: delegation is not permitted", commonapi.ErrForbidden)
	ErrDelegationConflict         = fmt.Errorf("%w: delegation call already exists", commonapi.ErrConflict)
)

type DelegationToolCatalog interface {
	FindToolAuthorization(string) (commonauth.ToolAuthorization, bool)
}

type DelegationServiceConfig struct {
	AccessTokenTTL time.Duration
	Generate       OpaqueTokenGenerator
}

type IssueDelegatedAccessTokenInput struct {
	SourceAccessToken string
	Audience          string
	Scopes            []string
	AgentRunID        string
	ToolCallID        string
	Audit             AuditMetadata
}

type IssuedDelegatedAccessToken struct {
	AccessToken string
	TokenType   string
	ExpiresAt   time.Time
	Audience    string
	Scopes      []string
	AgentRunID  string
	ToolCallID  string
}

type DelegationService struct {
	repository *Repository
	catalog    DelegationToolCatalog
	ttl        time.Duration
	generate   OpaqueTokenGenerator
}

func NewDelegationService(
	repository *Repository,
	catalog DelegationToolCatalog,
	config DelegationServiceConfig,
) (*DelegationService, error) {
	if repository == nil || catalog == nil {
		return nil, fmt.Errorf("%w: delegation repository and Tool catalog are required", commonapi.ErrBadRequest)
	}
	if config.AccessTokenTTL == 0 {
		config.AccessTokenTTL = defaultDelegatedAccessTokenTTL
	}
	if config.AccessTokenTTL <= 0 || config.AccessTokenTTL > defaultDelegatedAccessTokenTTL {
		return nil, fmt.Errorf("%w: delegated access token TTL must be within two minutes", commonapi.ErrBadRequest)
	}
	if config.Generate == nil {
		config.Generate = generateOpaqueToken
	}
	return &DelegationService{
		repository: repository,
		catalog:    catalog,
		ttl:        config.AccessTokenTTL,
		generate:   config.Generate,
	}, nil
}

func (s *DelegationService) IssueDelegatedAccessToken(
	ctx context.Context,
	input IssueDelegatedAccessTokenInput,
) (*IssuedDelegatedAccessToken, error) {
	if s == nil || s.repository == nil || s.catalog == nil || s.generate == nil {
		return nil, fmt.Errorf("%w: delegation service is required", commonapi.ErrBadRequest)
	}
	tool, err := s.validateRequest(input)
	if err != nil {
		return nil, err
	}
	sourceSnapshot, err := resolveDelegationSourceAccessTokenSnapshot(ctx, s.repository, input.SourceAccessToken)
	if err != nil {
		return nil, err
	}
	if err := validateDelegationOAuthScopes(sourceSnapshot, tool.RequiredScopes); err != nil {
		return nil, err
	}

	plainToken, err := s.generate("addp_dat_")
	if err != nil {
		return nil, fmt.Errorf("generate delegated access token: %w", err)
	}
	if !strings.HasPrefix(plainToken, "addp_dat_") || len(plainToken) == len("addp_dat_") {
		return nil, fmt.Errorf("delegated access token generator returned an invalid token")
	}

	var issued *IssuedDelegatedAccessToken
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, sourceSnapshot.FamilyPrincipalID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		family, err := tx.LockRefreshTokenFamily(ctx, sourceSnapshot.CredentialFamilyID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		sourceAccessToken, err := tx.LockAccessToken(ctx, sourceSnapshot.CredentialID)
		if err != nil {
			return hideTokenLookupError(err)
		}

		lockedSnapshot, err := resolveDelegationSourceAccessTokenSnapshot(ctx, tx, input.SourceAccessToken)
		if err != nil {
			return err
		}
		if lockedSnapshot.FamilyPrincipalID != principal.ID ||
			lockedSnapshot.CredentialFamilyID != family.ID ||
			lockedSnapshot.CredentialID != sourceAccessToken.ID ||
			family.PrincipalID != principal.ID || sourceAccessToken.FamilyID != family.ID {
			return commonapi.ErrUnauthorized
		}
		if err := validateDelegationOAuthScopes(lockedSnapshot, tool.RequiredScopes); err != nil {
			return err
		}

		tenantID := lockedSnapshot.TenantID
		tenantMembershipID := lockedSnapshot.TenantMembershipID
		permissionRows, err := tx.ListEffectiveRoleAssignmentPermissions(
			ctx,
			lockedSnapshot.FamilyPrincipalID,
			lockedSnapshot.PrincipalType,
			lockedSnapshot.FamilyContextType,
			tenantID,
			tenantMembershipID,
			lockedSnapshot.DatabaseTime,
		)
		if err != nil {
			return err
		}
		if !containsAllDelegationPermissions(permissionRows, tool.RequiredPermissions) {
			return ErrDelegationPermissionDenied
		}

		now := lockedSnapshot.DatabaseTime.UTC()
		expiresAt := earlierTime(now.Add(s.ttl), sourceAccessToken.ExpiresAt.UTC())
		expiresAt = earlierTime(expiresAt, family.ExpiresAt.UTC())
		if !expiresAt.After(now) {
			return commonapi.ErrUnauthorized
		}
		token := &DelegatedAccessToken{
			TokenHash:           hashOpaqueToken(plainToken),
			SourceAccessTokenID: sourceAccessToken.ID,
			Audience:            tool.Owner,
			Scopes:              pq.StringArray(append([]string(nil), tool.RequiredScopes...)),
			AgentRunID:          input.AgentRunID,
			ToolCallID:          input.ToolCallID,
			ExpiresAt:           expiresAt,
			CreatedAt:           now,
		}
		if err := tx.CreateDelegatedAccessToken(ctx, token); err != nil {
			return err
		}

		auditMetadata := input.Audit
		principalID := principal.ID
		principalType := principal.PrincipalType
		contextType := family.ContextType
		auditMetadata.PrincipalID = &principalID
		auditMetadata.PrincipalType = &principalType
		auditMetadata.ContextType = &contextType
		auditMetadata.TenantID = tenantID
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata:   auditMetadata,
			EventName:  "iam.delegation.issued",
			Result:     AuditResultSucceeded,
			RiskLevel:  AuditRiskMedium,
			ModuleName: "system",
			EntityType: "delegated_access_token",
			EntityID:   strconv.FormatInt(token.ID, 10),
			Details: map[string]any{
				"tool_name":             tool.Name,
				"audience":              tool.Owner,
				"scope_count":           len(tool.RequiredScopes),
				"agent_run_id":          input.AgentRunID,
				"tool_call_id":          input.ToolCallID,
				"expires_at":            expiresAt,
				"authorization_version": principal.AuthorizationVersion,
			},
		}); err != nil {
			return err
		}

		issued = &IssuedDelegatedAccessToken{
			AccessToken: plainToken,
			TokenType:   "Bearer",
			ExpiresAt:   expiresAt,
			Audience:    tool.Owner,
			Scopes:      append([]string(nil), tool.RequiredScopes...),
			AgentRunID:  input.AgentRunID,
			ToolCallID:  input.ToolCallID,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return issued, nil
}

func (s *DelegationService) validateRequest(
	input IssueDelegatedAccessTokenInput,
) (commonauth.ToolAuthorization, error) {
	if !strings.HasPrefix(input.SourceAccessToken, "addp_at_") || len(input.SourceAccessToken) == len("addp_at_") {
		return commonauth.ToolAuthorization{}, commonapi.ErrUnauthorized
	}
	if input.Audience == "" || strings.TrimSpace(input.Audience) != input.Audience ||
		commonauth.ValidateOwnerModuleName(input.Audience) != nil ||
		input.AgentRunID == "" || strings.TrimSpace(input.AgentRunID) != input.AgentRunID ||
		len(input.AgentRunID) > maxDelegationBindingLength ||
		input.ToolCallID == "" || strings.TrimSpace(input.ToolCallID) != input.ToolCallID ||
		len(input.ToolCallID) > maxDelegationBindingLength || len(input.Scopes) != 1 ||
		commonauth.ValidateToolScope(input.Scopes[0]) != nil {
		return commonauth.ToolAuthorization{}, fmt.Errorf("%w: invalid delegation request", commonapi.ErrBadRequest)
	}

	tool, exists := s.catalog.FindToolAuthorization(input.Scopes[0])
	if !exists {
		return commonauth.ToolAuthorization{}, fmt.Errorf("%w: unknown Tool scope", commonapi.ErrBadRequest)
	}
	if tool.Name != input.Scopes[0] || tool.Owner == "" ||
		commonauth.ValidateOwnerModuleName(tool.Owner) != nil ||
		len(tool.RequiredScopes) != 1 || tool.RequiredScopes[0] != tool.Name ||
		len(tool.RequiredPermissions) == 0 {
		return commonauth.ToolAuthorization{}, fmt.Errorf("invalid generated Tool authorization catalog entry %q", input.Scopes[0])
	}
	seenPermissions := make(map[string]struct{}, len(tool.RequiredPermissions))
	for _, permission := range tool.RequiredPermissions {
		if commonauth.ValidatePermissionKey(permission) != nil {
			return commonauth.ToolAuthorization{}, fmt.Errorf("invalid generated Tool authorization catalog entry %q", input.Scopes[0])
		}
		if _, duplicate := seenPermissions[permission]; duplicate {
			return commonauth.ToolAuthorization{}, fmt.Errorf("invalid generated Tool authorization catalog entry %q", input.Scopes[0])
		}
		seenPermissions[permission] = struct{}{}
	}
	if input.Audience != tool.Owner {
		return commonauth.ToolAuthorization{}, fmt.Errorf("%w: Tool audience does not match owner", commonapi.ErrBadRequest)
	}
	return tool, nil
}

func validateDelegationOAuthScopes(snapshot *SessionCredentialAuthSnapshot, scopes []string) error {
	if snapshot.FamilyAuthType != "oauth" || containsCredentialValue(snapshot.FamilyScopes, "addp.api") {
		return nil
	}
	for _, scope := range scopes {
		if !containsCredentialValue(snapshot.FamilyScopes, scope) {
			return ErrDelegationPermissionDenied
		}
	}
	return nil
}

func containsAllDelegationPermissions(
	rows []RoleAssignmentPermissionProjection,
	required []string,
) bool {
	available := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		available[row.PermissionKey] = struct{}{}
	}
	for _, permission := range required {
		if _, exists := available[permission]; !exists {
			return false
		}
	}
	return true
}
