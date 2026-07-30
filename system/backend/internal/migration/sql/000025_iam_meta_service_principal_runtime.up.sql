BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES
    (
        'audit.tenant_event.create', 'system', 'create', 'medium', false,
        ARRAY['tenant']::text[], false,
        'permissions.audit.tenant_event.create.name',
        'permissions.audit.tenant_event.create.description', 'active'
    ),
    (
        'system.runtime_registry.update', 'system', 'update', 'medium', false,
        ARRAY['platform']::text[], false,
        'permissions.system.runtime_registry.update.name',
        'permissions.system.runtime_registry.update.description', 'active'
    );

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
) VALUES
    (
        'tenant.meta_runtime', 'roles.tenant.meta_runtime.name',
        'roles.tenant.meta_runtime.description', 'tenant_builtin',
        ARRAY['tenant']::text[], ARRAY['service_principal']::text[], true, 'active'
    ),
    (
        'platform.meta_runtime', 'roles.platform.meta_runtime.name',
        'roles.platform.meta_runtime.description', 'platform_builtin',
        ARRAY['platform']::text[], ARRAY['service_principal']::text[], true, 'active'
    );

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('tenant.meta_runtime', 'audit.tenant_event.create'),
    ('tenant.meta_runtime', 'system.engine.read'),
    ('platform.meta_runtime', 'system.runtime_registry.update')
) AS seed(role_key, permission_key)
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = seed.role_key
JOIN system.permissions permission ON permission.permission_key = seed.permission_key
ORDER BY seed.role_key, seed.permission_key;

WITH principal AS (
    INSERT INTO system.principals (principal_type, status)
    VALUES ('service_principal', 'active')
    RETURNING id
)
INSERT INTO system.service_principals (
    id, name, description, owner_scope, created_by_principal_id
)
SELECT id, 'addp-meta', 'ADDP Meta runtime', 'platform', id
FROM principal;

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
WHERE service_principal.name = 'addp-meta';

INSERT INTO system.tenant_memberships (
    tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id
)
SELECT tenant.id, service_principal.id, 'active', 'bootstrap', tenant.initialized_at,
       tenant.initialized_by_principal_id
FROM system.tenants tenant
CROSS JOIN system.service_principals service_principal
WHERE tenant.initialized_at IS NOT NULL
  AND service_principal.name = 'addp-meta'
ORDER BY tenant.id;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, tenant_id, status, valid_from,
    source_type, reason
)
SELECT service_principal.id, role.id, 'tenant', tenant.id, 'active',
       tenant.initialized_at, 'bootstrap', 'built-in service runtime'
FROM system.tenants tenant
JOIN system.service_principals service_principal ON service_principal.name = 'addp-meta'
JOIN system.roles role
  ON role.tenant_id IS NULL
 AND role.role_key = 'tenant.meta_runtime'
WHERE tenant.initialized_at IS NOT NULL
ORDER BY tenant.id;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, status, valid_from,
    source_type, reason
)
SELECT service_principal.id, role.id, 'platform', 'active', transaction_timestamp(),
       'bootstrap', 'built-in service control plane runtime'
FROM system.service_principals service_principal
JOIN system.roles role
  ON role.tenant_id IS NULL
 AND role.role_key = 'platform.meta_runtime'
WHERE service_principal.name = 'addp-meta';

DO $$
BEGIN
    IF to_regclass('common.task_executions') IS NOT NULL THEN
        UPDATE common.task_executions
        SET execution_config = execution_config - 'token',
            updated_at = transaction_timestamp()
        WHERE module = 'meta'
          AND execution_config ? 'token';
    END IF;
END;
$$;

COMMIT;
