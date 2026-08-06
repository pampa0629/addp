BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
VALUES
    ('develop.configuration.read', 'develop', 'read', 'low', false, ARRAY['platform', 'tenant']::text[], false, 'permissions.develop.configuration.read.name', 'permissions.develop.configuration.read.description', 'active'),
    ('develop.configuration.update', 'develop', 'update', 'high', false, ARRAY['platform', 'tenant']::text[], false, 'permissions.develop.configuration.update.name', 'permissions.develop.configuration.update.description', 'active')
ON CONFLICT (permission_key) DO NOTHING;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('platform.system_administrator', 'develop.configuration.read'),
    ('platform.system_administrator', 'develop.configuration.update'),
    ('tenant.administrator', 'develop.configuration.read'),
    ('tenant.administrator', 'develop.configuration.update')
) AS seed(role_key, permission_key)
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = seed.role_key AND role.status = 'active'
JOIN system.permissions permission ON permission.permission_key = seed.permission_key AND permission.status = 'active'
ON CONFLICT DO NOTHING;

COMMIT;
