package iam

import (
	"context"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

type ResolveExecutionSubjectRolesInput struct {
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	Attempt            int
	LeaseToken         uuid.UUID
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

func (s *ExecutionAuthorizationService) ResolveSubjectRoles(ctx context.Context, in ResolveExecutionSubjectRolesInput) (*execution.SubjectRoles, error) {
	if s == nil || s.repository == nil || in.AuthorizationID <= 0 || in.ExecutionID == uuid.Nil || in.Attempt <= 0 || in.LeaseToken == uuid.Nil || in.TenantID <= 0 || in.ServicePrincipalID <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	owner := ""
	switch in.ServiceClientID {
	case "addp-develop":
		owner = "develop"
	case "addp-transfer":
		owner = "transfer"
	default:
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	if in.Audit.PrincipalID == nil || *in.Audit.PrincipalID != in.ServicePrincipalID || in.Audit.PrincipalType == nil || *in.Audit.PrincipalType != PrincipalTypeServicePrincipal || in.Audit.ContextType == nil || *in.Audit.ContextType != ContextTypeTenant || in.Audit.TenantID == nil || *in.Audit.TenantID != in.TenantID {
		return nil, commonapi.ErrBadRequest
	}
	var result *execution.SubjectRoles
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		a, err := tx.LockExecutionAuthorization(ctx, in.AuthorizationID)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		if a.TenantID != in.TenantID || a.ExecutionID != in.ExecutionID || (a.Audience != owner && !(owner == "develop" && a.Audience == "duckdb")) || a.SourceType != executionAuthorizationSourceUser || a.SourceNotebookSessionAuthorizationID != nil || a.InternalTask != nil || a.SealedAt == nil || a.RevokedAt != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		if (a.SourceExecutionAttempt == nil) != (a.SourceExecutionLeaseToken == nil) || (a.SourceExecutionAttempt != nil && (*a.SourceExecutionAttempt != in.Attempt || *a.SourceExecutionLeaseToken != in.LeaseToken)) {
			return ErrExecutionAuthorizationUnavailable
		}
		principal, membership, now, err := lockAndValidateExecutionActor(ctx, tx, a.ActorPrincipalID, a.TenantMembershipID, a.TenantID, a.IssuedAuthorizationVersion)
		if err != nil {
			return err
		}
		claim, err := tx.lockSubjectRoleExecution(ctx, a, owner, in)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		expires := a.ExpiresAt.UTC()
		if membership.ExpiresAt != nil && membership.ExpiresAt.Before(expires) {
			expires = membership.ExpiresAt.UTC()
		}
		if claim.LeaseExpiresAt.Before(expires) {
			expires = claim.LeaseExpiresAt.UTC()
		}
		// Re-read database time after any execution-lock wait. No transaction-start clock.
		now, err = tx.CurrentDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if !expires.After(now) {
			return ErrExecutionAuthorizationUnavailable
		}
		rows, err := tx.listEffectiveSubjectRoles(ctx, principal.ID, membership.TenantID, now)
		if err != nil {
			return err
		}
		keys := make([]string, 0, len(rows))
		for _, row := range rows {
			if len(keys) == 0 || keys[len(keys)-1] != row.RoleKey {
				keys = append(keys, row.RoleKey)
			}
			if row.ValidUntil != nil && row.ValidUntil.Before(expires) {
				expires = row.ValidUntil.UTC()
			}
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: in.Audit, EventName: "iam.execution_authorization.subject_roles_observed", Result: AuditResultSucceeded, RiskLevel: executionAuthorizationRisk([]string{"read"}), ModuleName: "system", EntityType: "execution_authorization", EntityID: strconv.FormatInt(a.ID, 10),
			Details: map[string]any{"execution_id": a.ExecutionID.String(), "attempt": in.Attempt, "owner": owner, "source_principal_id": strconv.FormatInt(principal.ID, 10)},
		}); err != nil {
			return err
		}
		now, err = tx.CurrentDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if !expires.After(now) {
			return ErrExecutionAuthorizationUnavailable
		}
		result = &execution.SubjectRoles{AuthorizationID: strconv.FormatInt(a.ID, 10), ExecutionID: a.ExecutionID.String(), Attempt: in.Attempt, Owner: owner, TenantID: strconv.FormatInt(a.TenantID, 10), ActorPrincipalID: strconv.FormatInt(a.ActorPrincipalID, 10), TenantMembershipID: strconv.FormatInt(a.TenantMembershipID, 10), AuthorizationVersion: strconv.FormatInt(a.IssuedAuthorizationVersion, 10), RoleKeys: keys, ObservedAt: now, ExpiresAt: expires}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type subjectRoleExecution struct {
	ExecutionID       uuid.UUID
	ParentExecutionID *uuid.UUID
	LeaseExpiresAt    time.Time
}

func (r *Repository) lockSubjectRoleExecution(ctx context.Context, a *ExecutionAuthorization, owner string, in ResolveExecutionSubjectRolesInput) (*subjectRoleExecution, error) {
	var row subjectRoleExecution
	err := r.db.WithContext(ctx).Table("common.task_executions").Clauses(clause.Locking{Strength: "SHARE"}).
		Select("execution_id, parent_execution_id, lease_expires_at").
		Where("execution_id = ? AND execution_authorization_id = ? AND tenant_id = ? AND module = ? AND status = 'running' AND attempt = ? AND lease_token = ? AND actor_principal_id = ? AND actor_tenant_membership_id = ? AND issued_authorization_version = ?", in.ExecutionID, a.ID, a.TenantID, owner, in.Attempt, in.LeaseToken, a.ActorPrincipalID, a.TenantMembershipID, a.IssuedAuthorizationVersion).Take(&row).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	if a.SourceExecutionAttempt != nil && row.ParentExecutionID == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	if row.ParentExecutionID != nil {
		var parent subjectRoleExecution
		err = r.db.WithContext(ctx).Table("common.task_executions").Clauses(clause.Locking{Strength: "SHARE"}).Select("execution_id").
			Where("execution_id = ? AND tenant_id = ? AND module = 'orchestrator' AND status = 'running' AND actor_principal_id = ? AND actor_tenant_membership_id = ? AND issued_authorization_version = ?", *row.ParentExecutionID, a.TenantID, a.ActorPrincipalID, a.TenantMembershipID, a.IssuedAuthorizationVersion).Take(&parent).Error
		if err != nil {
			return nil, wrapRepositoryError(err)
		}
	}
	return &row, nil
}

type effectiveSubjectRole struct {
	RoleKey    string
	ValidUntil *time.Time
}

// Role eligibility must not disappear merely because a role has no Permission.
func (r *Repository) listEffectiveSubjectRoles(ctx context.Context, principalID, tenantID int64, now time.Time) ([]effectiveSubjectRole, error) {
	var rows []effectiveSubjectRole
	err := r.db.WithContext(ctx).Raw(`SELECT role.role_key, assignment.valid_until
 FROM system.role_assignments assignment JOIN system.roles role ON role.id = assignment.role_id
 WHERE assignment.principal_id = ? AND assignment.tenant_id = ? AND assignment.scope_type = 'tenant'
 AND assignment.status = 'active' AND assignment.valid_from <= ? AND (assignment.valid_until IS NULL OR assignment.valid_until > ?)
 AND role.status = 'active' AND ? = ANY(role.allowed_principal_types)
 ORDER BY role.role_key, assignment.id`, principalID, tenantID, now, now, PrincipalTypeUser).Scan(&rows).Error
	return rows, wrapRepositoryError(err)
}
