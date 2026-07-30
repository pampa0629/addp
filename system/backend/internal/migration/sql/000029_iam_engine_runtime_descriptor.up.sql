BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, tenant_customizable,
    allowed_scope_types, delegable, name_i18n_key, description_i18n_key, status
) VALUES (
    'system.engine_descriptor.read', 'system', 'read', 'low', false,
    ARRAY['tenant']::text[], false,
    'permissions.system.engine_descriptor.read.name',
    'permissions.system.engine_descriptor.read.description', 'active'
);

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'system.engine_descriptor.read'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.develop_runtime'
  AND role.status = 'active';

COMMIT;
