BEGIN;

INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES ('catalog.sharing_fulfillment.read', 'catalog', 'read', 'high', false,
    ARRAY['tenant']::text[], false,
    'permissions.catalog.sharing_fulfillment.read.name',
    'permissions.catalog.sharing_fulfillment.read.description', 'active');

INSERT INTO system.roles (role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status)
VALUES ('tenant.system_runtime', 'roles.tenant.system_runtime.name', 'roles.tenant.system_runtime.description',
    'tenant_builtin', ARRAY['tenant']::text[], ARRAY['service_principal']::text[], true, 'active');

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT r.id, p.id, 'product' FROM system.roles r JOIN system.permissions p ON p.permission_key = 'catalog.sharing_fulfillment.read'
WHERE r.tenant_id IS NULL AND r.role_key = 'tenant.system_runtime';

DO $$ DECLARE service_id bigint; BEGIN
    INSERT INTO system.principals (principal_type, status) VALUES ('service_principal', 'active') RETURNING id INTO service_id;
    INSERT INTO system.service_principals (id, name, description, owner_scope, created_by_principal_id)
    VALUES (service_id, 'addp-system', 'ADDP System Catalog basis reader', 'platform', service_id);
END $$;

INSERT INTO system.oauth_clients (client_id, display_name, client_type, client_secret_hash, service_principal_id,
    redirect_uris, grant_types, response_types, allowed_scopes, allowed_audiences, token_endpoint_auth_method, status)
SELECT name, description, 'confidential', NULL, id, ARRAY[]::text[], ARRAY['client_credentials']::text[],
    ARRAY[]::text[], ARRAY['addp.api']::text[], ARRAY['addp.api']::text[], 'client_secret_basic', 'disabled'
FROM system.service_principals WHERE name = 'addp-system';

INSERT INTO system.tenant_memberships (tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id)
SELECT t.id, s.id, 'active', 'bootstrap', t.initialized_at, t.initialized_by_principal_id
FROM system.tenants t CROSS JOIN system.service_principals s WHERE t.initialized_at IS NOT NULL AND s.name = 'addp-system' ORDER BY t.id;

INSERT INTO system.role_assignments (principal_id, role_id, scope_type, tenant_id, status, valid_from, source_type, grant_reason)
SELECT s.id, r.id, 'tenant', t.id, 'active', t.initialized_at, 'bootstrap', 'built-in System Catalog basis reader'
FROM system.tenants t CROSS JOIN system.service_principals s JOIN system.roles r ON r.tenant_id IS NULL AND r.role_key = 'tenant.system_runtime'
WHERE t.initialized_at IS NOT NULL AND s.name = 'addp-system' ORDER BY t.id;

COMMIT;
