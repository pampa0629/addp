BEGIN;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'develop.task.read'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.copilot_runtime'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active';

COMMIT;
