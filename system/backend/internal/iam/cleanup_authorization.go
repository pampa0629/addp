package iam

import (
	"context"
	"fmt"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IssueCleanup is called only by System acceptance inside the transaction which
// creates the private request, exact pending parent/children and acceptance audit.
// It accepts persisted server-owned origin, never caller-supplied identity fields.
func (s *ExecutionAuthorizationService) IssueCleanup(ctx context.Context, childID uuid.UUID, audit AuditMetadata) (*ExecutionAuthorization, error) {
	if s == nil || s.repository == nil || childID == uuid.Nil {
		return nil, commonapi.ErrBadRequest
	}
	var issued *ExecutionAuthorization
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		var child execution.TaskExecution
		if err := tx.db.WithContext(ctx).Where("execution_id = ?", childID.String()).First(&child).Error; err != nil {
			return err
		}
		scope, err := execution.CleanupAuthorizationFor(child)
		if err != nil {
			return ErrExecutionAuthorizationPermissionDenied
		}
		principal, membership, now, err := lockAndValidateExecutionActor(ctx, tx, *child.ActorPrincipalID, *child.ActorTenantMembershipID, int64(child.TenantID), *child.IssuedAuthorizationVersion)
		if err != nil {
			return err
		}
		permissions, err := tx.ListEffectiveRoleAssignmentPermissions(ctx, principal.ID, principal.PrincipalType, ContextTypeTenant, &membership.TenantID, &membership.ID, now)
		if err != nil {
			return err
		}
		if !hasCleanupActorPermissions(permissions, child) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		// This ceiling bounds valid children independently of the shorter coordinator
		// deadline. It is never refreshed on coordinator recovery.
		expires := now.Add(maximumExecutionAuthorizationTTL)
		if membership.ExpiresAt != nil && membership.ExpiresAt.Before(expires) {
			expires = *membership.ExpiresAt
		}
		authorization := &ExecutionAuthorization{CleanupScope: &scope, ActorPrincipalID: principal.ID, TenantID: membership.TenantID,
			TenantMembershipID: membership.ID, IssuedAuthorizationVersion: principal.AuthorizationVersion,
			SourceType: executionAuthorizationSourceUser, ExecutionID: childID, Audience: execution.AudienceCleanup, ExpiresAt: expires, CreatedAt: now}
		if err := tx.CreateExecutionAuthorization(ctx, authorization, nil); err != nil {
			return err
		}
		if err := execution.AttachPendingAuthorization(ctx, tx.db, execution.PendingIdentity{ExecutionID: child.ExecutionID, TenantID: child.TenantID, Module: child.Module, TaskType: child.TaskType, Source: child.Source}, execution.PendingAuthorization{
			ID: authorization.ID, ExpiresAt: expires, ActorPrincipalID: principal.ID, ActorTenantMembershipID: membership.ID, IssuedAuthorizationVersion: principal.AuthorizationVersion}); err != nil {
			return err
		}
		principalType, contextType := PrincipalTypeUser, ContextTypeTenant
		audit.PrincipalID, audit.PrincipalType, audit.ContextType, audit.TenantID = &principal.ID, &principalType, &contextType, &membership.TenantID
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: audit, EventName: "iam.execution_authorization.issued", Result: AuditResultSucceeded, RiskLevel: "high", ModuleName: "system", EntityType: "execution_authorization", EntityID: strconv.FormatInt(authorization.ID, 10),
			Details: map[string]any{"audience": execution.AudienceCleanup, "execution_id": child.ExecutionID, "cleanup_scope": scope}}); err != nil {
			return err
		}
		issued = authorization
		return nil
	})
	return issued, err
}

func hasCleanupPermission(rows []RoleAssignmentPermissionProjection) bool {
	for _, row := range rows {
		if row.PermissionKey == "system.cleanup.execute" {
			return true
		}
	}
	return false
}

