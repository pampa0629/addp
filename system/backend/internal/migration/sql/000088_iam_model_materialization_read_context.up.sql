BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'model.materialization_read.execute', 'model', 'execute', 'medium', false,
    ARRAY['tenant']::text[], false,
    'permissions.model.materialization_read.execute.name',
    'permissions.model.materialization_read.execute.description', 'active'
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
    status = 'active',
    updated_at = NOW();

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('tenant.develop_runtime', 'model.materialization_read.execute'),
    ('tenant.quality_runtime', 'model.materialization_read.execute')
) AS seed(role_key, permission_key)
JOIN system.roles AS role
  ON role.tenant_id IS NULL
 AND role.role_key = seed.role_key
 AND role.status = 'active'
JOIN system.permissions AS permission
  ON permission.permission_key = seed.permission_key
 AND permission.status = 'active'
ORDER BY seed.role_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
