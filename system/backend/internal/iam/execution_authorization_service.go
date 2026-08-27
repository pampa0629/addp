package iam

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var serviceDefinitionVersionPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

const (
	defaultExecutionAuthorizationTTL              = 15 * time.Minute
	maximumExecutionAuthorizationTTL              = time.Hour
	developExecutionPermission                    = "develop.task.execute"
	transferExecutionPermission                   = "transfer.task.execute"
	qualityExecutionPermission                    = "quality.check_task.execute"
	modelMaterializationExecutionPermission       = "model.materialization.execute"
	serviceQuerySamplePermission                  = "service.definition.create"
	serviceDataReadPermission                     = "service.data_read.execute"
	executionAuthorizationSourceUser              = "user"
	executionAuthorizationSourceServiceDefinition = "service_definition"
)

var (
	ErrExecutionAuthorizationPermissionDenied = fmt.Errorf("%w: execution authorization is not permitted", commonapi.ErrForbidden)
	ErrExecutionAuthorizationConflict         = fmt.Errorf("%w: execution authorization already exists", commonapi.ErrConflict)
	ErrExecutionAuthorizationUnavailable      = fmt.Errorf("%w: execution authorization is unavailable", commonapi.ErrForbidden)
)

var executionEffectPermissions = map[string]string{
	"read":            "develop.data_read.execute",
	"write":           "develop.data_write.execute",
	"ddl":             "develop.data_ddl.execute",
	"external_effect": "develop.data_external_effect.execute",
}

var executionAudienceClients = map[string]string{
	commonExecution.AudienceModel:    "addp-model",
	commonExecution.AudienceQuality:  "addp-quality",
	commonExecution.AudienceDevelop:  "addp-develop",
	commonExecution.AudienceTransfer: "addp-transfer",
	commonExecution.AudienceDuckDB:   "addp-duckdb",
	commonExecution.AudienceService:  "addp-service",
}

type IssueExecutionAuthorizationInput struct {
	SourceAccessToken string
	Audience          string
	ExecutionID       uuid.UUID
	Accesses          []ExecutionEngineAccessScope
	ExpiresIn         time.Duration
	Audit             AuditMetadata
}

