package iam

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
)

type CredentialInvalidReason string

const (
	CredentialInvalidFormat               CredentialInvalidReason = "invalid_format"
	CredentialInvalidNotFound             CredentialInvalidReason = "not_found"
	CredentialInvalidTokenExpired         CredentialInvalidReason = "token_expired"
	CredentialInvalidTokenRevoked         CredentialInvalidReason = "token_revoked"
	CredentialInvalidSourceTokenExpired   CredentialInvalidReason = "source_token_expired"
	CredentialInvalidSourceTokenRevoked   CredentialInvalidReason = "source_token_revoked"
	CredentialInvalidFamilyExpired        CredentialInvalidReason = "family_expired"
	CredentialInvalidFamilyRevoked        CredentialInvalidReason = "family_revoked"
	CredentialInvalidPrincipalInactive    CredentialInvalidReason = "principal_inactive"
	CredentialInvalidAuthorizationVersion CredentialInvalidReason = "authorization_version_mismatch"
	CredentialInvalidContext              CredentialInvalidReason = "context_invalid"
	CredentialInvalidMembershipInactive   CredentialInvalidReason = "tenant_membership_inactive"
	CredentialInvalidTenantInactive       CredentialInvalidReason = "tenant_inactive"
	CredentialInvalidUnsupportedTokenType CredentialInvalidReason = "unsupported_token_type"
)

type CredentialValidationError struct {
	Reason CredentialInvalidReason
}

func (e *CredentialValidationError) Error() string {
	return commonapi.ErrUnauthorized.Error()
}

func (e *CredentialValidationError) Unwrap() error {
	return commonapi.ErrUnauthorized
}

type AuthContextService struct {
	repository *Repository
}

func NewAuthContextService(repository *Repository) (*AuthContextService, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	return &AuthContextService{repository: repository}, nil
}

func (s *AuthContextService) ResolveAuthContext(
	ctx context.Context,
	credential string,
) (*commonauth.AuthContext, error) {
	switch {
	case strings.HasPrefix(credential, "addp_at_"):
		return s.ResolveFirstPartyAccessToken(ctx, credential)
	case strings.HasPrefix(credential, "addp_rat_"):
		return s.ResolveResourceAccessTicket(ctx, credential)
	case strings.HasPrefix(credential, "addp_dat_"):
		return s.ResolveDelegatedAccessToken(ctx, credential)
	default:
		return nil, invalidCredential(CredentialInvalidFormat)
	}
}

func (s *AuthContextService) ResolveFirstPartyAccessToken(
	ctx context.Context,
	accessToken string,
) (*commonauth.AuthContext, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: AuthContext service is required", commonapi.ErrBadRequest)
	}

	return s.resolveSessionCredential(ctx, func(tx *Repository) (*SessionCredentialAuthSnapshot, error) {
		return resolveFirstPartyAccessTokenSnapshot(ctx, tx, accessToken)
	}, func(snapshot *SessionCredentialAuthSnapshot) credentialProjection {
		clientID := snapshot.FamilyClientID
		return credentialProjection{
			TokenType: "first_party_access_token",
			ClientID:  clientID,
			Audiences: append(make([]string, 0, len(snapshot.FamilyAudiences)), snapshot.FamilyAudiences...),
			ScopeMode: "unrestricted",
			Scopes:    append(make([]string, 0, len(snapshot.FamilyScopes)), snapshot.FamilyScopes...),
		}
	})
}

func (s *AuthContextService) ResolveResourceAccessTicket(
	ctx context.Context,
	resourceAccessTicket string,
) (*commonauth.AuthContext, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: AuthContext service is required", commonapi.ErrBadRequest)
	}
	return s.resolveSessionCredential(ctx, func(tx *Repository) (*SessionCredentialAuthSnapshot, error) {
		return resolveResourceAccessTicketSnapshot(ctx, tx, resourceAccessTicket)
	}, func(snapshot *SessionCredentialAuthSnapshot) credentialProjection {
		clientID := "addp-web"
		return credentialProjection{
			TokenType: "resource_access_ticket",
			ClientID:  clientID,
			Audiences: []string{*snapshot.CredentialOwner},
			ScopeMode: "restricted",
			Scopes:    []string{commonauth.BrowserResourceAccessScope},
		}
	})
}

