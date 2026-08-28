BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'asset', action, risk_level, false,
       ARRAY['tenant', 'department', 'project_group']::text[], true,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', 'active'
FROM (VALUES
    ('asset.category.create', 'create', 'medium'),
    ('asset.category.delete', 'delete', 'high'),
    ('asset.category.read', 'read', 'low'),
    ('asset.category.update', 'update', 'medium')
) AS seed(permission_key, action, risk_level)
ORDER BY permission_key
ON CONFLICT (permission_key) DO NOTHING;

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id, created_at
)
SELECT old_binding.role_id,
       new_permission.id,
       old_binding.source_type,
       old_binding.created_by_principal_id,
       old_binding.created_at
FROM system.role_permissions AS old_binding
JOIN system.permissions AS old_permission
  ON old_permission.id = old_binding.permission_id
JOIN system.permissions AS new_permission
  ON new_permission.permission_key = replace(old_permission.permission_key, 'asset.catalog.', 'asset.category.')
WHERE old_permission.permission_key IN (
    'asset.catalog.create',
    'asset.catalog.delete',
    'asset.catalog.read',
    'asset.catalog.update'
)
ON CONFLICT (role_id, permission_id) DO NOTHING;

DELETE FROM system.role_permissions AS old_binding
USING system.permissions AS old_permission
WHERE old_binding.permission_id = old_permission.id
  AND old_permission.permission_key IN (
      'asset.catalog.create',
      'asset.catalog.delete',
      'asset.catalog.read',
      'asset.catalog.update'
  );

UPDATE system.permissions
SET status = 'disabled',
    updated_at = transaction_timestamp()
WHERE permission_key IN (
    'asset.catalog.create',
    'asset.catalog.delete',
    'asset.catalog.read',
    'asset.catalog.update'
)
  AND status <> 'disabled';

COMMIT;
