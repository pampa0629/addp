BEGIN;

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('tenant.data_architect', 'develop.data_write.execute'),
    ('tenant.data_architect', 'develop.task.execute')
) AS seed(role_key, permission_key)
JOIN system.roles AS role
  ON role.tenant_id IS NULL
 AND role.role_key = seed.role_key
 AND role.status = 'active'
JOIN system.permissions AS permission
  ON permission.permission_key = seed.permission_key
 AND permission.status = 'active'
ORDER BY seed.role_key, seed.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