type IssueExecutionAuthorizationFromExecutionInput struct {
	ParentExecutionID  uuid.UUID
	Audience           string
	ExecutionID        uuid.UUID
	Attempt            int
	LeaseToken         uuid.UUID
	Accesses           []ExecutionEngineAccessScope
	ExpiresIn          time.Duration
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

type IssueExecutionAuthorizationFromServiceDefinitionInput struct {
	ExecutionID        uuid.UUID
	Accesses           []ExecutionEngineAccessScope
	DefinitionID       int64
	DefinitionVersion  string
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	ExpiresIn          time.Duration
	Audit              AuditMetadata
}

type IssuedExecutionAuthorization struct {
	ID                         int64
	ExecutionID                uuid.UUID
	Audience                   string
	Accesses                   []ExecutionEngineAccessScope
	ExpiresAt                  time.Time
	ActorPrincipalID           int64
	TenantID                   int64
	TenantMembershipID         int64
	IssuedAuthorizationVersion int64
	SourceType                 string
	SourceDefinitionID         *int64
	SourceDefinitionVersion    *string
}

type ExecutionEngineAccessScope struct {
	EngineID int64
	Effects  []string
}

type AuthorizeExecutionEngineAccessInput struct {
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	EngineID           int64
	RequiredEffects    []string
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

type AuthorizedExecutionEngineAccess struct {
	AuthorizationID int64
	ExecutionID     uuid.UUID
	Audience        string
	EngineID        int64
	Effects         []string
	TenantID        int64
	ExpiresAt       time.Time
}

type ExecutionAuthorizationService struct {
	repository *Repository
}

func NewExecutionAuthorizationService(repository *Repository) (*ExecutionAuthorizationService, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: execution authorization repository is required", commonapi.ErrBadRequest)
	}
	return &ExecutionAuthorizationService{repository: repository}, nil
}

func (s *ExecutionAuthorizationService) Issue(
	ctx context.Context,
	input IssueExecutionAuthorizationInput,
) (*IssuedExecutionAuthorization, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: execution authorization service is required", commonapi.ErrBadRequest)
	}
	audience, accesses, effects, ttl, err := normalizeExecutionAuthorizationRequest(
		input.Audience, input.ExecutionID, input.Accesses, input.ExpiresIn,
	)
	if err != nil {
		return nil, err
	}
	sourceSnapshot, err := resolveDelegationSourceAccessTokenSnapshot(ctx, s.repository, input.SourceAccessToken)
	if err != nil {
		return nil, err
	}

	var issued *IssuedExecutionAuthorization
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
		if !sameLockedUserAccessTokenSnapshot(lockedSnapshot, principal, family, sourceAccessToken) ||
			lockedSnapshot.FamilyContextType != ContextTypeTenant || lockedSnapshot.TenantID == nil ||
			lockedSnapshot.TenantMembershipID == nil {
			return commonapi.ErrUnauthorized
		}

		permissionRows, err := tx.ListEffectiveRoleAssignmentPermissions(
			ctx,
			lockedSnapshot.FamilyPrincipalID,
			lockedSnapshot.PrincipalType,
			lockedSnapshot.FamilyContextType,
			lockedSnapshot.TenantID,
			lockedSnapshot.TenantMembershipID,
			lockedSnapshot.DatabaseTime,
		)
		if err != nil {
			return err
		}
		if !containsAllExecutionPermissions(permissionRows, audience, effects) {
			return ErrExecutionAuthorizationPermissionDenied
		}

		now := lockedSnapshot.DatabaseTime.UTC()
		expiresAt := now.Add(ttl)
		authorization := &ExecutionAuthorization{
			ActorPrincipalID: principal.ID, TenantID: *lockedSnapshot.TenantID,
			TenantMembershipID:         *lockedSnapshot.TenantMembershipID,
			IssuedAuthorizationVersion: principal.AuthorizationVersion,
			SourceType:                 executionAuthorizationSourceUser,
			ExecutionID:                input.ExecutionID, Audience: audience,
			ExpiresAt: expiresAt, CreatedAt: now,
		}
		if err := tx.CreateExecutionAuthorization(ctx, authorization, executionAuthorizationAccessRows(accesses)); err != nil {
			return err
		}

		audit := input.Audit
		principalID := principal.ID
		principalType := principal.PrincipalType
		contextType := family.ContextType
		audit.PrincipalID = &principalID
		audit.PrincipalType = &principalType
		audit.ContextType = &contextType
		audit.TenantID = lockedSnapshot.TenantID
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: audit, EventName: "iam.execution_authorization.issued",
			Result: AuditResultSucceeded, RiskLevel: executionAuthorizationRisk(effects), ModuleName: "system",
			EntityType: "execution_authorization", EntityID: strconv.FormatInt(authorization.ID, 10),
			Details: map[string]any{
				"audience": audience, "execution_id": input.ExecutionID.String(),
				"accesses":              executionAuthorizationAccessAudit(accesses),
				"expires_at":            expiresAt.Format(time.RFC3339Nano),
				"authorization_version": strconv.FormatInt(principal.AuthorizationVersion, 10),
			},
		}); err != nil {
			return err
		}

		issued = &IssuedExecutionAuthorization{
			ID: authorization.ID, ExecutionID: authorization.ExecutionID, Audience: audience,
			Accesses:  cloneExecutionEngineAccessScopes(accesses),
			ExpiresAt: expiresAt, ActorPrincipalID: principal.ID, TenantID: *lockedSnapshot.TenantID,
			TenantMembershipID:         *lockedSnapshot.TenantMembershipID,
			IssuedAuthorizationVersion: principal.AuthorizationVersion,
			SourceType:                 executionAuthorizationSourceUser,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return issued, nil
}