func (s *AuthContextService) ResolveDelegatedAccessToken(
	ctx context.Context,
	delegatedAccessToken string,
) (*commonauth.AuthContext, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: AuthContext service is required", commonapi.ErrBadRequest)
	}
	return s.resolveSessionCredential(ctx, func(tx *Repository) (*SessionCredentialAuthSnapshot, error) {
		return resolveDelegatedAccessTokenSnapshot(ctx, tx, delegatedAccessToken)
	}, func(snapshot *SessionCredentialAuthSnapshot) credentialProjection {
		clientID := snapshot.FamilyClientID
		return credentialProjection{
			TokenType: "delegated_access_token",
			ClientID:  clientID,
			Audiences: []string{*snapshot.CredentialOwner},
			ScopeMode: "restricted",
			Scopes:    append(make([]string, 0, len(snapshot.CredentialScopes)), snapshot.CredentialScopes...),
			Delegation: &commonauth.DelegationFacts{
				DelegatedByClientID: clientID,
				AgentRunID:          *snapshot.CredentialAgentRunID,
				ToolCallID:          *snapshot.CredentialToolCallID,
			},
		}
	})
}

type credentialProjection struct {
	TokenType  string
	ClientID   string
	Audiences  []string
	ScopeMode  string
	Scopes     []string
	Delegation *commonauth.DelegationFacts
}

func (s *AuthContextService) resolveSessionCredential(
	ctx context.Context,
	loadSnapshot func(*Repository) (*SessionCredentialAuthSnapshot, error),
	projectCredential func(*SessionCredentialAuthSnapshot) credentialProjection,
) (*commonauth.AuthContext, error) {
	var authContext *commonauth.AuthContext
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		snapshot, err := loadSnapshot(tx)
		if err != nil {
			return err
		}

		organization := commonauth.OrganizationContext{
			Departments:   make([]commonauth.DepartmentMembership, 0),
			ProjectGroups: make([]commonauth.ProjectGroupMembership, 0),
		}
		var tenantID *int64
		var tenantMembershipID *int64
		if snapshot.FamilyContextType == ContextTypeTenant {
			tenantID = snapshot.TenantID
			tenantMembershipID = snapshot.TenantMembershipID
			departments, err := tx.ListEffectiveDepartmentMemberships(
				ctx,
				*tenantMembershipID,
				*tenantID,
			)
			if err != nil {
				return err
			}
			organization.Departments = make([]commonauth.DepartmentMembership, 0, len(departments))
			for _, membership := range departments {
				ancestorIDs := make([]string, 0, len(membership.AncestorIDs))
				for _, ancestorID := range membership.AncestorIDs {
					ancestorIDs = append(ancestorIDs, formatIAMID(ancestorID))
				}
				organization.Departments = append(organization.Departments, commonauth.DepartmentMembership{
					MembershipID:   formatIAMID(membership.MembershipID),
					DepartmentID:   formatIAMID(membership.DepartmentID),
					MembershipType: membership.MembershipType,
					RelationRole:   membership.RelationRole,
					AncestorIDs:    ancestorIDs,
				})
			}

			projectGroups, err := tx.ListEffectiveProjectGroupMemberships(
				ctx,
				*tenantMembershipID,
				*tenantID,
			)
			if err != nil {
				return err
			}
			organization.ProjectGroups = make([]commonauth.ProjectGroupMembership, 0, len(projectGroups))
			for _, membership := range projectGroups {
				organization.ProjectGroups = append(organization.ProjectGroups, commonauth.ProjectGroupMembership{
					MembershipID:   formatIAMID(membership.MembershipID),
					ProjectGroupID: formatIAMID(membership.ProjectGroupID),
					RelationRole:   membership.RelationRole,
				})
			}
		}

		permissionRows, err := tx.ListEffectiveRoleAssignmentPermissions(
			ctx,
			snapshot.FamilyPrincipalID,
			snapshot.PrincipalType,
			snapshot.FamilyContextType,
			tenantID,
			tenantMembershipID,
			snapshot.DatabaseTime,
		)
		if err != nil {
			return err
		}
		assignments, err := buildRoleAssignments(permissionRows)
		if err != nil {
			return err
		}

		projection := projectCredential(snapshot)
		clientID := projection.ClientID
		methods := append(make([]string, 0, len(snapshot.FamilyAuthenticationMethods)), snapshot.FamilyAuthenticationMethods...)
		audiences := append(make([]string, 0, len(projection.Audiences)), projection.Audiences...)
		scopes := append(make([]string, 0, len(projection.Scopes)), projection.Scopes...)
		sort.Strings(methods)
		sort.Strings(audiences)
		sort.Strings(scopes)

		resolvedContext := commonauth.AuthSessionContext{Type: string(snapshot.FamilyContextType)}
		if snapshot.FamilyContextType == ContextTypeTenant {
			formattedTenantID := formatIAMID(*snapshot.TenantID)
			formattedMembershipID := formatIAMID(*snapshot.TenantMembershipID)
			resolvedContext.TenantID = &formattedTenantID
			resolvedContext.TenantMembershipID = &formattedMembershipID
		}
		candidate := &commonauth.AuthContext{
			SchemaVersion: commonauth.AuthContextSchemaVersion,
			Principal: commonauth.AuthPrincipal{
				Type: string(snapshot.PrincipalType),
				ID:   formatIAMID(snapshot.FamilyPrincipalID),
			},
			Context: resolvedContext,
			Authentication: commonauth.AuthenticationFacts{
				Methods:         methods,
				AssuranceLevel:  string(snapshot.FamilyAssuranceLevel),
				AuthenticatedAt: snapshot.FamilyAuthenticatedAt.UTC(),
				StepUpExpiresAt: utcTimePointer(snapshot.FamilyStepUpExpiresAt),
			},
			Client: commonauth.ClientConstraints{
				ClientID:  &clientID,
				Audiences: audiences,
				ScopeMode: projection.ScopeMode,
				Scopes:    scopes,
			},
			Organization: organization,
			Authorization: commonauth.AuthorizationFacts{
				AuthorizationVersion: formatIAMID(snapshot.PrincipalAuthorizationVersion),
				RoleAssignments:      assignments,
			},
			Token: commonauth.TokenFacts{
				Type:      projection.TokenType,
				IssuedAt:  snapshot.CredentialCreatedAt.UTC(),
				ExpiresAt: snapshot.CredentialExpiresAt.UTC(),
			},
			Delegation: projection.Delegation,
		}
		if err := commonauth.ValidateAuthContext(*candidate); err != nil {
			return fmt.Errorf("project %s AuthContext: %w", projection.TokenType, err)
		}
		authContext = candidate
		return nil
	})
	if err != nil {
		return nil, err
	}
	return authContext, nil
}

