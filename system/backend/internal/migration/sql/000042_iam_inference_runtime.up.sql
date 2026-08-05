BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'inference', action, risk_level, false,
       allowed_scope_types, false,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', 'active'
FROM (VALUES
    ('inference.deployment.create', 'create', 'medium', ARRAY['platform', 'tenant']::text[]),
    ('inference.deployment.delete', 'delete', 'high', ARRAY['platform', 'tenant']::text[]),
    ('inference.deployment.execute', 'execute', 'medium', ARRAY['platform', 'tenant']::text[]),
    ('inference.deployment.read', 'read', 'low', ARRAY['platform', 'tenant']::text[]),
    ('inference.deployment.update', 'update', 'medium', ARRAY['platform', 'tenant']::text[]),
    ('inference.runtime.execute', 'execute', 'medium', ARRAY['tenant']::text[]),
    ('inference.profile.create', 'create', 'medium', ARRAY['platform', 'tenant']::text[]),
    ('inference.profile.read', 'read', 'low', ARRAY['platform', 'tenant']::text[]),
    ('inference.profile.update', 'update', 'medium', ARRAY['platform', 'tenant']::text[]),
    ('inference.provider.create', 'create', 'medium', ARRAY['platform', 'tenant']::text[]),
    ('inference.provider.delete', 'delete', 'high', ARRAY['platform', 'tenant']::text[]),
    ('inference.provider.read', 'read', 'low', ARRAY['platform', 'tenant']::text[]),
    ('inference.provider.update', 'update', 'medium', ARRAY['platform', 'tenant']::text[]),
    ('inference.provider_credential.update', 'update', 'high', ARRAY['platform', 'tenant']::text[])
) AS seed(permission_key, action, risk_level, allowed_scope_types)
ORDER BY permission_key;

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
)
SELECT role_key, 'roles.' || role_key || '.name', 'roles.' || role_key || '.description',
       role_type, allowed_scope_types, ARRAY['service_principal']::text[], true, 'active'
FROM (VALUES
    ('platform.agent_runtime', 'platform_builtin', ARRAY['platform']::text[]),
    ('platform.copilot_runtime', 'platform_builtin', ARRAY['platform']::text[]),
    ('platform.inference_runtime', 'platform_builtin', ARRAY['platform']::text[]),
    ('tenant.agent_runtime', 'tenant_builtin', ARRAY['tenant']::text[]),
    ('tenant.copilot_runtime', 'tenant_builtin', ARRAY['tenant']::text[])
) AS seed(role_key, role_type, allowed_scope_types)
ORDER BY role_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('platform.agent_runtime', 'system.runtime_registry.update'),
    ('platform.copilot_runtime', 'system.runtime_registry.update'),
    ('platform.inference_runtime', 'system.runtime_registry.update'),
    ('tenant.agent_runtime', 'inference.runtime.execute'),
    ('tenant.copilot_runtime', 'inference.runtime.execute'),
    ('tenant.manager_runtime', 'inference.runtime.execute')
) AS seed(role_key, permission_key)
JOIN system.roles role
  ON role.tenant_id IS NULL
 AND role.role_key = seed.role_key
 AND role.status = 'active'
JOIN system.permissions permission
  ON permission.permission_key = seed.permission_key
 AND permission.status = 'active'
ORDER BY seed.role_key, seed.permission_key;

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles role
JOIN system.permissions permission
  ON permission.owner_module = 'inference'
 AND permission.permission_key <> 'inference.runtime.execute'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('platform.system_administrator', 'tenant.administrator')
  AND role.status = 'active'
ORDER BY role.role_key, permission.permission_key;

DO $$
DECLARE
    service_name text;
    service_description text;
    service_id bigint;
BEGIN
    FOR service_name, service_description IN
        SELECT * FROM (VALUES
            ('addp-agent', 'ADDP Agent runtime'),
            ('addp-copilot', 'ADDP Copilot runtime'),
            ('addp-inference', 'ADDP Inference runtime')
        ) AS seed(name, description)
        ORDER BY name
    LOOP
        INSERT INTO system.principals (principal_type, status)
        VALUES ('service_principal', 'active')
        RETURNING id INTO service_id;

        INSERT INTO system.service_principals (
            id, name, description, owner_scope, created_by_principal_id
        ) VALUES (
            service_id, service_name, service_description, 'platform', service_id
        );
    END LOOP;
END;
$$;

INSERT INTO system.oauth_clients (
    client_id, display_name, client_type, client_secret_hash, service_principal_id,
    redirect_uris, grant_types, response_types, allowed_scopes, allowed_audiences,
    token_endpoint_auth_method, status
)
SELECT service_principal.name, service_principal.description, 'confidential', NULL,
       service_principal.id, ARRAY[]::text[], ARRAY['client_credentials']::text[],
       ARRAY[]::text[], ARRAY['addp.api']::text[], ARRAY['addp.api']::text[],
       'client_secret_basic', 'disabled'
FROM system.service_principals service_principal
WHERE service_principal.name IN ('addp-agent', 'addp-copilot', 'addp-inference')
ORDER BY service_principal.name;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, status, valid_from, source_type, reason
)
SELECT service_principal.id, role.id, 'platform', 'active', transaction_timestamp(),
       'bootstrap', 'built-in service control plane runtime'
FROM system.service_principals service_principal
JOIN system.roles role
  ON role.tenant_id IS NULL
 AND role.role_key = 'platform.' || replace(service_principal.name, 'addp-', '') || '_runtime'
WHERE service_principal.name IN ('addp-agent', 'addp-copilot', 'addp-inference')
ORDER BY service_principal.name;

INSERT INTO system.tenant_memberships (
    tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id
)
SELECT tenant.id, service_principal.id, 'active', 'bootstrap', tenant.initialized_at,
       tenant.initialized_by_principal_id
FROM system.tenants tenant
CROSS JOIN system.service_principals service_principal
WHERE tenant.initialized_at IS NOT NULL
  AND service_principal.name IN ('addp-agent', 'addp-copilot')
ORDER BY tenant.id, service_principal.name;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, tenant_id, status, valid_from,
    source_type, reason
)
SELECT service_principal.id, role.id, 'tenant', tenant.id, 'active',
       tenant.initialized_at, 'bootstrap', 'built-in service runtime'
FROM system.tenants tenant
CROSS JOIN system.service_principals service_principal
JOIN system.roles role
  ON role.tenant_id IS NULL
 AND role.role_key = 'tenant.' || replace(service_principal.name, 'addp-', '') || '_runtime'
WHERE tenant.initialized_at IS NOT NULL
  AND service_principal.name IN ('addp-agent', 'addp-copilot')
ORDER BY tenant.id, service_principal.name;

COMMIT;