func (s *ExecutionAuthorizationService) IssueFromServiceDefinition(
	ctx context.Context,
	input IssueExecutionAuthorizationFromServiceDefinitionInput,
) (*IssuedExecutionAuthorization, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: execution authorization service is required", commonapi.ErrBadRequest)
	}
	if input.ExpiresIn == 0 {
		input.ExpiresIn = time.Minute
	}
	audience, accesses, effects, ttl, err := normalizeExecutionAuthorizationRequest(
		"duckdb", input.ExecutionID, input.Accesses, input.ExpiresIn,
	)
	if err != nil {
		return nil, err
	}
	if input.DefinitionID <= 0 || !serviceDefinitionVersionPattern.MatchString(input.DefinitionVersion) ||
		input.ServicePrincipalID <= 0 || input.ServiceClientID != "addp-service" || input.TenantID <= 0 {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	if len(effects) != 1 || effects[0] != "read" {
		return nil, fmt.Errorf("%w: service definition authorization must be read-only", commonapi.ErrBadRequest)
	}
	if ttl > time.Minute {
		return nil, fmt.Errorf("%w: service definition authorization exceeds one minute", commonapi.ErrBadRequest)
	}
	var issued *IssuedExecutionAuthorization
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, input.ServicePrincipalID)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		membership, err := tx.LockTenantMembership(ctx, input.TenantID, input.ServicePrincipalID)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		tenant, err := tx.LockTenant(ctx, input.TenantID)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		now, err := tx.CurrentDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if principal.PrincipalType != PrincipalTypeServicePrincipal || principal.Status != PrincipalStatusActive ||
			membership.Status != TenantMembershipStatusActive || membership.TenantID != input.TenantID ||
			membership.PrincipalID != principal.ID || tenant.Status != TenantStatusActive {
			return ErrExecutionAuthorizationUnavailable
		}
		for _, access := range accesses {
			engineID := access.EngineID
			available, err := tx.ExecutionAuthorizationEngineAvailable(ctx, input.TenantID, engineID)
			if err != nil {
				return err
			}
			if !available {
				return ErrExecutionAuthorizationUnavailable
			}
		}
		expiresAt := now.UTC().Add(ttl)
		definitionID := input.DefinitionID
		definitionVersion := input.DefinitionVersion
		authorization := &ExecutionAuthorization{
			ActorPrincipalID: principal.ID, TenantID: input.TenantID,
			TenantMembershipID: membership.ID, IssuedAuthorizationVersion: principal.AuthorizationVersion,
			SourceType:         executionAuthorizationSourceServiceDefinition,
			SourceDefinitionID: &definitionID, SourceDefinitionVersion: &definitionVersion,
			ExecutionID: input.ExecutionID, Audience: audience,
			ExpiresAt: expiresAt, CreatedAt: now.UTC(),
		}
		if err := tx.CreateExecutionAuthorization(ctx, authorization, executionAuthorizationAccessRows(accesses)); err != nil {
			return err
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.execution_authorization.issued_from_service_definition",
			Result: AuditResultSucceeded, RiskLevel: AuditRiskLow, ModuleName: "system",
			EntityType: "execution_authorization", EntityID: strconv.FormatInt(authorization.ID, 10),
			Details: map[string]any{
				"audience": audience, "execution_id": input.ExecutionID.String(),
				"definition_id":      strconv.FormatInt(input.DefinitionID, 10),
				"definition_version": input.DefinitionVersion,
				"accesses":           executionAuthorizationAccessAudit(accesses),
			},
		}); err != nil {
			return err
		}
		issued = &IssuedExecutionAuthorization{
			ID: authorization.ID, ExecutionID: input.ExecutionID, Audience: audience,
			Accesses:  cloneExecutionEngineAccessScopes(accesses),
			ExpiresAt: expiresAt, ActorPrincipalID: principal.ID, TenantID: input.TenantID,
			TenantMembershipID: membership.ID, IssuedAuthorizationVersion: principal.AuthorizationVersion,
			SourceType:         executionAuthorizationSourceServiceDefinition,
			SourceDefinitionID: &definitionID, SourceDefinitionVersion: &definitionVersion,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return issued, nil
}

