BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'transfer.execution.read', 'transfer', 'read', 'low', false,
    ARRAY['tenant']::text[], false,
    'permissions.transfer.execution.read.name',
    'permissions.transfer.execution.read.description', 'active'
)
ON CONFLICT (permission_key) DO UPDATE SET
    owner_module = EXCLUDED.owner_module,
    action = EXCLUDED.action,
    risk_level = EXCLUDED.risk_level,
    delegable = EXCLUDED.delegable,
    allowed_scope_types = EXCLUDED.allowed_scope_types,
    tenant_customizable = EXCLUDED.tenant_customizable,
    name_i18n_key = EXCLUDED.name_i18n_key,
    description_i18n_key = EXCLUDED.description_i18n_key,
    status = EXCLUDED.status,
    updated_at = transaction_timestamp();

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'transfer.execution.read'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.develop_runtime', 'tenant.manager_runtime')
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
