BEGIN;

UPDATE system.permissions
SET allowed_scope_types = ARRAY['platform', 'tenant']::text[],
    updated_at = transaction_timestamp()
WHERE permission_key IN (
    'manager.configuration.read',
    'manager.configuration.update'
)
  AND owner_module = 'manager'
  AND status = 'active';

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key IN (
      'manager.configuration.read',
      'manager.configuration.update'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.administrator'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ORDER BY permission.permission_key;

COMMIT;
