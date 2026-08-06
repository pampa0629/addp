BEGIN;

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('tenant.manager_runtime'),
    ('tenant.meta_runtime'),
    ('tenant.transfer_runtime')
) AS seed(role_key)
JOIN system.roles AS role
  ON role.tenant_id IS NULL
 AND role.role_key = seed.role_key
 AND role.role_type = 'tenant_builtin'
 AND role.status = 'active'
JOIN system.permissions AS permission
  ON permission.permission_key = 'system.engine_descriptor.read'
ORDER BY seed.role_key;

COMMIT;
