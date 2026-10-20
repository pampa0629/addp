package iam

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	commonmodels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExecutionSourceReadVerifier is wired by System's composition root, not by an
// HTTP caller. It must use this transaction and the source-rule authority; it
// must not write source rules or return cached observations.
type ExecutionSourceReadVerifier func(context.Context, *gorm.DB, *ExecutionAuthorization) (*ExecutionSourceReadObservation, error)

type ExecutionSourceReadObservation struct {
	ObservedAt time.Time
	Engine     *commonmodels.Engine
}

func sourceReadObservedAt(o *ExecutionSourceReadObservation) time.Time {
	if o == nil {
		return time.Time{}
	}
	return o.ObservedAt
}

// ExecutionSourceReadPreparer must not be invoked inside an IAM transaction.
// Its verifier accepts only the exact scope and identity that were prepared.
type ExecutionSourceReadPreparer func(context.Context, *ExecutionAuthorization) (ExecutionSourceReadVerifier, error)

type managerSourceAuthorizationService struct {
	repository *Repository
	prepare    ExecutionSourceReadPreparer
	purpose    managerSourcePurpose
}

func newManagerSourceAuthorizationService(repository *Repository, prepare ExecutionSourceReadPreparer, purpose managerSourcePurpose) *managerSourceAuthorizationService {
	return &managerSourceAuthorizationService{repository: repository, prepare: prepare, purpose: purpose}
}

type issueManagerSourceInput struct {
	SourceAccessToken string
	ExecutionID       uuid.UUID
	ExpiresIn         time.Duration
	Audit             AuditMetadata
}

type issuedManagerSourceAuthorization struct {
	ID              int64
	ExecutionID     uuid.UUID
	TenantID        int64
	SourceReadScope ManagerSourceReadScope
	ExpiresAt       time.Time
}

