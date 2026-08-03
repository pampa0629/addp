package iam

import (
	"context"
	"fmt"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

const (
	defaultNotebookSessionAuthorizationTTL = time.Hour
	maximumNotebookExecutionAccessTTL      = time.Hour
	notebookSessionAudience                = "develop"
	notebookCatalogOperation               = "catalog.list_children"
	notebookExecutionAccessOperation       = "execution_engine_access.derive"
	notebookCatalogUserPermission          = "system.engine.read"
	notebookSessionServiceClientID         = "addp-develop"
)

var (
	ErrNotebookSessionAuthorizationConflict  = fmt.Errorf("%w: notebook session authorization already exists", commonapi.ErrConflict)
	ErrNotebookSessionAuthorizationForbidden = fmt.Errorf("%w: notebook session authorization is unavailable", commonapi.ErrForbidden)
)

type IssueNotebookSessionAuthorizationInput struct {
	SourceAccessToken string
	SessionID         uuid.UUID
	TaskID            int64
	ExpiresIn         time.Duration
	Audit             AuditMetadata
}

type IssuedNotebookSessionAuthorization struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	TaskID    int64
	ExpiresAt time.Time
}

type AuthorizeNotebookCatalogInput struct {
	AuthorizationID    uuid.UUID
	SessionID          uuid.UUID
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

type AuthorizedNotebookCatalog struct {
	AuthorizationID uuid.UUID
	SessionID       uuid.UUID
	TaskID          int64
	TenantID        int64
	ExpiresAt       time.Time
}

type DeriveNotebookExecutionEngineAccessInput struct {
	AuthorizationID    uuid.UUID
	SessionID          uuid.UUID
	ExecutionID        uuid.UUID
	EngineID           int64
	ExpiresIn          time.Duration
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

type RevokeNotebookSessionAuthorizationInput struct {
	AuthorizationID    uuid.UUID
	SessionID          uuid.UUID
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

type NotebookSessionAuthorizationService struct {
	repository *Repository
}

func NewNotebookSessionAuthorizationService(repository *Repository) (*NotebookSessionAuthorizationService, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: notebook session authorization repository is required", commonapi.ErrBadRequest)
	}
	return &NotebookSessionAuthorizationService{repository: repository}, nil
}

func (s *NotebookSessionAuthorizationService) Issue(
	ctx context.Context,
	input IssueNotebookSessionAuthorizationInput,
) (*IssuedNotebookSessionAuthorization, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: notebook session authorization service is required", commonapi.ErrBadRequest)
	}
	if input.SessionID == uuid.Nil || input.TaskID <= 0 || input.ExpiresIn <= 0 ||
		input.ExpiresIn > defaultNotebookSessionAuthorizationTTL || input.ExpiresIn%time.Second != 0 {
		return nil, fmt.Errorf("%w: invalid notebook session authorization request", commonapi.ErrBadRequest)
	}
	sourceSnapshot, err := resolveDelegationSourceAccessTokenSnapshot(ctx, s.repository, input.SourceAccessToken)
	if err != nil {
		return nil, err
	}

	var issued *IssuedNotebookSessionAuthorization
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, sourceSnapshot.FamilyPrincipalID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		family, err := tx.LockRefreshTokenFamily(ctx, sourceSnapshot.CredentialFamilyID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		accessToken, err := tx.LockAccessToken(ctx, sourceSnapshot.CredentialID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		lockedSnapshot, err := resolveDelegationSourceAccessTokenSnapshot(ctx, tx, input.SourceAccessToken)
		if err != nil {
			return err
		}
		if !sameLockedUserAccessTokenSnapshot(lockedSnapshot, principal, family, accessToken) ||
			lockedSnapshot.FamilyContextType != ContextTypeTenant || lockedSnapshot.TenantID == nil ||
			lockedSnapshot.TenantMembershipID == nil {
			return commonapi.ErrUnauthorized
		}
		permissionRows, err := tx.ListEffectiveRoleAssignmentPermissions(
			ctx, principal.ID, principal.PrincipalType, ContextTypeTenant,
			lockedSnapshot.TenantID, lockedSnapshot.TenantMembershipID, lockedSnapshot.DatabaseTime,
		)
		if err != nil {
			return err
		}
		if !containsNotebookCatalogPermission(permissionRows) {
			return commonapi.ErrForbidden
		}

		now := lockedSnapshot.DatabaseTime.UTC()
		expiresAt := earlierTime(now.Add(input.ExpiresIn), family.ExpiresAt.UTC())
		if !expiresAt.After(now) {
			return commonapi.ErrUnauthorized
		}
		authorization := &NotebookSessionAuthorization{
			ID: uuid.New(), SessionID: input.SessionID, TaskID: input.TaskID,
			ActorPrincipalID: principal.ID, TenantID: *lockedSnapshot.TenantID,
			TenantMembershipID: *lockedSnapshot.TenantMembershipID, TokenFamilyID: family.ID,
			IssuedAuthorizationVersion: principal.AuthorizationVersion,
			Audience:                   notebookSessionAudience,
			Operations:                 pq.StringArray{notebookCatalogOperation, notebookExecutionAccessOperation},
			ExpiresAt:                  expiresAt, CreatedAt: now,
		}
		if err := tx.CreateNotebookSessionAuthorization(ctx, authorization); err != nil {
			return err
		}
		if err := writeNotebookCatalogAudit(ctx, tx, notebookCatalogSourceAudit(input.Audit, principal, family, authorization),
			"iam.notebook_session_authorization.issued", authorization, nil); err != nil {
			return err
		}
		issued = &IssuedNotebookSessionAuthorization{
			ID: authorization.ID, SessionID: authorization.SessionID,
			TaskID: authorization.TaskID, ExpiresAt: authorization.ExpiresAt.UTC(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return issued, nil
}

func (s *NotebookSessionAuthorizationService) Authorize(
	ctx context.Context,
	input AuthorizeNotebookCatalogInput,
) (*AuthorizedNotebookCatalog, error) {
	if err := validateNotebookSessionServiceActor(input.AuthorizationID, input.SessionID,
		input.ServicePrincipalID, input.ServiceClientID, input.TenantID, input.Audit); err != nil {
		return nil, err
	}
	var authorized *AuthorizedNotebookCatalog
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		authorization, principal, membership, _, now, err := lockAndValidateNotebookSessionSource(
			ctx, tx, input.AuthorizationID,
		)
		if err != nil {
			return err
		}
		if authorization.SessionID != input.SessionID || authorization.TenantID != input.TenantID ||
			authorization.Audience != notebookSessionAudience ||
			!containsNotebookSessionOperation(authorization.Operations, notebookCatalogOperation) ||
			authorization.RevokedAt != nil || !authorization.ExpiresAt.After(now) {
			return ErrNotebookSessionAuthorizationForbidden
		}
		permissions, err := tx.ListEffectiveRoleAssignmentPermissions(
			ctx, principal.ID, principal.PrincipalType, ContextTypeTenant,
			&membership.TenantID, &membership.ID, now,
		)
		if err != nil {
			return err
		}
		if !containsNotebookCatalogPermission(permissions) {
			return ErrNotebookSessionAuthorizationForbidden
		}
		if err := writeNotebookCatalogAudit(ctx, tx, input.Audit,
			"iam.notebook_session_authorization.consumed", authorization, nil); err != nil {
			return err
		}
		authorized = &AuthorizedNotebookCatalog{
			AuthorizationID: authorization.ID, SessionID: authorization.SessionID,
			TaskID: authorization.TaskID, TenantID: authorization.TenantID,
			ExpiresAt: authorization.ExpiresAt.UTC(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return authorized, nil
}

func (s *NotebookSessionAuthorizationService) DeriveExecutionEngineAccess(
	ctx context.Context,
	input DeriveNotebookExecutionEngineAccessInput,
) (*AuthorizedExecutionEngineAccess, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: notebook session authorization service is required", commonapi.ErrBadRequest)
	}
	if input.ExecutionID == uuid.Nil || input.EngineID <= 0 || input.ExpiresIn <= 0 ||
		input.ExpiresIn > maximumNotebookExecutionAccessTTL || input.ExpiresIn%time.Second != 0 {
		return nil, fmt.Errorf("%w: invalid notebook execution engine access request", commonapi.ErrBadRequest)
	}
	if err := validateNotebookSessionServiceActor(input.AuthorizationID, input.SessionID,
		input.ServicePrincipalID, input.ServiceClientID, input.TenantID, input.Audit); err != nil {
		return nil, err
	}

	var access *AuthorizedExecutionEngineAccess
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		sessionAuthorization, principal, membership, _, now, err := lockAndValidateNotebookSessionSource(
			ctx, tx, input.AuthorizationID,
		)
		if err != nil {
			return err
		}
		if sessionAuthorization.SessionID != input.SessionID || sessionAuthorization.TenantID != input.TenantID ||
			sessionAuthorization.Audience != notebookSessionAudience ||
			!containsNotebookSessionOperation(sessionAuthorization.Operations, notebookExecutionAccessOperation) ||
			sessionAuthorization.RevokedAt != nil || !sessionAuthorization.ExpiresAt.After(now) {
			return ErrNotebookSessionAuthorizationForbidden
		}
		permissions, err := tx.ListEffectiveRoleAssignmentPermissions(
			ctx, principal.ID, principal.PrincipalType, ContextTypeTenant,
			&membership.TenantID, &membership.ID, now,
		)
		if err != nil {
			return err
		}
		if !containsNotebookCatalogPermission(permissions) ||
			!containsAllExecutionPermissions(permissions, notebookSessionAudience, []string{"read"}) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		available, err := tx.ExecutionAuthorizationEngineAvailable(ctx, input.TenantID, input.EngineID)
		if err != nil {
			return err
		}
		if !available {
			return ErrExecutionAuthorizationUnavailable
		}
		expiresAt := earlierTime(now.Add(input.ExpiresIn), sessionAuthorization.ExpiresAt.UTC())
		if !expiresAt.After(now) {
			return ErrNotebookSessionAuthorizationForbidden
		}
		authorization := &ExecutionAuthorization{
			ActorPrincipalID: principal.ID, TenantID: input.TenantID,
			TenantMembershipID: membership.ID, IssuedAuthorizationVersion: principal.AuthorizationVersion,
			SourceType:                           executionAuthorizationSourceUser,
			SourceNotebookSessionAuthorizationID: &sessionAuthorization.ID,
			ExecutionID:                          input.ExecutionID,
			Audience:                             notebookSessionAudience, Effects: pq.StringArray{"read"},
			EngineIDs: pq.Int64Array{input.EngineID}, ExpiresAt: expiresAt, CreatedAt: now,
		}
		if err := tx.CreateExecutionAuthorization(ctx, authorization); err != nil {
			return err
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.notebook_session_authorization.execution_engine_access_derived",
			Result: AuditResultSucceeded, RiskLevel: AuditRiskLow, ModuleName: "system",
			EntityType: "execution_authorization", EntityID: strconv.FormatInt(authorization.ID, 10),
			Details: map[string]any{
				"notebook_session_authorization_id": sessionAuthorization.ID.String(),
				"session_id":                        sessionAuthorization.SessionID.String(), "task_id": strconv.FormatInt(sessionAuthorization.TaskID, 10),
				"execution_id": input.ExecutionID.String(), "engine_id": strconv.FormatInt(input.EngineID, 10),
				"effects": []string{"read"}, "expires_at": expiresAt.UTC(),
			},
		}); err != nil {
			return err
		}
		access = &AuthorizedExecutionEngineAccess{
			AuthorizationID: authorization.ID, ExecutionID: authorization.ExecutionID,
			Audience: authorization.Audience, EngineID: input.EngineID, Effects: []string{"read"},
			TenantID: input.TenantID, ExpiresAt: expiresAt.UTC(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return access, nil
}

func (s *NotebookSessionAuthorizationService) Revoke(
	ctx context.Context,
	input RevokeNotebookSessionAuthorizationInput,
) error {
	if err := validateNotebookSessionServiceActor(input.AuthorizationID, input.SessionID,
		input.ServicePrincipalID, input.ServiceClientID, input.TenantID, input.Audit); err != nil {
		return err
	}
	return s.repository.Transaction(ctx, func(tx *Repository) error {
		now, err := tx.CurrentDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if err := tx.RevokeNotebookSessionAuthorization(
			ctx, input.AuthorizationID, input.SessionID, input.TenantID,
			now.UTC(), "notebook_session_closed",
		); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.notebook_session_authorization.revoked",
			Result: AuditResultSucceeded, RiskLevel: AuditRiskLow, ModuleName: "system",
			EntityType: "notebook_session_authorization", EntityID: input.AuthorizationID.String(),
			Details: map[string]any{"session_id": input.SessionID.String()},
		})
	})
}

func validateNotebookSessionServiceActor(
	authorizationID, sessionID uuid.UUID,
	servicePrincipalID int64,
	serviceClientID string,
	tenantID int64,
	audit AuditMetadata,
) error {
	if authorizationID == uuid.Nil || sessionID == uuid.Nil || servicePrincipalID <= 0 || tenantID <= 0 ||
		serviceClientID != notebookSessionServiceClientID {
		return ErrNotebookSessionAuthorizationForbidden
	}
	if audit.PrincipalID == nil || *audit.PrincipalID != servicePrincipalID ||
		audit.PrincipalType == nil || *audit.PrincipalType != PrincipalTypeServicePrincipal ||
		audit.ContextType == nil || *audit.ContextType != ContextTypeTenant ||
		audit.TenantID == nil || *audit.TenantID != tenantID {
		return fmt.Errorf("%w: invalid notebook catalog service actor", commonapi.ErrBadRequest)
	}
	return nil
}

func lockAndValidateNotebookSessionSource(
	ctx context.Context,
	repository *Repository,
	authorizationID uuid.UUID,
) (*NotebookSessionAuthorization, *Principal, *TenantMembership, *RefreshTokenFamily, time.Time, error) {
	snapshot, err := repository.GetNotebookSessionAuthorization(ctx, authorizationID)
	if err != nil {
		return nil, nil, nil, nil, time.Time{}, ErrNotebookSessionAuthorizationForbidden
	}
	principal, err := repository.LockPrincipal(ctx, snapshot.ActorPrincipalID)
	if err != nil {
		return nil, nil, nil, nil, time.Time{}, ErrNotebookSessionAuthorizationForbidden
	}
	family, err := repository.LockRefreshTokenFamily(ctx, snapshot.TokenFamilyID)
	if err != nil {
		return nil, nil, nil, nil, time.Time{}, ErrNotebookSessionAuthorizationForbidden
	}
	membership, err := repository.LockTenantMembershipByID(ctx, snapshot.TenantMembershipID)
	if err != nil {
		return nil, nil, nil, nil, time.Time{}, ErrNotebookSessionAuthorizationForbidden
	}
	tenant, err := repository.LockTenant(ctx, snapshot.TenantID)
	if err != nil {
		return nil, nil, nil, nil, time.Time{}, ErrNotebookSessionAuthorizationForbidden
	}
	authorization, err := repository.LockNotebookSessionAuthorization(ctx, authorizationID)
	if err != nil {
		return nil, nil, nil, nil, time.Time{}, ErrNotebookSessionAuthorizationForbidden
	}
	now, err := repository.CurrentDatabaseTime(ctx)
	if err != nil {
		return nil, nil, nil, nil, time.Time{}, err
	}
	if !sameNotebookSessionAuthorizationSnapshot(snapshot, authorization) ||
		principal.PrincipalType != PrincipalTypeUser || principal.Status != PrincipalStatusActive ||
		principal.AuthorizationVersion != authorization.IssuedAuthorizationVersion ||
		membership.PrincipalID != principal.ID || membership.TenantID != authorization.TenantID ||
		membership.Status != TenantMembershipStatusActive ||
		(membership.ExpiresAt != nil && !membership.ExpiresAt.After(now)) || tenant.Status != TenantStatusActive ||
		family.ID != authorization.TokenFamilyID || family.PrincipalID != principal.ID ||
		family.ContextType != ContextTypeTenant || family.TenantMembershipID == nil ||
		*family.TenantMembershipID != membership.ID || family.RevokedAt != nil || !family.ExpiresAt.After(now) {
		return nil, nil, nil, nil, time.Time{}, ErrNotebookSessionAuthorizationForbidden
	}
	return authorization, principal, membership, family, now.UTC(), nil
}

func sameNotebookSessionAuthorizationSnapshot(
	snapshot, authorization *NotebookSessionAuthorization,
) bool {
	return snapshot != nil && authorization != nil && snapshot.ID == authorization.ID &&
		snapshot.SessionID == authorization.SessionID && snapshot.TaskID == authorization.TaskID &&
		snapshot.ActorPrincipalID == authorization.ActorPrincipalID && snapshot.TenantID == authorization.TenantID &&
		snapshot.TenantMembershipID == authorization.TenantMembershipID &&
		snapshot.TokenFamilyID == authorization.TokenFamilyID &&
		snapshot.IssuedAuthorizationVersion == authorization.IssuedAuthorizationVersion
}

func containsNotebookSessionOperation(operations []string, required string) bool {
	for _, operation := range operations {
		if operation == required {
			return true
		}
	}
	return false
}

func containsNotebookCatalogPermission(rows []RoleAssignmentPermissionProjection) bool {
	for _, row := range rows {
		if row.PermissionKey == notebookCatalogUserPermission {
			return true
		}
	}
	return false
}

func writeNotebookCatalogAudit(
	ctx context.Context,
	repository *Repository,
	metadata AuditMetadata,
	eventName string,
	authorization *NotebookSessionAuthorization,
	details map[string]any,
) error {
	if authorization == nil {
		return fmt.Errorf("%w: notebook catalog audit facts are required", commonapi.ErrBadRequest)
	}
	if details == nil {
		details = map[string]any{}
	}
	details["session_id"] = authorization.SessionID.String()
	details["task_id"] = strconv.FormatInt(authorization.TaskID, 10)
	details["expires_at"] = authorization.ExpiresAt.UTC()
	details["authorization_version"] = authorization.IssuedAuthorizationVersion
	return NewAuditWriter(repository).Write(ctx, AuditEvent{
		Metadata: metadata, EventName: eventName, Result: AuditResultSucceeded,
		RiskLevel: AuditRiskLow, ModuleName: "system",
		EntityType: "notebook_session_authorization", EntityID: authorization.ID.String(),
		Details: details,
	})
}

func notebookCatalogSourceAudit(
	metadata AuditMetadata,
	principal *Principal,
	family *RefreshTokenFamily,
	authorization *NotebookSessionAuthorization,
) AuditMetadata {
	principalID := principal.ID
	principalType := principal.PrincipalType
	contextType := family.ContextType
	tenantID := authorization.TenantID
	metadata.PrincipalID = &principalID
	metadata.PrincipalType = &principalType
	metadata.ContextType = &contextType
	metadata.TenantID = &tenantID
	return metadata
}
