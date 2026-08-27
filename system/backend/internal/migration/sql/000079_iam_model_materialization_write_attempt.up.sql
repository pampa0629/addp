BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'model.materialization_write.execute', 'model', 'execute', 'high', false,
    ARRAY['tenant']::text[], false,
    'permissions.model.materialization_write.execute.name',
    'permissions.model.materialization_write.execute.description', 'active'
)
ON CONFLICT (permission_key) DO UPDATE
SET risk_level = EXCLUDED.risk_level,
    delegable = EXCLUDED.delegable,
    allowed_scope_types = EXCLUDED.allowed_scope_types,
    tenant_customizable = EXCLUDED.tenant_customizable,
    name_i18n_key = EXCLUDED.name_i18n_key,
    description_i18n_key = EXCLUDED.description_i18n_key,
    status = 'active',
    updated_at = NOW();

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('tenant.develop_runtime', 'model.materialization_write.execute'),
    ('tenant.transfer_runtime', 'model.materialization_write.execute')
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

DELETE FROM system.role_permissions
WHERE permission_id IN (
    SELECT id
    FROM system.permissions
    WHERE permission_key = 'model.materialization_context.read'
);

UPDATE system.permissions
SET status = 'disabled', updated_at = NOW()
WHERE permission_key = 'model.materialization_context.read';

COMMIT;