func (s *ExecutionAuthorizationService) IssueFromExecution(
	ctx context.Context,
	input IssueExecutionAuthorizationFromExecutionInput,
) (*IssuedExecutionAuthorization, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: execution authorization service is required", commonapi.ErrBadRequest)
	}
	audience, accesses, effects, ttl, err := normalizeExecutionAuthorizationRequest(
		input.Audience, input.ExecutionID, input.Accesses, input.ExpiresIn,
	)
	if err != nil {
		return nil, err
	}
	expectedClientID := executionAudienceClients[audience]
	if audience == "duckdb" {
		expectedClientID = "addp-develop"
	}
	hasAttempt := input.Attempt != 0
	hasLeaseToken := input.LeaseToken != uuid.Nil
	if hasAttempt != hasLeaseToken || input.Attempt < 0 {
		return nil, fmt.Errorf("%w: lease attempt and token must be provided together", commonapi.ErrBadRequest)
	}
	if input.ParentExecutionID == uuid.Nil || input.ServicePrincipalID <= 0 || input.TenantID <= 0 ||
		input.ServiceClientID != expectedClientID {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	provenance, err := s.repository.GetExecutionAuthorizationProvenance(
		ctx, input.ParentExecutionID, input.ExecutionID, audience, input.Attempt, input.LeaseToken,
	)
	if err != nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	if provenance.TenantID != input.TenantID {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}

	var issued *IssuedExecutionAuthorization
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, membership, now, err := lockAndValidateExecutionActor(
			ctx, tx, provenance.ActorPrincipalID, provenance.ActorTenantMembershipID,
			provenance.TenantID, provenance.IssuedAuthorizationVersion,
		)
		if err != nil {
			return err
		}
		permissionRows, err := tx.ListEffectiveRoleAssignmentPermissions(
			ctx, principal.ID, principal.PrincipalType, ContextTypeTenant,
			&membership.TenantID, &membership.ID, now,
		)
		if err != nil {
			return err
		}
		if !containsAllExecutionPermissions(permissionRows, audience, effects) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		for _, access := range accesses {
			engineID := access.EngineID
			available, err := tx.ExecutionAuthorizationEngineAvailable(ctx, provenance.TenantID, engineID)
			if err != nil {
				return err
			}
			if !available {
				return ErrExecutionAuthorizationUnavailable
			}
		}
		expiresAt := now.Add(ttl)
		var sourceExecutionAttempt *int
		var sourceExecutionLeaseToken *uuid.UUID
		if hasAttempt {
			attempt := input.Attempt
			leaseToken := input.LeaseToken
			sourceExecutionAttempt = &attempt
			sourceExecutionLeaseToken = &leaseToken
		}
		authorization := &ExecutionAuthorization{
			ActorPrincipalID: principal.ID, TenantID: provenance.TenantID,
			TenantMembershipID:         membership.ID,
			IssuedAuthorizationVersion: principal.AuthorizationVersion,
			SourceType:                 executionAuthorizationSourceUser,
			ExecutionID:                input.ExecutionID, Audience: audience,
			SourceExecutionAttempt:    sourceExecutionAttempt,
			SourceExecutionLeaseToken: sourceExecutionLeaseToken,
			ExpiresAt:                 expiresAt, CreatedAt: now,
		}
		if err := tx.CreateExecutionAuthorization(ctx, authorization, executionAuthorizationAccessRows(accesses)); err != nil {
			return err
		}
		audit := input.Audit
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: audit, EventName: "iam.execution_authorization.issued_from_execution",
			Result: AuditResultSucceeded, RiskLevel: executionAuthorizationRisk(effects), ModuleName: "system",
			EntityType: "execution_authorization", EntityID: strconv.FormatInt(authorization.ID, 10),
			Details: map[string]any{
				"audience": audience, "execution_id": input.ExecutionID.String(),
				"parent_execution_id": input.ParentExecutionID.String(),
				"source_principal_id": strconv.FormatInt(principal.ID, 10),
				"accesses":            executionAuthorizationAccessAudit(accesses),
			},
		}); err != nil {
			return err
		}
		issued = &IssuedExecutionAuthorization{
			ID: authorization.ID, ExecutionID: authorization.ExecutionID, Audience: audience,
			Accesses:  cloneExecutionEngineAccessScopes(accesses),
			ExpiresAt: expiresAt, ActorPrincipalID: principal.ID, TenantID: provenance.TenantID,
			TenantMembershipID: membership.ID, IssuedAuthorizationVersion: principal.AuthorizationVersion,
			SourceType: executionAuthorizationSourceUser,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return issued, nil
}

