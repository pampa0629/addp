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
  ON permission.permission_key IN (
      'standard.dimension_hierarchy.read',
      'standard.domain.read',
      'standard.element.read',
      'standard.metric.read'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.data_architect'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ORDER BY permission.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
