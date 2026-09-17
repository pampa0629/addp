BEGIN;

-- The owner manifest defines the capability. No existing User role gains it
-- implicitly; tenant administrators may assign it through a custom role.
INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status
) VALUES (
    'ontology.revision.publish', 'ontology', 'publish', 'high', false,
    ARRAY['tenant']::text[], true,
    'permissions.ontology.revision.publish.name', 'permissions.ontology.revision.publish.description', 'active'
);

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
) VALUES (
    'tenant.ontology_runtime', 'roles.tenant.ontology_runtime.name', 'roles.tenant.ontology_runtime.description',
    'tenant_builtin', ARRAY['tenant']::text[], ARRAY['service_principal']::text[], true, 'active'
);

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role JOIN system.permissions permission ON permission.permission_key = 'system.execution_authorization.execute'
WHERE role.role_key = 'tenant.ontology_runtime' AND role.tenant_id IS NULL;

WITH principal AS (
    INSERT INTO system.principals (principal_type, status) VALUES ('service_principal','active') RETURNING id
), service_principal AS (
    INSERT INTO system.service_principals (id, name, description, owner_scope, created_by_principal_id)
    SELECT id, 'addp-ontology', 'ADDP Ontology projection runtime', 'platform', id FROM principal RETURNING id
)
INSERT INTO system.oauth_clients (
    client_id, display_name, client_type, client_secret_hash, service_principal_id,
    redirect_uris, grant_types, response_types, allowed_scopes, allowed_audiences, token_endpoint_auth_method, status
)
SELECT 'addp-ontology', 'ADDP Ontology projection runtime', 'confidential', NULL, id,
    ARRAY[]::text[], ARRAY['client_credentials']::text[], ARRAY[]::text[],
    ARRAY['addp.api']::text[], ARRAY['addp.api']::text[], 'client_secret_basic', 'disabled'
FROM service_principal;

INSERT INTO system.tenant_memberships (tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id)
SELECT tenant.id, service.id, 'active', 'bootstrap', tenant.initialized_at, tenant.initialized_by_principal_id
FROM system.tenants tenant JOIN system.service_principals service ON service.name = 'addp-ontology'
WHERE tenant.initialized_at IS NOT NULL ORDER BY tenant.id;

INSERT INTO system.role_assignments (principal_id, role_id, scope_type, tenant_id, status, valid_from, source_type, grant_reason)
SELECT service.id, role.id, 'tenant', tenant.id, 'active', tenant.initialized_at, 'bootstrap', 'built-in Ontology projection runtime'
FROM system.tenants tenant JOIN system.service_principals service ON service.name = 'addp-ontology'
JOIN system.roles role ON role.role_key = 'tenant.ontology_runtime' AND role.tenant_id IS NULL
WHERE tenant.initialized_at IS NOT NULL ORDER BY tenant.id;

COMMIT;
