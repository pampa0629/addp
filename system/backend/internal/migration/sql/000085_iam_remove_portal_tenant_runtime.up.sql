BEGIN;

UPDATE system.role_assignments assignment
SET status = 'revoked',
    revoked_by_principal_id = assignment.principal_id,
    revoked_at = GREATEST(transaction_timestamp(), assignment.created_at),
    updated_at = transaction_timestamp()
FROM system.roles role, system.service_principals service_principal
WHERE assignment.role_id = role.id
  AND assignment.principal_id = service_principal.id
  AND role.tenant_id IS NULL
  AND role.role_key = 'tenant.portal_runtime'
  AND service_principal.name = 'addp-portal'
  AND assignment.status = 'active';

DELETE FROM system.role_permissions role_permission
USING system.roles role
WHERE role_permission.role_id = role.id
  AND role.tenant_id IS NULL
  AND role.role_key = 'tenant.portal_runtime';

UPDATE system.roles
SET status = 'disabled', updated_at = transaction_timestamp()
WHERE tenant_id IS NULL
  AND role_key = 'tenant.portal_runtime'
  AND status <> 'disabled';

COMMIT;
