BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
VALUES (
    'catalog.reference.read', 'catalog', 'read', 'medium', false,
    ARRAY['tenant']::text[], false,
    'permissions.catalog.reference.read.name',
    'permissions.catalog.reference.read.description', 'active'
)
ON CONFLICT (permission_key) DO UPDATE
SET owner_module = EXCLUDED.owner_module,
    action = EXCLUDED.action,
    risk_level = EXCLUDED.risk_level,
    delegable = EXCLUDED.delegable,
    allowed_scope_types = EXCLUDED.allowed_scope_types,
    tenant_customizable = EXCLUDED.tenant_customizable,
    name_i18n_key = EXCLUDED.name_i18n_key,
    description_i18n_key = EXCLUDED.description_i18n_key,
    status = EXCLUDED.status;

DELETE FROM system.role_permissions role_permission
USING system.roles role, system.permissions permission
WHERE role_permission.role_id = role.id
  AND role_permission.permission_id = permission.id
  AND role.tenant_id IS NULL
  AND role.role_key = 'tenant.asset_runtime'
  AND permission.permission_key IN (
      'develop.task.read',
      'meta.catalog.read',
      'service.definition.read',
      'standard.metric.read'
  );

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'catalog.reference.read'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.asset_runtime'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'catalog.reference.read'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.administrator'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