func resolveFirstPartyAccessTokenSnapshot(
	ctx context.Context,
	repository *Repository,
	accessToken string,
) (*SessionCredentialAuthSnapshot, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if !strings.HasPrefix(accessToken, "addp_at_") || len(accessToken) == len("addp_at_") {
		return nil, invalidCredential(CredentialInvalidFormat)
	}
	snapshot, err := repository.GetAccessTokenAuthSnapshot(ctx, hashOpaqueToken(accessToken))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			return nil, invalidCredential(CredentialInvalidNotFound)
		}
		return nil, err
	}
	if err := validateFirstPartyAccessTokenSnapshot(snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func resolveDelegationSourceAccessTokenSnapshot(
	ctx context.Context,
	repository *Repository,
	accessToken string,
) (*SessionCredentialAuthSnapshot, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if !strings.HasPrefix(accessToken, "addp_at_") || len(accessToken) == len("addp_at_") {
		return nil, invalidCredential(CredentialInvalidFormat)
	}
	snapshot, err := repository.GetAccessTokenAuthSnapshot(ctx, hashOpaqueToken(accessToken))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			return nil, invalidCredential(CredentialInvalidNotFound)
		}
		return nil, err
	}
	if err := validateDelegationSourceAccessTokenSnapshot(snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func resolveResourceAccessTicketSnapshot(
	ctx context.Context,
	repository *Repository,
	resourceAccessTicket string,
) (*SessionCredentialAuthSnapshot, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if !strings.HasPrefix(resourceAccessTicket, "addp_rat_") || len(resourceAccessTicket) == len("addp_rat_") {
		return nil, invalidCredential(CredentialInvalidFormat)
	}
	snapshot, err := repository.GetResourceAccessTicketAuthSnapshot(ctx, hashOpaqueToken(resourceAccessTicket))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			return nil, invalidCredential(CredentialInvalidNotFound)
		}
		return nil, err
	}
	if err := validateResourceAccessTicketSnapshot(snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func resolveDelegatedAccessTokenSnapshot(
	ctx context.Context,
	repository *Repository,
	delegatedAccessToken string,
) (*SessionCredentialAuthSnapshot, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if !strings.HasPrefix(delegatedAccessToken, "addp_dat_") || len(delegatedAccessToken) == len("addp_dat_") {
		return nil, invalidCredential(CredentialInvalidFormat)
	}
	snapshot, err := repository.GetDelegatedAccessTokenAuthSnapshot(ctx, hashOpaqueToken(delegatedAccessToken))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			return nil, invalidCredential(CredentialInvalidNotFound)
		}
		return nil, err
	}
	if err := validateDelegatedAccessTokenSnapshot(snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func validateFirstPartyAccessTokenSnapshot(snapshot *SessionCredentialAuthSnapshot) error {
	if err := validateFirstPartyBrowserCredentialSnapshot(snapshot); err != nil {
		return err
	}
	if snapshot.CredentialOwner != nil || len(snapshot.FamilyAudiences) != 1 ||
		snapshot.FamilyAudiences[0] != "addp.api" || len(snapshot.FamilyScopes) != 0 {
		return invalidCredential(CredentialInvalidContext)
	}
	return nil
}

func validateDelegationSourceAccessTokenSnapshot(snapshot *SessionCredentialAuthSnapshot) error {
	if err := validateSessionCredentialSnapshot(snapshot); err != nil {
		return err
	}
	if snapshot.PrincipalType != PrincipalTypeUser || snapshot.CredentialOwner != nil ||
		strings.TrimSpace(snapshot.FamilyClientID) != snapshot.FamilyClientID || snapshot.FamilyClientID == "" ||
		len(snapshot.FamilyAudiences) != 1 || snapshot.FamilyAudiences[0] != "addp.api" {
		return invalidCredential(CredentialInvalidUnsupportedTokenType)
	}
	switch snapshot.FamilyAuthType {
	case "first_party":
		if snapshot.FamilyClientID != "addp-web" || len(snapshot.FamilyScopes) != 0 {
			return invalidCredential(CredentialInvalidUnsupportedTokenType)
		}
	case "oauth":
		if snapshot.FamilyClientID == "addp-web" || len(snapshot.FamilyScopes) == 0 {
			return invalidCredential(CredentialInvalidUnsupportedTokenType)
		}
	default:
		return invalidCredential(CredentialInvalidUnsupportedTokenType)
	}
	return nil
}

func validateResourceAccessTicketSnapshot(snapshot *SessionCredentialAuthSnapshot) error {
	if err := validateFirstPartyBrowserCredentialSnapshot(snapshot); err != nil {
		return err
	}
	if snapshot.CredentialOwner == nil || commonauth.ValidateOwnerModuleName(*snapshot.CredentialOwner) != nil ||
		len(snapshot.FamilyAudiences) != 1 || snapshot.FamilyAudiences[0] != "addp.api" ||
		len(snapshot.FamilyScopes) != 0 {
		return invalidCredential(CredentialInvalidContext)
	}
	return nil
}

func validateFirstPartyBrowserCredentialSnapshot(snapshot *SessionCredentialAuthSnapshot) error {
	if err := validateSessionCredentialSnapshot(snapshot); err != nil {
		return err
	}
	if snapshot.PrincipalType != PrincipalTypeUser || snapshot.FamilyAuthType != "first_party" ||
		snapshot.FamilyClientID != "addp-web" {
		return invalidCredential(CredentialInvalidUnsupportedTokenType)
	}
	return nil
}

func validateDelegatedAccessTokenSnapshot(snapshot *SessionCredentialAuthSnapshot) error {
	if err := validateSessionCredentialSnapshot(snapshot); err != nil {
		return err
	}
	if snapshot.PrincipalType != PrincipalTypeUser || snapshot.CredentialOwner == nil ||
		commonauth.ValidateOwnerModuleName(*snapshot.CredentialOwner) != nil ||
		strings.TrimSpace(snapshot.FamilyClientID) != snapshot.FamilyClientID || snapshot.FamilyClientID == "" ||
		snapshot.CredentialAgentRunID == nil || strings.TrimSpace(*snapshot.CredentialAgentRunID) != *snapshot.CredentialAgentRunID ||
		*snapshot.CredentialAgentRunID == "" || snapshot.CredentialToolCallID == nil ||
		strings.TrimSpace(*snapshot.CredentialToolCallID) != *snapshot.CredentialToolCallID ||
		*snapshot.CredentialToolCallID == "" || len(snapshot.CredentialScopes) == 0 {
		return invalidCredential(CredentialInvalidContext)
	}
	seenScopes := make(map[string]struct{}, len(snapshot.CredentialScopes))
	for _, scope := range snapshot.CredentialScopes {
		if commonauth.ValidateToolScope(scope) != nil {
			return invalidCredential(CredentialInvalidContext)
		}
		if _, exists := seenScopes[scope]; exists {
			return invalidCredential(CredentialInvalidContext)
		}
		seenScopes[scope] = struct{}{}
	}
	if snapshot.SourceAccessTokenID == nil || snapshot.SourceAccessTokenExpiresAt == nil ||
		snapshot.SourceAccessTokenCreatedAt == nil ||
		snapshot.SourceAccessTokenCreatedAt.After(snapshot.CredentialCreatedAt) ||
		snapshot.CredentialExpiresAt.After(*snapshot.SourceAccessTokenExpiresAt) {
		return invalidCredential(CredentialInvalidContext)
	}
	if snapshot.SourceAccessTokenRevokedAt != nil {
		return invalidCredential(CredentialInvalidSourceTokenRevoked)
	}
	if !snapshot.SourceAccessTokenExpiresAt.After(snapshot.DatabaseTime) {
		return invalidCredential(CredentialInvalidSourceTokenExpired)
	}

	switch snapshot.FamilyAuthType {
	case "first_party":
		if snapshot.FamilyClientID != "addp-web" || len(snapshot.FamilyAudiences) != 1 ||
			snapshot.FamilyAudiences[0] != "addp.api" || len(snapshot.FamilyScopes) != 0 {
			return invalidCredential(CredentialInvalidUnsupportedTokenType)
		}
	case "oauth":
		if snapshot.FamilyClientID == "addp-web" || len(snapshot.FamilyAudiences) != 1 ||
			snapshot.FamilyAudiences[0] != "addp.api" || len(snapshot.FamilyScopes) == 0 {
			return invalidCredential(CredentialInvalidUnsupportedTokenType)
		}
		if !containsCredentialValue(snapshot.FamilyScopes, "addp.api") {
			for _, scope := range snapshot.CredentialScopes {
				if !containsCredentialValue(snapshot.FamilyScopes, scope) {
					return invalidCredential(CredentialInvalidContext)
				}
			}
		}
	default:
		return invalidCredential(CredentialInvalidUnsupportedTokenType)
	}
	return nil
}

func validateSessionCredentialSnapshot(snapshot *SessionCredentialAuthSnapshot) error {
	if snapshot == nil {
		return invalidCredential(CredentialInvalidNotFound)
	}
	now := snapshot.DatabaseTime
	if snapshot.CredentialRevokedAt != nil {
		return invalidCredential(CredentialInvalidTokenRevoked)
	}
	if !snapshot.CredentialExpiresAt.After(now) {
		return invalidCredential(CredentialInvalidTokenExpired)
	}
	if snapshot.FamilyRevokedAt != nil {
		return invalidCredential(CredentialInvalidFamilyRevoked)
	}
	if !snapshot.FamilyExpiresAt.After(now) {
		return invalidCredential(CredentialInvalidFamilyExpired)
	}
	if snapshot.PrincipalStatus != PrincipalStatusActive {
		return invalidCredential(CredentialInvalidPrincipalInactive)
	}
	if snapshot.FamilyAuthorizationVersion != snapshot.PrincipalAuthorizationVersion {
		return invalidCredential(CredentialInvalidAuthorizationVersion)
	}
	if snapshot.CredentialCreatedAt.After(now) || snapshot.FamilyAuthenticatedAt.After(now) ||
		snapshot.CredentialExpiresAt.After(snapshot.FamilyExpiresAt) {
		return invalidCredential(CredentialInvalidContext)
	}
	if snapshot.FamilyStepUpExpiresAt != nil && snapshot.FamilyStepUpExpiresAt.After(snapshot.FamilyExpiresAt) {
		return invalidCredential(CredentialInvalidContext)
	}

	switch snapshot.FamilyContextType {
	case ContextTypePlatform:
		if snapshot.FamilyTenantMembershipID != nil || snapshot.TenantMembershipID != nil ||
			(snapshot.FamilyAssuranceLevel != AssuranceLevelAAL2 && snapshot.FamilyAssuranceLevel != AssuranceLevelAAL3) {
			return invalidCredential(CredentialInvalidContext)
		}
	case ContextTypeTenant:
		if snapshot.FamilyTenantMembershipID == nil || snapshot.TenantMembershipID == nil || snapshot.TenantID == nil ||
			*snapshot.FamilyTenantMembershipID != *snapshot.TenantMembershipID {
			return invalidCredential(CredentialInvalidContext)
		}
		if snapshot.TenantMembershipStatus == nil || *snapshot.TenantMembershipStatus != TenantMembershipStatusActive ||
			snapshot.TenantMembershipJoinedAt == nil || snapshot.TenantMembershipJoinedAt.After(now) ||
			(snapshot.TenantMembershipExpiresAt != nil && !snapshot.TenantMembershipExpiresAt.After(now)) {
			return invalidCredential(CredentialInvalidMembershipInactive)
		}
		if snapshot.TenantStatus == nil || *snapshot.TenantStatus != TenantStatusActive {
			return invalidCredential(CredentialInvalidTenantInactive)
		}
	default:
		return invalidCredential(CredentialInvalidContext)
	}
	return nil
}

func containsCredentialValue(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func buildRoleAssignments(rows []RoleAssignmentPermissionProjection) ([]commonauth.RoleAssignment, error) {
	assignments := make([]commonauth.RoleAssignment, 0)
	assignmentIndexes := make(map[int64]int)
	for _, row := range rows {
		index, exists := assignmentIndexes[row.AssignmentID]
		if !exists {
			scope, err := buildAssignmentScope(row)
			if err != nil {
				return nil, err
			}
			assignments = append(assignments, commonauth.RoleAssignment{
				AssignmentID: formatIAMID(row.AssignmentID),
				RoleKey:      row.RoleKey,
				Scope:        scope,
				Permissions:  make([]string, 0),
				SourceType:   row.SourceType,
				ValidFrom:    row.ValidFrom.UTC(),
				ValidUntil:   utcTimePointer(row.ValidUntil),
			})
			index = len(assignments) - 1
			assignmentIndexes[row.AssignmentID] = index
		}
		assignment := &assignments[index]
		if assignment.RoleKey != row.RoleKey || assignment.SourceType != row.SourceType {
			return nil, fmt.Errorf("role assignment %d projection is inconsistent", row.AssignmentID)
		}
		assignment.Permissions = append(assignment.Permissions, row.PermissionKey)
	}
	return assignments, nil
}

func buildAssignmentScope(row RoleAssignmentPermissionProjection) (commonauth.AssignmentScope, error) {
	scope := commonauth.AssignmentScope{Type: row.ScopeType}
	switch row.ScopeType {
	case "platform":
		if row.TenantID != nil || row.DepartmentID != nil || row.ProjectGroupID != nil {
			return commonauth.AssignmentScope{}, fmt.Errorf("platform assignment %d contains tenant fields", row.AssignmentID)
		}
	case "tenant":
		if row.TenantID == nil || row.DepartmentID != nil || row.ProjectGroupID != nil {
			return commonauth.AssignmentScope{}, fmt.Errorf("tenant assignment %d fields are invalid", row.AssignmentID)
		}
		tenantID := formatIAMID(*row.TenantID)
		scope.TenantID = &tenantID
	case "department":
		if row.TenantID == nil || row.DepartmentID == nil || row.ProjectGroupID != nil {
			return commonauth.AssignmentScope{}, fmt.Errorf("department assignment %d fields are invalid", row.AssignmentID)
		}
		tenantID := formatIAMID(*row.TenantID)
		departmentID := formatIAMID(*row.DepartmentID)
		scope.TenantID = &tenantID
		scope.DepartmentID = &departmentID
	case "project_group":
		if row.TenantID == nil || row.DepartmentID != nil || row.ProjectGroupID == nil {
			return commonauth.AssignmentScope{}, fmt.Errorf("project group assignment %d fields are invalid", row.AssignmentID)
		}
		tenantID := formatIAMID(*row.TenantID)
		projectGroupID := formatIAMID(*row.ProjectGroupID)
		scope.TenantID = &tenantID
		scope.ProjectGroupID = &projectGroupID
	default:
		return commonauth.AssignmentScope{}, fmt.Errorf("role assignment %d has unsupported scope", row.AssignmentID)
	}
	return scope, nil
}

func invalidCredential(reason CredentialInvalidReason) error {
	return &CredentialValidationError{Reason: reason}
}

func formatIAMID(value int64) string {
	return strconv.FormatInt(value, 10)
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
