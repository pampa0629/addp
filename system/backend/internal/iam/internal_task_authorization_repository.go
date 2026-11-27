package iam

import (
	"context"
	"time"
)

func (r *Repository) internalTaskDatabaseTime(ctx context.Context) (time.Time, error) {
	if r.db.Dialector.Name() == "mysql" {
		return r.CurrentDatabaseTime(ctx)
	}
	var now time.Time
	err := r.db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&now).Error
	return now, wrapRepositoryError(err)
}

func (r *Repository) lockInternalTaskExecution(ctx context.Context, authorization *ExecutionAuthorization, input AuthorizeInternalTaskInput) (time.Time, error) {
	var row struct {
		LeaseExpiresAt      time.Time
		MembershipExpiresAt *time.Time
	}
	config := "jsonb_build_object('ontology_id', ?::text, 'revision', ?::text, 'digest', ?::text, 'generation', ?::text)"
	now := "clock_timestamp()"
	if r.db.Dialector.Name() == "mysql" {
		config = "JSON_OBJECT('ontology_id', ?, 'revision', ?, 'digest', ?, 'generation', ?)"
		now = "CURRENT_TIMESTAMP(6)"
	}
	result := r.db.WithContext(ctx).Raw(`
		SELECT e.lease_expires_at, membership.expires_at AS membership_expires_at FROM common.task_executions e
		JOIN system.oauth_clients client ON client.client_id = ?
		JOIN system.principals consumer ON consumer.id = client.service_principal_id
		JOIN system.service_principals service ON service.id = consumer.id
		JOIN system.tenant_memberships membership ON membership.principal_id = consumer.id AND membership.tenant_id = e.tenant_id
		WHERE e.execution_id = ? AND e.tenant_id = ? AND e.module = 'ontology' AND e.source = 'ontology'
		AND e.task_type = ? AND e.execution_boundary = 'bounded'
		AND e.execution_config = `+config+`
		AND e.actor_principal_id = ? AND e.actor_tenant_membership_id = ? AND e.issued_authorization_version = ?
		AND e.execution_authorization_id = ? AND e.authorization_expires_at = ?
		AND e.authorization_expires_at > `+now+`
		AND e.status = 'running' AND e.attempt = ? AND e.lease_token = ? AND e.lease_expires_at > `+now+`
		AND client.status = 'active' AND consumer.id = ? AND consumer.status = 'active'
		AND service.owner_scope = 'platform' AND service.name = 'addp-ontology'
		AND membership.status = 'active' AND (membership.expires_at IS NULL OR membership.expires_at > `+now+`)
		FOR SHARE`, input.ServiceClientID, input.ExecutionID, input.TenantID,
		input.InternalTask.TaskType, input.InternalTask.ResourceID, input.InternalTask.Revision, input.InternalTask.Digest, input.InternalTask.Generation,
		authorization.ActorPrincipalID, authorization.TenantMembershipID, authorization.IssuedAuthorizationVersion,
		authorization.ID, authorization.ExpiresAt, input.Attempt, input.LeaseToken, input.ServicePrincipalID).Scan(&row)
	if result.Error != nil {
		return time.Time{}, wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return time.Time{}, ErrExecutionAuthorizationUnavailable
	}
	if row.MembershipExpiresAt != nil && row.MembershipExpiresAt.Before(row.LeaseExpiresAt) {
		return *row.MembershipExpiresAt, nil
	}
	return row.LeaseExpiresAt, nil
}
