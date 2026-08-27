BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'catalog', action, risk_level, delegable,
       allowed_scope_types, true,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', status
FROM (VALUES
    ('catalog.audit.read', 'read', 'medium', false, ARRAY['tenant']::text[], 'disabled'),
    ('catalog.entry.certify', 'certify', 'high', false, ARRAY['tenant']::text[], 'disabled'),
    ('catalog.entry.deprecate', 'deprecate', 'high', false, ARRAY['tenant']::text[], 'disabled'),
    ('catalog.entry.read', 'read', 'low', true, ARRAY['tenant', 'department']::text[], 'active'),
    ('catalog.entry.update', 'update', 'medium', false, ARRAY['tenant', 'department']::text[], 'disabled'),
    ('catalog.inventory.read', 'read', 'medium', false, ARRAY['tenant']::text[], 'active'),
    ('catalog.source.rebind', 'rebind', 'high', false, ARRAY['tenant']::text[], 'disabled')
) AS seed(permission_key, action, risk_level, delegable, allowed_scope_types, status)
ORDER BY permission_key;

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
)
VALUES
    ('platform.catalog_runtime', 'roles.platform.catalog_runtime.name', 'roles.platform.catalog_runtime.description',
     'platform_builtin', ARRAY['platform']::text[], ARRAY['service_principal']::text[], true, 'active'),
    ('tenant.catalog_runtime', 'roles.tenant.catalog_runtime.name', 'roles.tenant.catalog_runtime.description',
     'tenant_builtin', ARRAY['tenant']::text[], ARRAY['service_principal']::text[], true, 'active');

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('platform.catalog_runtime', 'platform.tenant.read'),
    ('platform.catalog_runtime', 'system.runtime_registry.update'),
    ('tenant.catalog_runtime', 'meta.catalog.read'),
    ('tenant.catalog_runtime', 'standard.domain.read'),
    ('tenant.catalog_runtime', 'standard.element.read'),
    ('tenant.catalog_runtime', 'standard.glossary.read')
) AS seed(role_key, permission_key)
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = seed.role_key AND role.status = 'active'
JOIN system.permissions permission
  ON permission.permission_key = seed.permission_key AND permission.status = 'active'
ORDER BY seed.role_key, seed.permission_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles role
JOIN system.permissions permission
  ON permission.owner_module = 'catalog' AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.administrator'
  AND role.status = 'active'
ORDER BY role.role_key, permission.permission_key;

DO $$
DECLARE
    service_id bigint;
BEGIN
    INSERT INTO system.principals (principal_type, status)
    VALUES ('service_principal', 'active')
    RETURNING id INTO service_id;

    INSERT INTO system.service_principals (
        id, name, description, owner_scope, created_by_principal_id
    ) VALUES (
        service_id, 'addp-catalog', 'ADDP Catalog runtime', 'platform', service_id
    );
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
WHERE service_principal.name = 'addp-catalog';

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, status, valid_from, source_type, reason
)
SELECT service_principal.id, role.id, 'platform', 'active', transaction_timestamp(),
       'bootstrap', 'built-in Catalog control plane runtime'
FROM system.service_principals service_principal
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = 'platform.catalog_runtime'
WHERE service_principal.name = 'addp-catalog';

INSERT INTO system.tenant_memberships (
    tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id
)
SELECT tenant.id, service_principal.id, 'active', 'bootstrap', tenant.initialized_at,
       tenant.initialized_by_principal_id
FROM system.tenants tenant
CROSS JOIN system.service_principals service_principal
WHERE tenant.initialized_at IS NOT NULL
  AND service_principal.name = 'addp-catalog'
ORDER BY tenant.id;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, tenant_id, status, valid_from,
    source_type, reason
)
SELECT service_principal.id, role.id, 'tenant', tenant.id, 'active',
       tenant.initialized_at, 'bootstrap', 'built-in Catalog tenant runtime'
FROM system.tenants tenant
CROSS JOIN system.service_principals service_principal
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = 'tenant.catalog_runtime'
WHERE tenant.initialized_at IS NOT NULL
  AND service_principal.name = 'addp-catalog'
ORDER BY tenant.id;

COMMIT;
