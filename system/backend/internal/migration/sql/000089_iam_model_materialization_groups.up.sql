BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT seed.permission_key, 'model', seed.action, seed.risk_level, false,
       ARRAY['tenant']::text[], true, seed.name_key, seed.description_key, 'active'
FROM (VALUES
    ('model.materialization_group.create', 'create', 'medium', 'permissions.model.materialization_group.create.name', 'permissions.model.materialization_group.create.description'),
    ('model.materialization_group.delete', 'delete', 'high', 'permissions.model.materialization_group.delete.name', 'permissions.model.materialization_group.delete.description'),
    ('model.materialization_group.read', 'read', 'low', 'permissions.model.materialization_group.read.name', 'permissions.model.materialization_group.read.description'),
    ('model.materialization_group.update', 'update', 'medium', 'permissions.model.materialization_group.update.name', 'permissions.model.materialization_group.update.description')
) AS seed(permission_key, action, risk_level, name_key, description_key)
ON CONFLICT (permission_key) DO UPDATE
SET owner_module = EXCLUDED.owner_module,
    action = EXCLUDED.action,
    risk_level = EXCLUDED.risk_level,
    delegable = EXCLUDED.delegable,
    allowed_scope_types = EXCLUDED.allowed_scope_types,
    tenant_customizable = EXCLUDED.tenant_customizable,
    name_i18n_key = EXCLUDED.name_i18n_key,
    description_i18n_key = EXCLUDED.description_i18n_key,
    status = 'active',
    updated_at = NOW();

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'model.materialization_group.create',
      'model.materialization_group.delete',
      'model.materialization_group.read',
      'model.materialization_group.update'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.data_architect'
  AND role.status = 'active'
ORDER BY permission.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
