BEGIN;

UPDATE system.permissions
SET status = 'active'
WHERE permission_key = 'iam.department.read'
  AND status = 'disabled';

UPDATE system.permissions
SET status = 'active'
WHERE permission_key IN (
    'catalog.audit.read',
    'catalog.entry.certify',
    'catalog.entry.deprecate',
    'catalog.entry.update',
    'catalog.source.rebind'
)
  AND status = 'disabled';

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key IN ('iam.department.read', 'iam.tenant_membership.read')
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.catalog_runtime'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key IN (
      'catalog.audit.read',
      'catalog.entry.certify',
      'catalog.entry.deprecate',
      'catalog.entry.read',
      'catalog.entry.update',
      'catalog.inventory.read',
      'catalog.source.rebind'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.administrator'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

DELETE FROM system.role_permissions role_permission
USING system.roles role, system.permissions permission
WHERE role_permission.role_id = role.id
  AND role_permission.permission_id = permission.id
  AND role.tenant_id IS NULL
  AND role.role_key = 'platform.system_administrator'
  AND permission.owner_module = 'catalog';

COMMIT;
