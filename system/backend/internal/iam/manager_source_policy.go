package iam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type managerSourcePurpose string

const (
	managerSourceProfile managerSourcePurpose = "data_profiling"
	managerSourceModel3D managerSourcePurpose = "model_3d_glb_generation"
)

func (p managerSourcePurpose) permission() string {
	switch p {
	case managerSourceProfile:
		return "manager.data_profile.execute"
	case managerSourceModel3D:
		return "manager.derived_artifact.create"
	}
	return ""
}
func (p managerSourcePurpose) scope(raw json.RawMessage) (*ManagerSourceReadScope, error) {
	if p == managerSourceProfile {
		scope, err := managerProfileScope(raw)
		if err != nil {
			return nil, err
		}
		return NewManagerProfileSourceScope(scope), nil
	}
	if p != managerSourceModel3D {
		return nil, commonapi.ErrBadRequest
	}
	var fields struct {
		OwnerTask struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"owner_task"`
		EngineVersion string                                     `json:"source_engine_version"`
		Composition   authorizationfacts.FileCompositionBoundary `json:"source_composition"`
	}
	if json.Unmarshal(raw, &fields) != nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	scope, err := execution.NewManagerModel3DReadScope(raw, fields.OwnerTask.ID, fields.OwnerTask.Version, fields.EngineVersion, &fields.Composition)
	if err != nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return NewManagerModel3DSourceScope(scope), nil
}
func (p managerSourcePurpose) matches(scope *ManagerSourceReadScope) bool {
	return scope.Validate() == nil && ((p == managerSourceProfile && scope.Profile() != nil) || (p == managerSourceModel3D && scope.Model3D() != nil))
}

// System reads only the owner-frozen common execution. Owner task definitions
// remain private; no current definition can replace an authorized raw config.
func (r *Repository) managerSourceExecutionConfig(ctx context.Context, auth *ExecutionAuthorization, attempt int, lease uuid.UUID, pending bool, purpose managerSourcePurpose) (json.RawMessage, error) {
	if purpose.permission() == "" {
		return nil, commonapi.ErrBadRequest
	}
	var row struct{ ExecutionConfig json.RawMessage }
	query := r.db.WithContext(ctx).Table("common.task_executions e").Select("e.execution_config").
		Where("e.execution_id=? AND e.tenant_id=? AND e.module='manager' AND e.source='manager' AND e.task_type=? AND e.execution_boundary='bounded' AND e.trigger_type='manual' AND e.parent_execution_id IS NULL", auth.ExecutionID.String(), auth.TenantID, string(purpose)).
		Where("e.actor_principal_id=? AND e.actor_tenant_membership_id=? AND e.issued_authorization_version=?", auth.ActorPrincipalID, auth.TenantMembershipID, auth.IssuedAuthorizationVersion)
	if purpose == managerSourceProfile {
		query = query.Where("e.source_task_id IS NULL")
	} else {
		query = query.Where("e.source_task_id IS NOT NULL AND e.source_task_id=e.execution_config->'owner_task'->>'id' AND e.triggered_by=e.actor_principal_id")
	}
	if pending {
		query = query.Where("e.status='pending' AND e.attempt=0 AND e.execution_authorization_id IS NULL AND e.authorization_expires_at IS NULL AND e.lease_token IS NULL AND e.lease_owner IS NULL AND e.lease_expires_at IS NULL")
	} else {
		query = query.Where("e.status='running' AND e.attempt=? AND e.lease_token=? AND e.lease_owner IS NOT NULL AND length(btrim(e.lease_owner))>0 AND e.lease_expires_at>clock_timestamp() AND e.execution_authorization_id=? AND e.authorization_expires_at=? AND e.authorization_expires_at>clock_timestamp()", attempt, lease, auth.ID, auth.ExpiresAt)
	}
	result := query.Clauses(clause.Locking{Strength: "SHARE", Table: clause.Table{Name: "e"}}).Take(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) || (result.Error == nil && result.RowsAffected != 1) {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	if result.Error != nil {
		return nil, wrapRepositoryError(result.Error)
	}
	if _, err := purpose.scope(row.ExecutionConfig); err != nil {
		return nil, err
	}
	return row.ExecutionConfig, nil
}
func managerSourceUserCredential(auth *authorization.AuthContext, purpose managerSourcePurpose) bool {
	if purpose.permission() == "" || auth == nil || auth.Principal.Type != "user" || auth.Context.Type != "tenant" || auth.Delegation != nil ||
		len(auth.Client.Audiences) != 1 || auth.Client.Audiences[0] != "addp.api" ||
		!authorization.HasContextPermissions(*auth, purpose.permission(), "manager.data_item.read") {
		return false
	}
	if auth.Token.Type == "first_party_access_token" {
		return auth.Client.ScopeMode == "unrestricted" && len(auth.Client.Scopes) == 0
	}
	if auth.Token.Type == "oauth_access_token" && auth.Client.ScopeMode == "restricted" {
		for _, scope := range auth.Client.Scopes {
			if scope == "addp.api" {
				return true
			}
		}
	}
	return false
}

func managerSourcePermissions(rows []RoleAssignmentPermissionProjection, purpose managerSourcePurpose) bool {
	permissions := make(map[string]bool)
	for _, row := range rows {
		if row.ScopeType == "tenant" {
			permissions[row.PermissionKey] = true
		}
	}
	return permissions[purpose.permission()] && permissions["manager.data_item.read"]
}

func (r *Repository) currentManagerSourceConsumer(ctx context.Context, input authorizeManagerSourceInput) error {
	var id int64
	result := r.db.WithContext(ctx).Raw(`SELECT member.id FROM system.oauth_clients client
		JOIN system.principals principal ON principal.id=client.service_principal_id
		JOIN system.service_principals service ON service.id=principal.id
		JOIN system.tenant_memberships member ON member.principal_id=principal.id
		WHERE client.client_id='addp-manager' AND client.status='active' AND principal.id=? AND principal.status='active'
		AND service.name='addp-manager' AND service.owner_scope='platform' AND member.tenant_id=? AND member.status='active'
		AND (member.expires_at IS NULL OR member.expires_at>clock_timestamp())
		FOR SHARE OF client,principal,service,member`, input.ServicePrincipalID, input.TenantID).Scan(&id)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrExecutionAuthorizationUnavailable
	}
	now, err := r.internalTaskDatabaseTime(ctx)
	if err != nil {
		return err
	}
	permissions, err := r.ListEffectiveRoleAssignmentPermissions(ctx, input.ServicePrincipalID, PrincipalTypeServicePrincipal,
		ContextTypeTenant, &input.TenantID, &id, now)
	if err != nil {
		return err
	}
	for _, row := range permissions {
		if row.ScopeType == "tenant" && row.PermissionKey == "system.execution_authorization.execute" {
			return nil
		}
	}
	return fmt.Errorf("%w: Manager Runtime execution permission is required", ErrExecutionAuthorizationPermissionDenied)
}
