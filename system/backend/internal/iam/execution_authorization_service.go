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
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var serviceDefinitionVersionPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

const (
	defaultExecutionAuthorizationTTL              = 15 * time.Minute
	maximumExecutionAuthorizationTTL              = time.Hour
	developExecutionPermission                    = "develop.task.execute"
	qualityExecutionPermission                    = "quality.check_task.execute"
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
	"addp-quality": "addp-quality",
	"develop":      "addp-develop",
	"duckdb":       "addp-duckdb",
	"service":      "addp-service",
}

type IssueExecutionAuthorizationInput struct {
	SourceAccessToken string
	Audience          string
	ExecutionID       uuid.UUID
	EngineIDs         []int64
	Effects           []string
	ExpiresIn         time.Duration
	Audit             AuditMetadata
}

type IssueExecutionAuthorizationFromExecutionInput struct {
	ParentExecutionID  uuid.UUID
	Audience           string
	ExecutionID        uuid.UUID
	EngineIDs          []int64
	Effects            []string
	ExpiresIn          time.Duration
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

type IssueExecutionAuthorizationFromServiceDefinitionInput struct {
	ExecutionID        uuid.UUID
	EngineIDs          []int64
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
	EngineIDs                  []int64
	Effects                    []string
	ExpiresAt                  time.Time
	ActorPrincipalID           int64
	TenantID                   int64
	TenantMembershipID         int64
	IssuedAuthorizationVersion int64
	SourceType                 string
	SourceDefinitionID         *int64
	SourceDefinitionVersion    *string
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
	audience, engineIDs, effects, ttl, err := normalizeExecutionAuthorizationRequest(
		input.Audience, input.ExecutionID, input.EngineIDs, input.Effects, input.ExpiresIn,
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
			Effects:   pq.StringArray(append([]string(nil), effects...)),
			EngineIDs: pq.Int64Array(append([]int64(nil), engineIDs...)),
			ExpiresAt: expiresAt, CreatedAt: now,
		}
		if err := tx.CreateExecutionAuthorization(ctx, authorization); err != nil {
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
				"engine_ids": formatExecutionEngineIDs(engineIDs), "effects": append([]string(nil), effects...),
				"expires_at":            expiresAt.Format(time.RFC3339Nano),
				"authorization_version": strconv.FormatInt(principal.AuthorizationVersion, 10),
			},
		}); err != nil {
			return err
		}

		issued = &IssuedExecutionAuthorization{
			ID: authorization.ID, ExecutionID: authorization.ExecutionID, Audience: audience,
			EngineIDs: append([]int64(nil), engineIDs...), Effects: append([]string(nil), effects...),
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
	audience, engineIDs, effects, ttl, err := normalizeExecutionAuthorizationRequest(
		"duckdb", input.ExecutionID, input.EngineIDs, []string{"read"}, input.ExpiresIn,
	)
	if err != nil {
		return nil, err
	}
	if input.DefinitionID <= 0 || !serviceDefinitionVersionPattern.MatchString(input.DefinitionVersion) ||
		input.ServicePrincipalID <= 0 || input.ServiceClientID != "addp-service" || input.TenantID <= 0 {
		return nil, ErrExecutionAuthorizationPermissionDenied
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
		for _, engineID := range engineIDs {
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
			Effects: pq.StringArray(effects), EngineIDs: pq.Int64Array(engineIDs),
			ExpiresAt: expiresAt, CreatedAt: now.UTC(),
		}
		if err := tx.CreateExecutionAuthorization(ctx, authorization); err != nil {
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
				"engine_ids":         formatExecutionEngineIDs(engineIDs), "effects": effects,
			},
		}); err != nil {
			return err
		}
		issued = &IssuedExecutionAuthorization{
			ID: authorization.ID, ExecutionID: input.ExecutionID, Audience: audience,
			EngineIDs: append([]int64(nil), engineIDs...), Effects: append([]string(nil), effects...),
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
	audience, engineIDs, effects, ttl, err := normalizeExecutionAuthorizationRequest(
		input.Audience, input.ExecutionID, input.EngineIDs, input.Effects, input.ExpiresIn,
	)
	if err != nil {
		return nil, err
	}
	expectedClientID := executionAudienceClients[audience]
	if audience == "duckdb" {
		expectedClientID = "addp-develop"
	}
	if input.ParentExecutionID == uuid.Nil || input.ServicePrincipalID <= 0 || input.TenantID <= 0 ||
		input.ServiceClientID != expectedClientID {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	provenance, err := s.repository.GetExecutionAuthorizationProvenance(
		ctx, input.ParentExecutionID, input.ExecutionID, audience,
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
		for _, engineID := range engineIDs {
			available, err := tx.ExecutionAuthorizationEngineAvailable(ctx, provenance.TenantID, engineID)
			if err != nil {
				return err
			}
			if !available {
				return ErrExecutionAuthorizationUnavailable
			}
		}
		expiresAt := now.Add(ttl)
		authorization := &ExecutionAuthorization{
			ActorPrincipalID: principal.ID, TenantID: provenance.TenantID,
			TenantMembershipID:         membership.ID,
			IssuedAuthorizationVersion: principal.AuthorizationVersion,
			SourceType:                 executionAuthorizationSourceUser,
			ExecutionID:                input.ExecutionID, Audience: audience,
			Effects:   pq.StringArray(append([]string(nil), effects...)),
			EngineIDs: pq.Int64Array(append([]int64(nil), engineIDs...)),
			ExpiresAt: expiresAt, CreatedAt: now,
		}
		if err := tx.CreateExecutionAuthorization(ctx, authorization); err != nil {
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
				"engine_ids":          formatExecutionEngineIDs(engineIDs), "effects": append([]string(nil), effects...),
			},
		}); err != nil {
			return err
		}
		issued = &IssuedExecutionAuthorization{
			ID: authorization.ID, ExecutionID: authorization.ExecutionID, Audience: audience,
			EngineIDs: append([]int64(nil), engineIDs...), Effects: append([]string(nil), effects...),
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
		if authorization.RevokedAt != nil || !authorization.ExpiresAt.After(now) ||
			authorization.ExecutionID != input.ExecutionID || authorization.Audience == "" {
			return ErrExecutionAuthorizationUnavailable
		}
		expectedClientID, exists := executionAudienceClients[authorization.Audience]
		if !exists || input.ServiceClientID != expectedClientID || input.TenantID != authorization.TenantID ||
			!containsExecutionEngine(authorization.EngineIDs, input.EngineID) ||
			!containsAllExecutionEffects(authorization.Effects, requiredEffects) {
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
	engineIDs []int64,
	effects []string,
	expiresIn time.Duration,
) (string, []int64, []string, time.Duration, error) {
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
	normalizedEngineIDs, err := normalizeExecutionEngineIDs(engineIDs)
	if err != nil {
		return "", nil, nil, 0, err
	}
	normalizedEffects, err := normalizeExecutionEffects(effects)
	if err != nil {
		return "", nil, nil, 0, err
	}
	return audience, normalizedEngineIDs, normalizedEffects, expiresIn, nil
}

func normalizeExecutionEngineIDs(engineIDs []int64) ([]int64, error) {
	if len(engineIDs) == 0 {
		return nil, fmt.Errorf("%w: execution authorization requires an engine", commonapi.ErrBadRequest)
	}
	seen := make(map[int64]struct{}, len(engineIDs))
	normalized := make([]int64, 0, len(engineIDs))
	for _, engineID := range engineIDs {
		if engineID <= 0 {
			return nil, fmt.Errorf("%w: invalid engine ID", commonapi.ErrBadRequest)
		}
		if _, duplicate := seen[engineID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate engine ID", commonapi.ErrBadRequest)
		}
		seen[engineID] = struct{}{}
		normalized = append(normalized, engineID)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	return normalized, nil
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
	case "addp-quality":
		return containsQualityReadBoundary()
	case "develop":
		return containsDevelopBoundary()
	case "service":
		return containsServiceSampleBoundary()
	case "duckdb":
		return containsDevelopBoundary() || containsServiceSampleBoundary()
	default:
		return false
	}
}

func containsExecutionEngine(engineIDs []int64, required int64) bool {
	for _, engineID := range engineIDs {
		if engineID == required {
			return true
		}
	}
	return false
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

func formatExecutionEngineIDs(engineIDs []int64) []string {
	formatted := make([]string, 0, len(engineIDs))
	for _, engineID := range engineIDs {
		formatted = append(formatted, strconv.FormatInt(engineID, 10))
	}
	return formatted
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
