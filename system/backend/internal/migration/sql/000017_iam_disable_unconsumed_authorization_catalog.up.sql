BEGIN;

CREATE TEMP TABLE retired_permissions ON COMMIT DROP AS
SELECT id
FROM system.permissions
WHERE permission_key IN (
    'audit.report.create',
    'audit.report.read',
    'audit.report.update',
    'audit.subject.read',
    'audit.tenant_subject.read',
    'develop.notebook.update',
    'develop.task.cancel',
    'iam.department.create',
    'iam.department.delete',
    'iam.department.read',
    'iam.department.update',
    'iam.department_membership.close',
    'iam.department_membership.create',
    'iam.department_membership.read',
    'iam.department_membership.update',
    'iam.external_identity.link',
    'iam.external_identity.read',
    'iam.external_identity.suspend',
    'iam.external_identity.unlink',
    'iam.identity_provider.create',
    'iam.identity_provider.read',
    'iam.identity_provider.suspend',
    'iam.identity_provider.update',
    'iam.permission.read',
    'iam.platform_role_change.approve',
    'iam.platform_role_change.create',
    'iam.platform_role_change.read',
    'iam.platform_role_change.reject',
    'iam.project_group.close',
    'iam.project_group.create',
    'iam.project_group.read',
    'iam.project_group.update',
    'iam.project_group_membership.close',
    'iam.project_group_membership.create',
    'iam.project_group_membership.read',
    'iam.project_group_membership.update',
    'iam.role.read',
    'iam.security_policy.read',
    'iam.security_policy.update',
    'iam.session.read',
    'iam.session.revoke',
    'iam.tenant_idp_connection.create',
    'iam.tenant_idp_connection.read',
    'iam.tenant_idp_connection.suspend',
    'iam.tenant_idp_connection.update',
    'iam.tenant_role.create',
    'iam.tenant_role.delete',
    'iam.tenant_role.read',
    'iam.tenant_role.update',
    'iam.tenant_role_assignment.create',
    'iam.tenant_role_assignment.read',
    'iam.tenant_role_assignment.revoke',
    'manager.data_item.delete',
    'manager.resource_grant.create',
    'manager.resource_grant.read',
    'manager.resource_grant.revoke',
    'monitor.execution.cancel',
    'monitor.execution.retry',
    'monitor.statistics.export',
    'orchestrator.workflow.cancel',
    'platform.backup.execute',
    'platform.configuration.read',
    'platform.configuration.update',
    'platform.module.read',
    'platform.module.update',
    'platform.operation.read',
    'platform.restore_request.approve',
    'platform.restore_request.create',
    'platform.restore_request.execute',
    'platform.restore_request.read',
    'platform.restore_request.reject',
    'service.definition.offline',
    'service.definition.publish',
    'service.endpoint.read',
    'statistics.summary.read',
    'statistics.tenant_breakdown.export',
    'statistics.tenant_breakdown.read',
    'transfer.task.cancel'
);

DO $$
BEGIN
    IF (SELECT count(*) FROM retired_permissions) <> 78 THEN
        RAISE EXCEPTION 'authorization catalog retirement expected 78 permissions, found %',
            (SELECT count(*) FROM retired_permissions);
    END IF;
END;
$$;

CREATE TEMP TABLE affected_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id
FROM system.role_assignments AS assignment
JOIN system.role_permissions AS role_permission ON role_permission.role_id = assignment.role_id
JOIN retired_permissions AS retired ON retired.id = role_permission.permission_id
WHERE assignment.status = 'active';

DELETE FROM system.role_permissions AS role_permission
USING retired_permissions AS retired
WHERE role_permission.permission_id = retired.id;

UPDATE system.roles
SET status = 'disabled',
    updated_at = now()
WHERE tenant_id IS NULL
  AND role_key IN (
      'platform.statistics_viewer',
      'tenant.department_coordinator',
      'tenant.project_group_coordinator'
  )
  AND status = 'active';

UPDATE system.permissions AS permission
SET status = 'disabled',
    updated_at = now()
FROM retired_permissions AS retired
WHERE permission.id = retired.id
  AND permission.status = 'active';

UPDATE system.refresh_token_families AS family
SET revoked_at = now(),
    revoked_reason = 'authorization_catalog_changed',
    updated_at = now()
FROM affected_principals AS affected
WHERE family.principal_id = affected.principal_id
  AND family.revoked_at IS NULL;

COMMIT;
