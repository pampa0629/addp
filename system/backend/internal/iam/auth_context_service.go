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

type AccessTokenInvalidReason string

const (
	AccessTokenInvalidFormat               AccessTokenInvalidReason = "invalid_format"
	AccessTokenInvalidNotFound             AccessTokenInvalidReason = "not_found"
	AccessTokenInvalidTokenExpired         AccessTokenInvalidReason = "token_expired"
	AccessTokenInvalidTokenRevoked         AccessTokenInvalidReason = "token_revoked"
	AccessTokenInvalidFamilyExpired        AccessTokenInvalidReason = "family_expired"
	AccessTokenInvalidFamilyRevoked        AccessTokenInvalidReason = "family_revoked"
	AccessTokenInvalidPrincipalInactive    AccessTokenInvalidReason = "principal_inactive"
	AccessTokenInvalidAuthorizationVersion AccessTokenInvalidReason = "authorization_version_mismatch"
	AccessTokenInvalidContext              AccessTokenInvalidReason = "context_invalid"
	AccessTokenInvalidMembershipInactive   AccessTokenInvalidReason = "tenant_membership_inactive"
	AccessTokenInvalidTenantInactive       AccessTokenInvalidReason = "tenant_inactive"
	AccessTokenInvalidUnsupportedTokenType AccessTokenInvalidReason = "unsupported_token_type"
)

type AccessTokenValidationError struct {
	Reason AccessTokenInvalidReason
}

func (e *AccessTokenValidationError) Error() string {
	return commonapi.ErrUnauthorized.Error()
}

func (e *AccessTokenValidationError) Unwrap() error {
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

func (s *AuthContextService) ResolveFirstPartyAccessToken(
	ctx context.Context,
	accessToken string,
) (*commonauth.AuthContext, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: AuthContext service is required", commonapi.ErrBadRequest)
	}

	var authContext *commonauth.AuthContext
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		snapshot, err := resolveFirstPartyAccessTokenSnapshot(ctx, tx, accessToken)
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

		clientID := snapshot.FamilyClientID
		methods := append(make([]string, 0, len(snapshot.FamilyAuthenticationMethods)), snapshot.FamilyAuthenticationMethods...)
		audiences := append(make([]string, 0, len(snapshot.FamilyAudiences)), snapshot.FamilyAudiences...)
		scopes := append(make([]string, 0, len(snapshot.FamilyScopes)), snapshot.FamilyScopes...)
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
				ScopeMode: "unrestricted",
				Scopes:    scopes,
			},
			Organization: organization,
			Authorization: commonauth.AuthorizationFacts{
				AuthorizationVersion: formatIAMID(snapshot.PrincipalAuthorizationVersion),
				RoleAssignments:      assignments,
			},
			Token: commonauth.TokenFacts{
				Type:      "first_party_access_token",
				IssuedAt:  snapshot.TokenCreatedAt.UTC(),
				ExpiresAt: snapshot.TokenExpiresAt.UTC(),
			},
		}
		if err := commonauth.ValidateAuthContext(*candidate); err != nil {
			return fmt.Errorf("project first-party AuthContext: %w", err)
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
) (*AccessTokenAuthSnapshot, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if !strings.HasPrefix(accessToken, "addp_at_") || len(accessToken) == len("addp_at_") {
		return nil, invalidAccessToken(AccessTokenInvalidFormat)
	}
	snapshot, err := repository.GetAccessTokenAuthSnapshot(ctx, hashOpaqueToken(accessToken))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			return nil, invalidAccessToken(AccessTokenInvalidNotFound)
		}
		return nil, err
	}
	if err := validateFirstPartyAccessTokenSnapshot(snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func validateFirstPartyAccessTokenSnapshot(snapshot *AccessTokenAuthSnapshot) error {
	if snapshot == nil {
		return invalidAccessToken(AccessTokenInvalidNotFound)
	}
	now := snapshot.DatabaseTime
	if snapshot.TokenRevokedAt != nil {
		return invalidAccessToken(AccessTokenInvalidTokenRevoked)
	}
	if !snapshot.TokenExpiresAt.After(now) {
		return invalidAccessToken(AccessTokenInvalidTokenExpired)
	}
	if snapshot.FamilyRevokedAt != nil {
		return invalidAccessToken(AccessTokenInvalidFamilyRevoked)
	}
	if !snapshot.FamilyExpiresAt.After(now) {
		return invalidAccessToken(AccessTokenInvalidFamilyExpired)
	}
	if snapshot.PrincipalStatus != PrincipalStatusActive {
		return invalidAccessToken(AccessTokenInvalidPrincipalInactive)
	}
	if snapshot.PrincipalType != PrincipalTypeUser || snapshot.FamilyAuthType != "first_party" || snapshot.FamilyClientID != "addp-web" {
		return invalidAccessToken(AccessTokenInvalidUnsupportedTokenType)
	}
	if snapshot.FamilyAuthorizationVersion != snapshot.PrincipalAuthorizationVersion {
		return invalidAccessToken(AccessTokenInvalidAuthorizationVersion)
	}
	if snapshot.TokenCreatedAt.After(now) || snapshot.FamilyAuthenticatedAt.After(now) {
		return invalidAccessToken(AccessTokenInvalidContext)
	}
	if len(snapshot.FamilyAudiences) != 1 || snapshot.FamilyAudiences[0] != "addp.api" || len(snapshot.FamilyScopes) != 0 {
		return invalidAccessToken(AccessTokenInvalidContext)
	}
	if snapshot.FamilyStepUpExpiresAt != nil && snapshot.FamilyStepUpExpiresAt.After(snapshot.FamilyExpiresAt) {
		return invalidAccessToken(AccessTokenInvalidContext)
	}

	switch snapshot.FamilyContextType {
	case ContextTypePlatform:
		if snapshot.FamilyTenantMembershipID != nil || snapshot.TenantMembershipID != nil ||
			(snapshot.FamilyAssuranceLevel != AssuranceLevelAAL2 && snapshot.FamilyAssuranceLevel != AssuranceLevelAAL3) {
			return invalidAccessToken(AccessTokenInvalidContext)
		}
	case ContextTypeTenant:
		if snapshot.FamilyTenantMembershipID == nil || snapshot.TenantMembershipID == nil || snapshot.TenantID == nil ||
			*snapshot.FamilyTenantMembershipID != *snapshot.TenantMembershipID {
			return invalidAccessToken(AccessTokenInvalidContext)
		}
		if snapshot.TenantMembershipStatus == nil || *snapshot.TenantMembershipStatus != TenantMembershipStatusActive ||
			snapshot.TenantMembershipJoinedAt == nil || snapshot.TenantMembershipJoinedAt.After(now) ||
			(snapshot.TenantMembershipExpiresAt != nil && !snapshot.TenantMembershipExpiresAt.After(now)) {
			return invalidAccessToken(AccessTokenInvalidMembershipInactive)
		}
		if snapshot.TenantStatus == nil || *snapshot.TenantStatus != TenantStatusActive {
			return invalidAccessToken(AccessTokenInvalidTenantInactive)
		}
	default:
		return invalidAccessToken(AccessTokenInvalidContext)
	}
	return nil
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

func invalidAccessToken(reason AccessTokenInvalidReason) error {
	return &AccessTokenValidationError{Reason: reason}
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
