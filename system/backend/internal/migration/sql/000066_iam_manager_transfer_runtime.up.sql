BEGIN;

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('transfer.task.create'),
    ('transfer.task.execute')
) AS seed(permission_key)
JOIN system.roles AS role
  ON role.tenant_id IS NULL
 AND role.role_key = 'tenant.manager_runtime'
 AND role.role_type = 'tenant_builtin'
 AND role.status = 'active'
JOIN system.permissions AS permission
  ON permission.permission_key = seed.permission_key
 AND permission.status = 'active'
ORDER BY seed.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
