BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
VALUES
    ('monitor.configuration.read', 'monitor', 'read', 'low', false, ARRAY['platform']::text[], false, 'permissions.monitor.configuration.read.name', 'permissions.monitor.configuration.read.description', 'active'),
    ('monitor.configuration.update', 'monitor', 'update', 'high', false, ARRAY['platform']::text[], false, 'permissions.monitor.configuration.update.name', 'permissions.monitor.configuration.update.description', 'active'),
    ('service.configuration.read', 'service', 'read', 'low', false, ARRAY['platform']::text[], false, 'permissions.service.configuration.read.name', 'permissions.service.configuration.read.description', 'active'),
    ('service.configuration.update', 'service', 'update', 'high', false, ARRAY['platform']::text[], false, 'permissions.service.configuration.update.name', 'permissions.service.configuration.update.description', 'active'),
    ('transfer.configuration.read', 'transfer', 'read', 'low', false, ARRAY['platform']::text[], false, 'permissions.transfer.configuration.read.name', 'permissions.transfer.configuration.read.description', 'active'),
    ('transfer.configuration.update', 'transfer', 'update', 'high', false, ARRAY['platform']::text[], false, 'permissions.transfer.configuration.update.name', 'permissions.transfer.configuration.update.description', 'active')
ON CONFLICT (permission_key) DO NOTHING;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('platform.system_administrator', 'monitor.configuration.read'),
    ('platform.system_administrator', 'monitor.configuration.update'),
    ('platform.system_administrator', 'service.configuration.read'),
    ('platform.system_administrator', 'service.configuration.update'),
    ('platform.system_administrator', 'transfer.configuration.read'),
    ('platform.system_administrator', 'transfer.configuration.update')
) AS seed(role_key, permission_key)
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = seed.role_key AND role.status = 'active'
JOIN system.permissions permission ON permission.permission_key = seed.permission_key AND permission.status = 'active'
ON CONFLICT DO NOTHING;

COMMIT;
