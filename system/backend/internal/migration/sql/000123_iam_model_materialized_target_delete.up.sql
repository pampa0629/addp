BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'model.materialized_target.delete', 'model', 'delete', 'critical', false,
    ARRAY['tenant']::text[], true,
    'permissions.model.materialized_target.delete.name',
    'permissions.model.materialized_target.delete.description', 'active'
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

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles role
JOIN system.permissions permission ON permission.permission_key = 'model.materialized_target.delete'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.data_architect'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