type AuthorizeCleanupInput struct {
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	Stage              string
	Attempt            int
	LeaseToken         uuid.UUID
	Scope              execution.CleanupAuthorizationScope
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

type AuthorizedCleanup struct {
	AuthorizationID int64
	ExecutionID     uuid.UUID
	TenantID        int64
	Stage           string
	Attempt         int
	Scope           execution.CleanupAuthorizationScope
	ExpiresAt       time.Time
}

type AuthorizedCleanupEngineConnection struct {
	Access   *AuthorizedCleanup
	EngineID int64
}

type cleanupEngineConnection struct {
	engineID       int64
	read           func(context.Context, *gorm.DB, uint, uint) error
	parentDeadline time.Time
}

func (s *ExecutionAuthorizationService) AuthorizeCleanupEngineConnection(ctx context.Context, input AuthorizeCleanupInput, engineID int64, read func(context.Context, *gorm.DB, uint, uint) error) (*AuthorizedCleanupEngineConnection, error) {
	if engineID <= 0 || read == nil {
		return nil, commonapi.ErrBadRequest
	}
	if input.Stage != "effect" || input.Scope.Module != events.ModuleTransfer || input.ServiceClientID != "addp-transfer" {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	access, err := s.authorizeCleanup(ctx, input, &cleanupEngineConnection{engineID: engineID, read: read})
	if err != nil {
		return nil, err
	}
	return &AuthorizedCleanupEngineConnection{Access: access, EngineID: engineID}, nil
}

func (s *ExecutionAuthorizationService) AuthorizeCleanup(ctx context.Context, input AuthorizeCleanupInput) (*AuthorizedCleanup, error) {
	return s.authorizeCleanup(ctx, input, nil)
}

func (s *ExecutionAuthorizationService) authorizeCleanup(ctx context.Context, input AuthorizeCleanupInput, connection *cleanupEngineConnection) (*AuthorizedCleanup, error) {
	if s == nil || s.repository == nil || input.AuthorizationID <= 0 || input.ExecutionID == uuid.Nil || input.TenantID <= 0 || input.ServicePrincipalID <= 0 || input.Scope.Validate() != nil ||
		!(input.Stage == "claim" && input.Attempt == 0 && input.LeaseToken == uuid.Nil || input.Stage == "effect" && input.Attempt > 0 && input.LeaseToken != uuid.Nil) {
		return nil, fmt.Errorf("%w: invalid cleanup authorization boundary", commonapi.ErrBadRequest)
	}
	if input.ServiceClientID != "addp-"+input.Scope.Module || input.Audit.PrincipalID == nil || *input.Audit.PrincipalID != input.ServicePrincipalID || input.Audit.PrincipalType == nil || *input.Audit.PrincipalType != PrincipalTypeServicePrincipal || input.Audit.ContextType == nil || *input.Audit.ContextType != ContextTypeTenant || input.Audit.TenantID == nil || *input.Audit.TenantID != input.TenantID {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	var result *AuthorizedCleanup
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		snapshot, err := tx.GetExecutionAuthorization(ctx, input.AuthorizationID)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		if snapshot.CleanupScope == nil || *snapshot.CleanupScope != input.Scope || snapshot.Audience != execution.AudienceCleanup || snapshot.ExecutionID != input.ExecutionID || snapshot.TenantID != input.TenantID || snapshot.SourceType != executionAuthorizationSourceUser {
			return ErrExecutionAuthorizationPermissionDenied
		}
		principal, membership, _, err := lockAndValidateExecutionSource(ctx, tx, snapshot)
		if err != nil {
			return err
		}
		authorization, err := tx.LockExecutionAuthorization(ctx, snapshot.ID)
		if err != nil {
			return err
		}
		now, err := tx.internalTaskDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if authorization.SealedAt == nil || authorization.RevokedAt != nil || !authorization.ExpiresAt.After(now) {
			return ErrExecutionAuthorizationUnavailable
		}
		// Runtime clients have fixed Owner identity and current active membership.
		var runtime struct {
			MembershipID int64
			ExpiresAt    *time.Time
		}
		check := tx.db.WithContext(ctx).Raw(`SELECT member.id AS membership_id, member.expires_at FROM system.oauth_clients client
   JOIN system.principals consumer ON consumer.id=client.service_principal_id
   JOIN system.service_principals service ON service.id=consumer.id
   JOIN system.tenant_memberships member ON member.principal_id=consumer.id AND member.tenant_id=?
   WHERE client.client_id=? AND client.status='active' AND consumer.id=? AND consumer.status='active'
   AND service.owner_scope='platform' AND service.name=? AND member.status='active'
   AND (member.expires_at IS NULL OR member.expires_at>clock_timestamp())
   FOR SHARE OF client, consumer, service, member`, input.TenantID, input.ServiceClientID, input.ServicePrincipalID, input.ServiceClientID).Scan(&runtime)
		if check.Error != nil {
			return check.Error
		}
		if check.RowsAffected != 1 {
			return ErrExecutionAuthorizationUnavailable
		}
		runtimePermissions, err := tx.ListEffectiveRoleAssignmentPermissions(ctx, input.ServicePrincipalID, PrincipalTypeServicePrincipal, ContextTypeTenant, &input.TenantID, &runtime.MembershipID, now)
		if err != nil {
			return err
		}
		permitted := false
		for _, row := range runtimePermissions {
			if row.PermissionKey == "system.cleanup_authorization.execute" {
				permitted = true
			}
		}
		if !permitted {
			return ErrExecutionAuthorizationPermissionDenied
		}
		// New connection authority requires an open P, before taking C. Ordinary
		// effect authority retains its existing post-parent-closure convergence.
		if connection != nil {
			var parent execution.TaskExecution
			if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("execution_id = ? AND tenant_id = ?", input.Scope.ParentExecutionID, input.TenantID).First(&parent).Error; err != nil {
				return ErrExecutionAuthorizationUnavailable
			}
			request, err := execution.CleanupRequestFromExecution(parent)
			if err != nil || parent.Module != execution.ModuleSystem || parent.TaskType != execution.TaskTypeCleanup || parent.ActorPrincipalID == nil || *parent.ActorPrincipalID != authorization.ActorPrincipalID || parent.ActorTenantMembershipID == nil || *parent.ActorTenantMembershipID != authorization.TenantMembershipID || parent.IssuedAuthorizationVersion == nil || *parent.IssuedAuthorizationVersion != authorization.IssuedAuthorizationVersion || request.Digest != input.Scope.RequestDigest || request.RequestID != input.Scope.RequestID || request.RequestedBy != uint(authorization.ActorPrincipalID) ||
				request.Action != events.CleanupActionExecute || request.CleanupMode != events.CleanupModePhysical || request.CauseEvent != events.CleanupCauseEngineDeleting || request.Context.EngineID != uint(connection.engineID) || request.Context.TenantID != 0 || request.Context.ExternalArtifactPolicy != "delete" {
				return ErrExecutionAuthorizationPermissionDenied
			}
			if parent.Status != execution.ExecutionStatusRunning || parent.Attempt <= 0 || parent.StartedAt == nil || parent.CompletedAt != nil || parent.LeaseExpiresAt == nil || parent.LeaseOwner == nil || *parent.LeaseOwner == "" || parent.LeaseToken == nil || *parent.LeaseToken == "" {
				return ErrExecutionAuthorizationUnavailable
			}
			connection.parentDeadline = request.Deadline
			if parent.LeaseExpiresAt.Before(connection.parentDeadline) {
				connection.parentDeadline = *parent.LeaseExpiresAt
			}
		}

		var child execution.TaskExecution
		if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("execution_id = ? AND tenant_id = ?", input.ExecutionID.String(), input.TenantID).First(&child).Error; err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		scope, err := execution.CleanupAuthorizationFor(child)
		if err != nil || scope != input.Scope || child.ExecutionAuthorizationID == nil || *child.ExecutionAuthorizationID != authorization.ID || child.AuthorizationExpiresAt == nil || !child.AuthorizationExpiresAt.Equal(authorization.ExpiresAt) ||
			*child.ActorPrincipalID != authorization.ActorPrincipalID || *child.ActorTenantMembershipID != authorization.TenantMembershipID || *child.IssuedAuthorizationVersion != authorization.IssuedAuthorizationVersion {
			return ErrExecutionAuthorizationPermissionDenied
		}
		expires := authorization.ExpiresAt
		if membership.ExpiresAt != nil && membership.ExpiresAt.Before(expires) {
			expires = *membership.ExpiresAt
		}
		if runtime.ExpiresAt != nil && runtime.ExpiresAt.Before(expires) {
			expires = *runtime.ExpiresAt
		}
		if input.Stage == "claim" {
			request, _ := execution.CleanupRequestFromExecution(child)
			if child.Status != execution.ExecutionStatusPending || child.Attempt != 0 || child.StartedAt != nil || child.CompletedAt != nil || child.LeaseToken != nil || child.LeaseOwner != nil || child.LeaseExpiresAt != nil {
				return ErrExecutionAuthorizationUnavailable
			}
			if request.Deadline.Before(expires) {
				expires = request.Deadline
			}
		} else {
			if child.Status != execution.ExecutionStatusRunning || child.Attempt != input.Attempt || child.LeaseToken == nil || *child.LeaseToken != input.LeaseToken.String() || child.LeaseExpiresAt == nil || child.StartedAt == nil || child.CompletedAt != nil {
				return ErrExecutionAuthorizationUnavailable
			}
			if child.LeaseExpiresAt.Before(expires) {
				expires = *child.LeaseExpiresAt
			}
		}
		if connection != nil {
			if child.LeaseOwner == nil || *child.LeaseOwner == "" {
				return ErrExecutionAuthorizationUnavailable
			}
			if child.ParentExecutionID == nil || *child.ParentExecutionID != input.Scope.ParentExecutionID {
				return ErrExecutionAuthorizationPermissionDenied
			}
			if connection.parentDeadline.Before(expires) {
				expires = connection.parentDeadline
			}

		}
		recheck := func() error {
			// Locks can cross either authorization or lease expiry.
			now, err := tx.internalTaskDatabaseTime(ctx)
			if err != nil {
				return err
			}
			if !expires.After(now) {
				return ErrExecutionAuthorizationUnavailable
			}
			// A role assignment may expire while the child row is locked elsewhere.
			// Recheck using the post-lock wall clock, not the earlier transaction time.
			permissions, err := tx.ListEffectiveRoleAssignmentPermissions(ctx, principal.ID, principal.PrincipalType, ContextTypeTenant, &membership.TenantID, &membership.ID, now)
			if err != nil {
				return err
			}
			if !hasCleanupActorPermissions(permissions, child) {
				return ErrExecutionAuthorizationPermissionDenied
			}
			runtimePermissions, err := tx.ListEffectiveRoleAssignmentPermissions(ctx, input.ServicePrincipalID, PrincipalTypeServicePrincipal, ContextTypeTenant, &input.TenantID, &runtime.MembershipID, now)
			if err != nil {
				return err
			}
			permitted := false
			for _, row := range runtimePermissions {
				if row.PermissionKey == "system.cleanup_authorization.execute" {
					permitted = true
				}
			}
			if !permitted {
				return ErrExecutionAuthorizationPermissionDenied
			}
			return nil
		}
		if err := recheck(); err != nil {
			return err
		}
		if connection != nil {
			// System Engine read/decrypt shares this transaction; never HTTP or effects.
			if err := connection.read(ctx, tx.db, uint(connection.engineID), uint(input.TenantID)); err != nil {
				return err
			}
			if err := recheck(); err != nil {
				return err
			}
		}
		details := map[string]any{"audience": execution.AudienceCleanup, "execution_id": child.ExecutionID, "cleanup_scope": scope, "stage": input.Stage, "attempt": input.Attempt}
		if connection != nil {
			details["purpose"], details["engine_id"] = execution.CleanupEngineConnectionPurpose, strconv.FormatInt(connection.engineID, 10)
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: input.Audit, EventName: "iam.execution_authorization.consumed", Result: AuditResultSucceeded, RiskLevel: "high", ModuleName: "system", EntityType: "execution_authorization", EntityID: strconv.FormatInt(authorization.ID, 10), Details: details}); err != nil {
			return err
		}
		if connection != nil {
			// Audit FK/row waits can cross the authority or parent/child lease window.
			// Roll back its audit too when no connection may be returned.
			if err := recheck(); err != nil {
				return err
			}
		}
		result = &AuthorizedCleanup{AuthorizationID: authorization.ID, ExecutionID: input.ExecutionID, TenantID: input.TenantID, Stage: input.Stage, Attempt: input.Attempt, Scope: scope, ExpiresAt: expires.UTC()}
		return nil
	})
	return result, err
}

// An Engine-purpose request never turns cleanup.execute into engine.delete.
// Current role facts are rechecked after the exact child lock at claim/effect.
func hasCleanupActorPermissions(rows []RoleAssignmentPermissionProjection, child execution.TaskExecution) bool {
	input, err := execution.CleanupRequestFromExecution(child)
	if err != nil {
		return false
	}
	if input.CauseEvent != events.CleanupCauseEngineDeleting && input.CauseEvent != events.CleanupCauseEngineDeleted {
		return hasCleanupPermission(rows)
	}
	for _, row := range rows {
		if row.PermissionKey == "system.engine.delete" {
			return true
		}
	}
	return false
}
