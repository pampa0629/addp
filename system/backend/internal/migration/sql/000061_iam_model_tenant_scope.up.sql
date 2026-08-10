BEGIN;

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key LIKE 'model.%'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.data_architect'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ORDER BY permission.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

DELETE FROM system.role_permissions AS role_permission
USING system.roles AS role, system.permissions AS permission
WHERE role_permission.role_id = role.id
  AND role_permission.permission_id = permission.id
  AND role.tenant_id IS NULL
  AND role.role_key = 'tenant.governance_manager'
  AND permission.permission_key LIKE 'model.%';

COMMIT;