type authorizeManagerSourceInput struct {
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	Attempt            int
	LeaseToken         uuid.UUID
	SourceReadScope    ManagerSourceReadScope
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

// Issue derives the source scope from the exact pending owner execution. No
// caller-supplied actor, tenant, configuration or target list is accepted.
func (s *managerSourceAuthorizationService) issue(ctx context.Context, input issueManagerSourceInput) (*issuedManagerSourceAuthorization, error) {
	if s == nil || s.repository == nil || s.prepare == nil || s.purpose.permission() == "" || input.ExecutionID == uuid.Nil {
		return nil, commonapi.ErrBadRequest
	}
	ttl := input.ExpiresIn
	if ttl == 0 {
		ttl = defaultExecutionAuthorizationTTL
	}
	if ttl <= 0 || ttl > maximumExecutionAuthorizationTTL || ttl%time.Second != 0 {
		return nil, commonapi.ErrBadRequest
	}
	snapshot, err := resolveDelegationSourceAccessTokenSnapshot(ctx, s.repository, input.SourceAccessToken)
	if err != nil {
		return nil, err
	}
	authService, err := NewAuthContextService(s.repository)
	if err != nil {
		return nil, err
	}
	preAuth, err := authService.ResolveUserAccessToken(ctx, input.SourceAccessToken)
	if err != nil {
		return nil, err
	}
	if !managerSourceUserCredential(preAuth, s.purpose) || snapshot.TenantID == nil || snapshot.TenantMembershipID == nil {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	preRow := &ExecutionAuthorization{ActorPrincipalID: snapshot.FamilyPrincipalID, TenantID: *snapshot.TenantID,
		TenantMembershipID: *snapshot.TenantMembershipID, IssuedAuthorizationVersion: snapshot.FamilyAuthorizationVersion,
		SourceType: executionAuthorizationSourceUser, Audience: "manager", ExecutionID: input.ExecutionID}
	preRaw, err := s.repository.managerSourceExecutionConfig(ctx, preRow, 0, uuid.Nil, true, s.purpose)
	if err != nil {
		return nil, err
	}
	preRow.SourceReadScope, err = s.purpose.scope(preRaw)
	if err != nil {
		return nil, err
	}
	verify, err := s.prepare(ctx, preRow)
	if err != nil {
		return nil, err
	}
	if verify == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	var result *issuedManagerSourceAuthorization
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, snapshot.FamilyPrincipalID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		family, err := tx.LockRefreshTokenFamily(ctx, snapshot.CredentialFamilyID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		token, err := tx.LockAccessToken(ctx, snapshot.CredentialID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		current, err := resolveDelegationSourceAccessTokenSnapshot(ctx, tx, input.SourceAccessToken)
		if err != nil {
			return err
		}
		if !sameLockedUserAccessTokenSnapshot(current, principal, family, token) ||
			current.FamilyContextType != ContextTypeTenant || current.TenantID == nil || current.TenantMembershipID == nil {
			return commonapi.ErrUnauthorized
		}
		// Resolve the ordinary credential here too: OAuth tool-only and preview
		// delegates cannot acquire an unrelated background execution capability.
		authService, err := NewAuthContextService(tx)
		if err != nil {
			return err
		}
		auth, err := authService.ResolveUserAccessToken(ctx, input.SourceAccessToken)
		if err != nil {
			return err
		}
		if !managerSourceUserCredential(auth, s.purpose) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		row := &ExecutionAuthorization{ActorPrincipalID: principal.ID, TenantID: *current.TenantID,
			TenantMembershipID: *current.TenantMembershipID, IssuedAuthorizationVersion: principal.AuthorizationVersion,
			SourceType: executionAuthorizationSourceUser, Audience: "manager", ExecutionID: input.ExecutionID,
			CreatedAt: current.DatabaseTime.UTC(), ExpiresAt: current.DatabaseTime.UTC().Add(ttl)}
		raw, err := tx.managerSourceExecutionConfig(ctx, row, 0, uuid.Nil, true, s.purpose)
		if err != nil {
			return err
		}
		row.SourceReadScope, err = s.purpose.scope(raw)
		if err != nil {
			return err
		}
		if s.purpose == managerSourceModel3D {
			var prior ExecutionAuthorization
			lookup := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("audience='manager' AND execution_id=?", input.ExecutionID).Take(&prior)
			if lookup.Error == nil {
				if prior.TenantID != row.TenantID || prior.ActorPrincipalID != row.ActorPrincipalID || prior.TenantMembershipID != row.TenantMembershipID || prior.IssuedAuthorizationVersion != row.IssuedAuthorizationVersion || prior.SourceType != executionAuthorizationSourceUser || prior.SealedAt == nil || prior.RevokedAt != nil || prior.ExpiresAt.Sub(prior.CreatedAt) != ttl || !reflect.DeepEqual(prior.SourceReadScope, row.SourceReadScope) {
					return ErrExecutionAuthorizationConflict
				}
				observation, err := verify(ctx, tx.db, &prior)
				observedAt := sourceReadObservedAt(observation)
				if err != nil {
					return err
				}
				if observedAt.IsZero() || !token.ExpiresAt.After(observedAt) || !prior.ExpiresAt.After(observedAt) {
					return ErrExecutionAuthorizationUnavailable
				}
				// Recover the original immutable authorization, without a new issue
				// audit, window or capability. Never revive a revoked/expired row.
				result = &issuedManagerSourceAuthorization{ID: prior.ID, ExecutionID: prior.ExecutionID, TenantID: prior.TenantID, SourceReadScope: *prior.SourceReadScope.Clone(), ExpiresAt: prior.ExpiresAt}
				return nil
			}
			if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
				return wrapRepositoryError(lookup.Error)
			}
		}
		observation, err := verify(ctx, tx.db, row)
		observedAt := sourceReadObservedAt(observation)
		if err != nil {
			return err
		}
		if observedAt.IsZero() || !token.ExpiresAt.After(observedAt) || !row.ExpiresAt.After(observedAt) {
			return ErrExecutionAuthorizationUnavailable
		}
		targets, err := row.SourceReadScope.Targets()
		if err != nil || len(targets) == 0 {
			return ErrExecutionAuthorizationUnavailable
		}
		accesses := []ExecutionAuthorizationEngineAccess{{EngineID: int64(targets[0].EngineID), Effects: []string{"read"}}}
		if err := tx.CreateExecutionAuthorization(ctx, row, accesses); err != nil {
			return err
		}
		audit := input.Audit
		principalID, principalType, contextType := principal.ID, principal.PrincipalType, family.ContextType
		audit.PrincipalID, audit.PrincipalType, audit.ContextType, audit.TenantID = &principalID, &principalType, &contextType, &row.TenantID
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: audit,
			EventName: "iam.execution_authorization.issued", Result: AuditResultSucceeded, RiskLevel: AuditRiskLow,
			ModuleName: "system", EntityType: "execution_authorization", EntityID: strconv.FormatInt(row.ID, 10),
			Details: map[string]any{"audience": "manager", "execution_id": row.ExecutionID.String(), "source_read_scope": row.SourceReadScope}}); err != nil {
			return err
		}
		// A slow audit must not turn expired credentials, assignments or source
		// Grants into a newly committed execution capability.
		auth, err = authService.ResolveUserAccessToken(ctx, input.SourceAccessToken)
		if err != nil {
			return err
		}
		if !managerSourceUserCredential(auth, s.purpose) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		observation, err = verify(ctx, tx.db, row)
		observedAt = sourceReadObservedAt(observation)
		if err != nil {
			return err
		}
		if observedAt.IsZero() || !token.ExpiresAt.After(observedAt) || !row.ExpiresAt.After(observedAt) {
			return ErrExecutionAuthorizationUnavailable
		}
		result = &issuedManagerSourceAuthorization{ID: row.ID, ExecutionID: row.ExecutionID, TenantID: row.TenantID,
			SourceReadScope: *row.SourceReadScope.Clone(), ExpiresAt: row.ExpiresAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Authorize validates current source rules and the exact active claim. The
// result is for this owner operation, not general engine connection access.
func (s *managerSourceAuthorizationService) authorize(ctx context.Context, input authorizeManagerSourceInput) (*ExecutionSourceReadObservation, error) {
	if s == nil || s.repository == nil || s.prepare == nil || input.AuthorizationID <= 0 || input.ExecutionID == uuid.Nil ||
		input.Attempt <= 0 || input.LeaseToken == uuid.Nil || input.TenantID <= 0 || input.ServicePrincipalID <= 0 ||
		input.ServiceClientID != "addp-manager" || !s.purpose.matches(&input.SourceReadScope) {
		return nil, commonapi.ErrBadRequest
	}
	if input.Audit.PrincipalID == nil || *input.Audit.PrincipalID != input.ServicePrincipalID ||
		input.Audit.PrincipalType == nil || *input.Audit.PrincipalType != PrincipalTypeServicePrincipal ||
		input.Audit.ContextType == nil || *input.Audit.ContextType != ContextTypeTenant ||
		input.Audit.TenantID == nil || *input.Audit.TenantID != input.TenantID {
		return nil, commonapi.ErrBadRequest
	}
	var preRow *ExecutionAuthorization
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		var err error
		preRow, err = tx.GetExecutionAuthorization(ctx, input.AuthorizationID)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		if preRow.Audience != "manager" || preRow.TenantID != input.TenantID || preRow.ExecutionID != input.ExecutionID ||
			preRow.SourceType != executionAuthorizationSourceUser || !reflect.DeepEqual(preRow.SourceReadScope, &input.SourceReadScope) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		if _, _, _, err := lockAndValidateExecutionSource(ctx, tx, preRow); err != nil {
			return err
		}
		if err := tx.currentManagerSourceConsumer(ctx, input); err != nil {
			return err
		}
		raw, err := tx.managerSourceExecutionConfig(ctx, preRow, input.Attempt, input.LeaseToken, false, s.purpose)
		if err != nil {
			return err
		}
		scope, err := s.purpose.scope(raw)
		if err != nil || !reflect.DeepEqual(scope, preRow.SourceReadScope) {
			return ErrExecutionAuthorizationUnavailable
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	verify, err := s.prepare(ctx, preRow)
	if err != nil {
		return nil, err
	}
	if verify == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	var observation *ExecutionSourceReadObservation
	var observedAt time.Time
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		snapshot, err := tx.GetExecutionAuthorization(ctx, input.AuthorizationID)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		if snapshot.Audience != "manager" || snapshot.TenantID != input.TenantID || snapshot.ExecutionID != input.ExecutionID ||
			snapshot.SourceType != executionAuthorizationSourceUser || !reflect.DeepEqual(snapshot.SourceReadScope, &input.SourceReadScope) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		principal, membership, _, err := lockAndValidateExecutionSource(ctx, tx, snapshot)
		if err != nil {
			return err
		}
		row, err := tx.LockExecutionAuthorization(ctx, snapshot.ID)
		if err != nil {
			return err
		}
		if row.SealedAt == nil || row.RevokedAt != nil || row.SourceReadScope == nil {
			return ErrExecutionAuthorizationUnavailable
		}
		raw, err := tx.managerSourceExecutionConfig(ctx, row, input.Attempt, input.LeaseToken, false, s.purpose)
		if err != nil {
			return err
		}
		currentScope, err := s.purpose.scope(raw)
		if err != nil || !reflect.DeepEqual(currentScope, row.SourceReadScope) {
			return ErrExecutionAuthorizationUnavailable
		}
		if err := tx.currentManagerSourceConsumer(ctx, input); err != nil {
			return err
		}
		observation, err = verify(ctx, tx.db, row)
		observedAt = sourceReadObservedAt(observation)
		if err != nil {
			return err
		}
		permissions, err := tx.ListEffectiveRoleAssignmentPermissions(ctx, principal.ID, principal.PrincipalType, ContextTypeTenant,
			&membership.TenantID, &membership.ID, observedAt)
		if err != nil {
			return err
		}
		if !managerSourcePermissions(permissions, s.purpose) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		if observedAt.IsZero() || !row.ExpiresAt.After(observedAt) || (membership.ExpiresAt != nil && !membership.ExpiresAt.After(observedAt)) {
			return ErrExecutionAuthorizationUnavailable
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: input.Audit,
			EventName: "iam.execution_authorization.consumed", Result: AuditResultSucceeded, RiskLevel: AuditRiskLow,
			ModuleName: "system", EntityType: "execution_authorization", EntityID: strconv.FormatInt(row.ID, 10),
			Details: map[string]any{"audience": "manager", "execution_id": row.ExecutionID.String(), "attempt": input.Attempt,
				"source_read_scope": row.SourceReadScope}}); err != nil {
			return err
		}
		// Auditing and lock waits may cross a deadline; never commit success then.
		observation, err = verify(ctx, tx.db, row)
		observedAt = sourceReadObservedAt(observation)
		if err != nil {
			return err
		}
		permissions, err = tx.ListEffectiveRoleAssignmentPermissions(ctx, principal.ID, principal.PrincipalType, ContextTypeTenant,
			&membership.TenantID, &membership.ID, observedAt)
		if err != nil {
			return err
		}
		if !managerSourcePermissions(permissions, s.purpose) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		if err := tx.currentManagerSourceConsumer(ctx, input); err != nil {
			return err
		}
		if observedAt.IsZero() || !row.ExpiresAt.After(observedAt) || (membership.ExpiresAt != nil && !membership.ExpiresAt.After(observedAt)) {
			return ErrExecutionAuthorizationUnavailable
		}
		_, err = tx.managerSourceExecutionConfig(ctx, row, input.Attempt, input.LeaseToken, false, s.purpose)
		return err
	})
	if err != nil {
		return nil, err
	}
	observation.ObservedAt = observedAt.UTC()
	return observation, nil
}
