BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'copilot', action, risk_level, false,
       allowed_scope_types, false,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', 'active'
FROM (VALUES
    ('copilot.configuration.read', 'read', 'low', ARRAY['platform', 'tenant']::text[]),
    ('copilot.configuration.update', 'update', 'medium', ARRAY['platform', 'tenant']::text[]),
    ('copilot.knowledge_graph.execute', 'execute', 'medium', ARRAY['tenant']::text[])
) AS seed(permission_key, action, risk_level, allowed_scope_types)
ORDER BY permission_key;

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
)
SELECT role_key, 'roles.' || role_key || '.name', 'roles.' || role_key || '.description',
       role_type, allowed_scope_types, ARRAY['service_principal']::text[], true, 'active'
FROM (VALUES
    ('platform.graph_runtime', 'platform_builtin', ARRAY['platform']::text[]),
    ('tenant.graph_runtime', 'tenant_builtin', ARRAY['tenant']::text[])
) AS seed(role_key, role_type, allowed_scope_types)
ORDER BY role_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('platform.graph_runtime', 'system.runtime_registry.update'),
    ('tenant.graph_runtime', 'copilot.knowledge_graph.execute'),
    ('platform.system_administrator', 'copilot.configuration.read'),
    ('platform.system_administrator', 'copilot.configuration.update'),
    ('tenant.administrator', 'copilot.configuration.read'),
    ('tenant.administrator', 'copilot.configuration.update')
) AS seed(role_key, permission_key)
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = seed.role_key AND role.status = 'active'
JOIN system.permissions permission
  ON permission.permission_key = seed.permission_key AND permission.status = 'active'
ORDER BY seed.role_key, seed.permission_key;

WITH principal AS (
    INSERT INTO system.principals (principal_type, status)
    VALUES ('service_principal', 'active')
    RETURNING id
), service_principal AS (
    INSERT INTO system.service_principals (
        id, name, description, owner_scope, created_by_principal_id
    )
    SELECT id, 'addp-graph', 'ADDP Graph runtime', 'platform', id FROM principal
    RETURNING id, name, description
)
INSERT INTO system.oauth_clients (
    client_id, display_name, client_type, client_secret_hash, service_principal_id,
    redirect_uris, grant_types, response_types, allowed_scopes, allowed_audiences,
    token_endpoint_auth_method, status
)
SELECT name, description, 'confidential', NULL, id,
       ARRAY[]::text[], ARRAY['client_credentials']::text[], ARRAY[]::text[],
       ARRAY['addp.api']::text[], ARRAY['addp.api']::text[],
       'client_secret_basic', 'disabled'
FROM service_principal;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, status, valid_from, source_type, reason
)
SELECT service_principal.id, role.id, 'platform', 'active', transaction_timestamp(),
       'bootstrap', 'built-in service control plane runtime'
FROM system.service_principals service_principal
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = 'platform.graph_runtime'
WHERE service_principal.name = 'addp-graph';

INSERT INTO system.tenant_memberships (
    tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id
)
SELECT tenant.id, service_principal.id, 'active', 'bootstrap', tenant.initialized_at,
       tenant.initialized_by_principal_id
FROM system.tenants tenant
JOIN system.service_principals service_principal ON service_principal.name = 'addp-graph'
WHERE tenant.initialized_at IS NOT NULL;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, tenant_id, status, valid_from,
    source_type, reason
)
SELECT service_principal.id, role.id, 'tenant', tenant.id, 'active', tenant.initialized_at,
       'bootstrap', 'built-in service runtime'
FROM system.tenants tenant
JOIN system.service_principals service_principal ON service_principal.name = 'addp-graph'
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = 'tenant.graph_runtime'
WHERE tenant.initialized_at IS NOT NULL;

COMMIT;