func (s *ExecutionAuthorizationService) AuthorizeEngineAccess(
	ctx context.Context,
	input AuthorizeExecutionEngineAccessInput,
) (*AuthorizedExecutionEngineAccess, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: execution authorization service is required", commonapi.ErrBadRequest)
	}
	if input.AuthorizationID <= 0 || input.ExecutionID == uuid.Nil || input.EngineID <= 0 ||
		input.ServicePrincipalID <= 0 || input.TenantID <= 0 || strings.TrimSpace(input.ServiceClientID) != input.ServiceClientID {
		return nil, fmt.Errorf("%w: invalid execution engine access request", commonapi.ErrBadRequest)
	}
	if input.Audit.PrincipalID == nil || *input.Audit.PrincipalID != input.ServicePrincipalID ||
		input.Audit.PrincipalType == nil || *input.Audit.PrincipalType != PrincipalTypeServicePrincipal ||
		input.Audit.ContextType == nil || *input.Audit.ContextType != ContextTypeTenant ||
		input.Audit.TenantID == nil || *input.Audit.TenantID != input.TenantID {
		return nil, fmt.Errorf("%w: invalid execution engine access actor", commonapi.ErrBadRequest)
	}
	requiredEffects, err := normalizeExecutionEffects(input.RequiredEffects)
	if err != nil {
		return nil, err
	}
	var authorized *AuthorizedExecutionEngineAccess
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		snapshot, err := tx.GetExecutionAuthorization(ctx, input.AuthorizationID)
		if err != nil {
			return err
		}
		var authorization *ExecutionAuthorization
		var principal *Principal
		var membership *TenantMembership
		var now time.Time
		if snapshot.SourceNotebookSessionAuthorizationID != nil {
			sessionAuthorization, lockedPrincipal, lockedMembership, _, lockedNow, lockErr :=
				lockAndValidateNotebookSessionSource(ctx, tx, *snapshot.SourceNotebookSessionAuthorizationID)
			if lockErr != nil {
				return ErrExecutionAuthorizationUnavailable
			}
			authorization, err = tx.LockExecutionAuthorization(ctx, input.AuthorizationID)
			if err != nil {
				return err
			}
			if authorization.SourceNotebookSessionAuthorizationID == nil ||
				*authorization.SourceNotebookSessionAuthorizationID != sessionAuthorization.ID ||
				sessionAuthorization.RevokedAt != nil || !sessionAuthorization.ExpiresAt.After(lockedNow) ||
				authorization.ActorPrincipalID != sessionAuthorization.ActorPrincipalID ||
				authorization.TenantID != sessionAuthorization.TenantID ||
				authorization.TenantMembershipID != sessionAuthorization.TenantMembershipID ||
				authorization.IssuedAuthorizationVersion != sessionAuthorization.IssuedAuthorizationVersion ||
				authorization.ExpiresAt.After(sessionAuthorization.ExpiresAt) {
				return ErrExecutionAuthorizationUnavailable
			}
			principal, membership, now = lockedPrincipal, lockedMembership, lockedNow
		} else {
			authorization, err = tx.LockExecutionAuthorization(ctx, input.AuthorizationID)
			if err != nil {
				return err
			}
			principal, membership, now, err = lockAndValidateExecutionSource(ctx, tx, authorization)
			if err != nil {
				return err
			}
		}
		if authorization.SealedAt == nil || authorization.RevokedAt != nil || !authorization.ExpiresAt.After(now) ||
			authorization.ExecutionID != input.ExecutionID || authorization.Audience == "" {
			return ErrExecutionAuthorizationUnavailable
		}
		if authorization.SourceExecutionAttempt != nil || authorization.SourceExecutionLeaseToken != nil {
			current, err := tx.ExecutionAuthorizationLeaseCurrent(ctx, authorization)
			if err != nil || !current {
				return ErrExecutionAuthorizationUnavailable
			}
		}
		access, err := tx.GetExecutionAuthorizationEngineAccess(ctx, authorization.ID, input.EngineID)
		if err != nil {
			return ErrExecutionAuthorizationPermissionDenied
		}
		expectedClientID, exists := executionAudienceClients[authorization.Audience]
		if !exists || input.ServiceClientID != expectedClientID || input.TenantID != authorization.TenantID ||
			!containsAllExecutionEffects(access.Effects, requiredEffects) {
			return ErrExecutionAuthorizationPermissionDenied
		}

		if authorization.SourceType == executionAuthorizationSourceUser {
			permissionRows, err := tx.ListEffectiveRoleAssignmentPermissions(
				ctx, principal.ID, principal.PrincipalType, ContextTypeTenant,
				&membership.TenantID, &membership.ID, now,
			)
			if err != nil {
				return err
			}
			if !containsAllExecutionPermissions(permissionRows, authorization.Audience, requiredEffects) {
				return ErrExecutionAuthorizationPermissionDenied
			}
		}
		engineAvailable, err := tx.ExecutionAuthorizationEngineAvailable(ctx, input.TenantID, input.EngineID)
		if err != nil {
			return err
		}
		if !engineAvailable {
			return ErrExecutionAuthorizationUnavailable
		}

		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.execution_authorization.consumed",
			Result: AuditResultSucceeded, RiskLevel: executionAuthorizationRisk(requiredEffects), ModuleName: "system",
			EntityType: "execution_authorization", EntityID: strconv.FormatInt(authorization.ID, 10),
			Details: map[string]any{
				"audience": authorization.Audience, "execution_id": authorization.ExecutionID.String(),
				"engine_id":           strconv.FormatInt(input.EngineID, 10),
				"required_effects":    append([]string(nil), requiredEffects...),
				"source_principal_id": strconv.FormatInt(principal.ID, 10),
			},
		}); err != nil {
			return err
		}
		authorized = &AuthorizedExecutionEngineAccess{
			AuthorizationID: authorization.ID, ExecutionID: authorization.ExecutionID,
			Audience: authorization.Audience, EngineID: input.EngineID,
			Effects: append([]string(nil), requiredEffects...), TenantID: input.TenantID,
			ExpiresAt: authorization.ExpiresAt.UTC(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return authorized, nil
}

func lockAndValidateExecutionSource(
	ctx context.Context,
	repository *Repository,
	authorization *ExecutionAuthorization,
) (*Principal, *TenantMembership, time.Time, error) {
	if authorization == nil {
		return nil, nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	if authorization.SourceType == executionAuthorizationSourceUser {
		return lockAndValidateExecutionActor(ctx, repository, authorization.ActorPrincipalID,
			authorization.TenantMembershipID, authorization.TenantID, authorization.IssuedAuthorizationVersion)
	}
	if authorization.SourceType != executionAuthorizationSourceServiceDefinition ||
		authorization.SourceDefinitionID == nil || authorization.SourceDefinitionVersion == nil {
		return nil, nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	principal, err := repository.LockPrincipal(ctx, authorization.ActorPrincipalID)
	if err != nil {
		return nil, nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	membership, err := repository.LockTenantMembershipByID(ctx, authorization.TenantMembershipID)
	if err != nil {
		return nil, nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	tenant, err := repository.LockTenant(ctx, authorization.TenantID)
	if err != nil {
		return nil, nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	now, err := repository.CurrentDatabaseTime(ctx)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if principal.PrincipalType != PrincipalTypeServicePrincipal || principal.Status != PrincipalStatusActive ||
		principal.AuthorizationVersion != authorization.IssuedAuthorizationVersion ||
		membership.PrincipalID != principal.ID || membership.TenantID != authorization.TenantID ||
		membership.Status != TenantMembershipStatusActive || tenant.Status != TenantStatusActive {
		return nil, nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	return principal, membership, now.UTC(), nil
}

func lockAndValidateExecutionActor(
	ctx context.Context,
	repository *Repository,
	principalID, membershipID, tenantID, authorizationVersion int64,
) (*Principal, *TenantMembership, time.Time, error) {
	principal, err := repository.LockPrincipal(ctx, principalID)
	if err != nil {
		return nil, nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	membership, err := repository.LockTenantMembershipByID(ctx, membershipID)
	if err != nil {
		return nil, nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	tenant, err := repository.LockTenant(ctx, tenantID)
	if err != nil {
		return nil, nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	now, err := repository.CurrentDatabaseTime(ctx)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if principal.PrincipalType != PrincipalTypeUser || principal.Status != PrincipalStatusActive ||
		principal.AuthorizationVersion != authorizationVersion || membership.PrincipalID != principal.ID ||
		membership.TenantID != tenantID || membership.Status != TenantMembershipStatusActive ||
		(membership.ExpiresAt != nil && !membership.ExpiresAt.After(now)) || tenant.Status != TenantStatusActive {
		return nil, nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	return principal, membership, now.UTC(), nil
}

func normalizeExecutionAuthorizationRequest(
	audience string,
	executionID uuid.UUID,
	accesses []ExecutionEngineAccessScope,
	expiresIn time.Duration,
) (string, []ExecutionEngineAccessScope, []string, time.Duration, error) {
	audience = strings.TrimSpace(audience)
	if executionID == uuid.Nil || audience == "" || executionAudienceClients[audience] == "" {
		return "", nil, nil, 0, fmt.Errorf("%w: unsupported execution authorization audience", commonapi.ErrBadRequest)
	}
	if expiresIn == 0 {
		expiresIn = defaultExecutionAuthorizationTTL
	}
	if expiresIn <= 0 || expiresIn > maximumExecutionAuthorizationTTL || expiresIn%time.Second != 0 {
		return "", nil, nil, 0, fmt.Errorf("%w: invalid execution authorization expiry", commonapi.ErrBadRequest)
	}
	normalizedAccesses, normalizedEffects, err := normalizeExecutionEngineAccessScopes(accesses)
	if err != nil {
		return "", nil, nil, 0, err
	}
	return audience, normalizedAccesses, normalizedEffects, expiresIn, nil
}

func normalizeExecutionEngineAccessScopes(accesses []ExecutionEngineAccessScope) ([]ExecutionEngineAccessScope, []string, error) {
	if len(accesses) == 0 {
		return nil, nil, fmt.Errorf("%w: execution authorization requires an engine", commonapi.ErrBadRequest)
	}
	seen := make(map[int64]struct{}, len(accesses))
	allEffects := make(map[string]struct{})
	normalized := make([]ExecutionEngineAccessScope, 0, len(accesses))
	for _, access := range accesses {
		if access.EngineID <= 0 {
			return nil, nil, fmt.Errorf("%w: invalid engine ID", commonapi.ErrBadRequest)
		}
		if _, duplicate := seen[access.EngineID]; duplicate {
			return nil, nil, fmt.Errorf("%w: duplicate engine ID", commonapi.ErrBadRequest)
		}
		effects, err := normalizeExecutionEffects(access.Effects)
		if err != nil {
			return nil, nil, err
		}
		seen[access.EngineID] = struct{}{}
		for _, effect := range effects {
			allEffects[effect] = struct{}{}
		}
		normalized = append(normalized, ExecutionEngineAccessScope{EngineID: access.EngineID, Effects: effects})
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].EngineID < normalized[j].EngineID })
	union, err := normalizeExecutionEffects(mapExecutionEffects(allEffects))
	if err != nil {
		return nil, nil, err
	}
	return normalized, union, nil
}

func mapExecutionEffects(values map[string]struct{}) []string {
	effects := make([]string, 0, len(values))
	for effect := range values {
		effects = append(effects, effect)
	}
	return effects
}

func executionAuthorizationAccessRows(accesses []ExecutionEngineAccessScope) []ExecutionAuthorizationEngineAccess {
	rows := make([]ExecutionAuthorizationEngineAccess, 0, len(accesses))
	for _, access := range accesses {
		rows = append(rows, ExecutionAuthorizationEngineAccess{EngineID: access.EngineID, Effects: pq.StringArray(append([]string(nil), access.Effects...))})
	}
	return rows
}

func cloneExecutionEngineAccessScopes(accesses []ExecutionEngineAccessScope) []ExecutionEngineAccessScope {
	cloned := make([]ExecutionEngineAccessScope, 0, len(accesses))
	for _, access := range accesses {
		cloned = append(cloned, ExecutionEngineAccessScope{EngineID: access.EngineID, Effects: append([]string(nil), access.Effects...)})
	}
	return cloned
}

func executionAuthorizationAccessAudit(accesses []ExecutionEngineAccessScope) []map[string]any {
	result := make([]map[string]any, 0, len(accesses))
	for _, access := range accesses {
		result = append(result, map[string]any{
			"engine_id": strconv.FormatInt(access.EngineID, 10),
			"effects":   append([]string(nil), access.Effects...),
		})
	}
	return result
}

func normalizeExecutionEffects(effects []string) ([]string, error) {
	if len(effects) == 0 {
		return nil, fmt.Errorf("%w: execution authorization requires an effect", commonapi.ErrBadRequest)
	}
	requested := make(map[string]struct{}, len(effects))
	for _, effect := range effects {
		if strings.TrimSpace(effect) != effect || executionEffectPermissions[effect] == "" {
			return nil, fmt.Errorf("%w: invalid execution effect", commonapi.ErrBadRequest)
		}
		if _, duplicate := requested[effect]; duplicate {
			return nil, fmt.Errorf("%w: duplicate execution effect", commonapi.ErrBadRequest)
		}
		requested[effect] = struct{}{}
	}
	order := []string{"read", "write", "ddl", "external_effect"}
	normalized := make([]string, 0, len(requested))
	for _, effect := range order {
		if _, exists := requested[effect]; exists {
			normalized = append(normalized, effect)
		}
	}
	return normalized, nil
}

func sameLockedUserAccessTokenSnapshot(
	snapshot *SessionCredentialAuthSnapshot,
	principal *Principal,
	family *RefreshTokenFamily,
	accessToken *AccessToken,
) bool {
	return snapshot != nil && principal != nil && family != nil && accessToken != nil &&
		snapshot.FamilyPrincipalID == principal.ID && snapshot.CredentialFamilyID == family.ID &&
		snapshot.CredentialID == accessToken.ID && family.PrincipalID == principal.ID &&
		accessToken.FamilyID == family.ID
}

func containsAllExecutionPermissions(
	rows []RoleAssignmentPermissionProjection,
	audience string,
	effects []string,
) bool {
	available := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		available[row.PermissionKey] = struct{}{}
	}
	containsDevelopBoundary := func() bool {
		if _, exists := available[developExecutionPermission]; !exists {
			return false
		}
		for _, effect := range effects {
			if _, exists := available[executionEffectPermissions[effect]]; !exists {
				return false
			}
		}
		return true
	}
	containsServiceSampleBoundary := func() bool {
		if len(effects) != 1 || effects[0] != "read" {
			return false
		}
		_, canCreate := available[serviceQuerySamplePermission]
		_, canRead := available[serviceDataReadPermission]
		return canCreate && canRead
	}
	containsQualityReadBoundary := func() bool {
		if len(effects) != 1 || effects[0] != "read" {
			return false
		}
		_, canExecute := available[qualityExecutionPermission]
		_, canRead := available[executionEffectPermissions["read"]]
		return canExecute && canRead
	}

	switch audience {
	case commonExecution.AudienceQuality:
		return containsQualityReadBoundary()
	case commonExecution.AudienceDevelop:
		return containsDevelopBoundary()
	case commonExecution.AudienceTransfer:
		if _, exists := available[transferExecutionPermission]; !exists {
			return false
		}
		for _, effect := range effects {
			if effect != "read" && effect != "write" {
				return false
			}
			if _, exists := available[executionEffectPermissions[effect]]; !exists {
				return false
			}
		}
		return true
	case commonExecution.AudienceModel:
		if _, exists := available[modelMaterializationExecutionPermission]; !exists {
			return false
		}
		for _, effect := range effects {
			if _, exists := available[executionEffectPermissions[effect]]; !exists {
				return false
			}
		}
		return true
	case commonExecution.AudienceService:
		return containsServiceSampleBoundary()
	case commonExecution.AudienceDuckDB:
		return containsDevelopBoundary() || containsServiceSampleBoundary()
	default:
		return false
	}
}

func containsAllExecutionEffects(available []string, required []string) bool {
	values := make(map[string]struct{}, len(available))
	for _, effect := range available {
		values[effect] = struct{}{}
	}
	for _, effect := range required {
		if _, exists := values[effect]; !exists {
			return false
		}
	}
	return true
}

func executionAuthorizationRisk(effects []string) AuditRiskLevel {
	for _, effect := range effects {
		if effect == "ddl" {
			return AuditRiskCritical
		}
	}
	for _, effect := range effects {
		if effect == "write" || effect == "external_effect" {
			return AuditRiskHigh
		}
	}
	return AuditRiskLow
}
