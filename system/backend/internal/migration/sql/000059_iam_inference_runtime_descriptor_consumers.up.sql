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
  ON permission.permission_key = 'system.engine_descriptor.read'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.agent_runtime', 'tenant.copilot_runtime')
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ORDER BY role.role_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
