BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'agent', action, risk_level, false,
       ARRAY['platform', 'tenant']::text[], false,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', 'active'
FROM (VALUES
    ('agent.configuration.read', 'read', 'low'),
    ('agent.configuration.update', 'update', 'medium')
) AS seed(permission_key, action, risk_level)
ORDER BY permission_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('platform.system_administrator', 'agent.configuration.read'),
    ('platform.system_administrator', 'agent.configuration.update'),
    ('tenant.administrator', 'agent.configuration.read'),
    ('tenant.administrator', 'agent.configuration.update')
) AS seed(role_key, permission_key)
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = seed.role_key AND role.status = 'active'
JOIN system.permissions permission
  ON permission.permission_key = seed.permission_key AND permission.status = 'active'
ORDER BY seed.role_key, seed.permission_key;

COMMIT;
