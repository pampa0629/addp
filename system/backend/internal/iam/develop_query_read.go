package iam

import (
	"context"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The composition root supplies this callback. Meta preparation occurs outside
// IAM transactions; the verifier must use the final transaction for all Grants.
type DevelopQueryReadPreparer func(context.Context, *ExecutionAuthorization) (DevelopQueryReadVerifier, error)
type DevelopQueryReadVerifier func(context.Context, *gorm.DB, *ExecutionAuthorization) (*client.DevelopQueryReadObservation, error)

func (s *ExecutionAuthorizationService) ObserveDevelopQueryRead(ctx context.Context, tenantID int64, request client.DevelopQueryReadRequest, prepare DevelopQueryReadPreparer) (*client.DevelopQueryReadObservation, error) {
	if request.Validate() != nil || tenantID <= 0 || prepare == nil {
		return nil, commonapi.ErrBadRequest
	}
	var original *ExecutionAuthorization
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		var err error
		original, _, err = tx.qualifyDevelopQueryRead(ctx, tenantID, request)
		return err
	})
	if err != nil {
		return nil, err
	}
	verify, err := prepare(ctx, original)
	if err != nil {
		return nil, err
	}
	if verify == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	var result *client.DevelopQueryReadObservation
	err = s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		current, expires, err := tx.qualifyDevelopQueryRead(ctx, tenantID, request)
		if err != nil {
			return err
		}
		if !sameDevelopActor(current, original) || current.ID != original.ID || current.ExecutionID != original.ExecutionID {
			return ErrExecutionAuthorizationPermissionDenied
		}
		observation, err := verify(ctx, tx.db, current)
		if err != nil {
			return err
		}
		if observation == nil || observation.ObservedAt.IsZero() {
			return ErrExecutionAuthorizationUnavailable
		}
		if !observation.ExpiresAt.IsZero() && observation.ExpiresAt.Before(expires) {
			expires = observation.ExpiresAt
		}
		now, err := tx.CurrentDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if !expires.After(now) {
			return ErrExecutionAuthorizationUnavailable
		}
		result = &client.DevelopQueryReadObservation{ObservedAt: now.UTC(), ExpiresAt: expires.UTC()}
		return nil
	})
	return result, err
}

func (r *Repository) qualifyDevelopQueryRead(ctx context.Context, tenantID int64, request client.DevelopQueryReadRequest) (*ExecutionAuthorization, time.Time, error) {
	id, _ := strconv.ParseInt(request.AuthorizationID, 10, 64)
	a, err := r.GetExecutionAuthorization(ctx, id)
	if err != nil {
		return nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	if a.Audience != "develop" || a.SourceType != executionAuthorizationSourceUser || a.TenantID != tenantID ||
		a.ExecutionID.String() != request.ExecutionID || a.SealedAt == nil || a.RevokedAt != nil ||
		a.SourceNotebookSessionAuthorizationID != nil || a.InternalTask != nil || a.SourceReadScope != nil || a.DevelopFileScope != nil || a.CleanupScope != nil {
		return nil, time.Time{}, ErrExecutionAuthorizationPermissionDenied
	}
	principal, err := r.GetPrincipal(ctx, a.ActorPrincipalID)
	if err != nil {
		return nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	membership, err := r.GetTenantMembershipByID(ctx, a.TenantMembershipID)
	if err != nil {
		return nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	tenant, err := r.GetTenant(ctx, a.TenantID)
	if err != nil {
		return nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	now, err := r.CurrentDatabaseTime(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	if principal.PrincipalType != PrincipalTypeUser || principal.Status != PrincipalStatusActive || principal.AuthorizationVersion != a.IssuedAuthorizationVersion ||
		membership.PrincipalID != principal.ID || membership.TenantID != a.TenantID || membership.Status != TenantMembershipStatusActive ||
		membership.ExpiresAt != nil && !membership.ExpiresAt.After(now) || tenant.Status != TenantStatusActive || !a.ExpiresAt.After(now) {
		return nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	permissions, err := r.ListEffectiveRoleAssignmentPermissions(ctx, principal.ID, principal.PrincipalType, ContextTypeTenant, &membership.TenantID, &membership.ID, now)
	if err != nil {
		return nil, time.Time{}, err
	}
	expires := a.ExpiresAt
	for _, permission := range []string{developExecutionPermission, executionEffectPermissions["read"]} {
		var until time.Time
		for _, row := range permissions {
			if row.ScopeType != "tenant" || row.PermissionKey != permission {
				continue
			}
			if row.ValidUntil == nil {
				until = a.ExpiresAt
				break
			}
			if row.ValidUntil.After(until) {
				until = *row.ValidUntil
			}
		}
		if !until.After(now) {
			return nil, time.Time{}, ErrExecutionAuthorizationPermissionDenied
		}
		if until.Before(expires) {
			expires = until
		}
	}
	engineID, _ := strconv.ParseInt(request.EngineID, 10, 64)
	access, err := r.GetExecutionAuthorizationEngineAccess(ctx, a.ID, engineID)
	if err != nil || !containsAllExecutionEffects(access.Effects, []string{"read"}) {
		return nil, time.Time{}, ErrExecutionAuthorizationPermissionDenied
	}
	var count int64
	err = r.db.WithContext(ctx).Table("system.engines").Where("id=? AND tenant_id=? AND version=? AND engine_type IN ('postgresql','gaussdb') AND lifecycle_state='active' AND deleted_at IS NULL", engineID, tenantID, request.EngineVersion).Count(&count).Error
	if err != nil || count != 1 {
		return nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	if membership.ExpiresAt != nil && membership.ExpiresAt.Before(expires) {
		expires = *membership.ExpiresAt
	}
	if request.SampleCredential != "" {
		if a.SourceExecutionAttempt != nil || a.SourceExecutionLeaseToken != nil {
			return nil, time.Time{}, ErrExecutionAuthorizationPermissionDenied
		}
		auth, err := NewAuthContextService(r)
		if err != nil {
			return nil, time.Time{}, err
		}
		user, err := auth.ResolveUserAccessToken(ctx, request.SampleCredential)
		if err != nil {
			return nil, time.Time{}, err
		}
		if user == nil || user.Principal.Type != "user" || user.Delegation != nil || user.Context.Type != "tenant" || user.Context.TenantID == nil || user.Context.TenantMembershipID == nil ||
			*user.Context.TenantID != strconv.FormatInt(a.TenantID, 10) || *user.Context.TenantMembershipID != strconv.FormatInt(a.TenantMembershipID, 10) ||
			user.Principal.ID != strconv.FormatInt(a.ActorPrincipalID, 10) || user.Authorization.AuthorizationVersion != strconv.FormatInt(a.IssuedAuthorizationVersion, 10) ||
			!developUserExecutionCredential(user, developExecutionPermission, executionEffectPermissions["read"]) {
			return nil, time.Time{}, ErrExecutionAuthorizationPermissionDenied
		}
		// A synchronous sample credential cannot turn a queued/running execution
		// into an unleased request, including attempts that have already ended.
		if err := r.db.WithContext(ctx).Table("common.task_executions").Where("execution_id=?", a.ExecutionID).Count(&count).Error; err != nil || count != 0 {
			return nil, time.Time{}, ErrExecutionAuthorizationUnavailable
		}
		if user.Token.ExpiresAt.Before(expires) {
			expires = user.Token.ExpiresAt
		}
	} else {
		lease, _ := uuid.Parse(request.LeaseToken)
		if (a.SourceExecutionAttempt == nil) != (a.SourceExecutionLeaseToken == nil) || a.SourceExecutionAttempt != nil && (*a.SourceExecutionAttempt != request.Attempt || *a.SourceExecutionLeaseToken != lease) {
			return nil, time.Time{}, ErrExecutionAuthorizationUnavailable
		}
		var claim struct {
			LeaseExpiresAt       time.Time
			ParentLeaseExpiresAt *time.Time
			ParentExecutionID    *uuid.UUID
		}
		query := r.db.WithContext(ctx).Table("common.task_executions e").Select("e.lease_expires_at, e.parent_execution_id, p.lease_expires_at AS parent_lease_expires_at").
			Joins("LEFT JOIN common.task_executions p ON p.execution_id=e.parent_execution_id AND p.tenant_id=e.tenant_id").
			Where("e.execution_id=? AND e.execution_authorization_id=? AND e.tenant_id=? AND e.module='develop' AND e.task_type='query' AND e.status='running' AND e.attempt=? AND e.lease_token=? AND e.lease_expires_at>? AND e.actor_principal_id=? AND e.actor_tenant_membership_id=? AND e.issued_authorization_version=?", a.ExecutionID, a.ID, a.TenantID, request.Attempt, lease, now, a.ActorPrincipalID, a.TenantMembershipID, a.IssuedAuthorizationVersion).
			Where("e.parent_execution_id IS NULL OR (p.module='orchestrator' AND p.status='running' AND p.lease_expires_at>? AND p.actor_principal_id=? AND p.actor_tenant_membership_id=? AND p.issued_authorization_version=?)", now, a.ActorPrincipalID, a.TenantMembershipID, a.IssuedAuthorizationVersion).Scan(&claim)
		if query.Error != nil || query.RowsAffected != 1 || a.SourceExecutionAttempt != nil && claim.ParentExecutionID == nil {
			return nil, time.Time{}, ErrExecutionAuthorizationUnavailable
		}
		if claim.LeaseExpiresAt.Before(expires) {
			expires = claim.LeaseExpiresAt
		}
		if claim.ParentLeaseExpiresAt != nil && claim.ParentLeaseExpiresAt.Before(expires) {
			expires = *claim.ParentLeaseExpiresAt
		}

	}
	if !expires.After(now) {
		return nil, time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	return a, expires, nil
}
