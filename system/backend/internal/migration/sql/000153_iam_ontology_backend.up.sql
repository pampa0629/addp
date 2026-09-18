BEGIN;

-- Tenant authors opt in through custom roles; no existing User role expands.
INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status
)
SELECT 'ontology.revision.' || action, 'ontology', action, risk, false,
    ARRAY['tenant']::text[], true,
    'permissions.ontology.revision.' || action || '.name',
    'permissions.ontology.revision.' || action || '.description', 'active'
FROM (VALUES ('read', 'low'), ('update', 'medium')) AS seed(action, risk);

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
) VALUES (
    'platform.ontology_runtime', 'roles.platform.ontology_runtime.name',
    'roles.platform.ontology_runtime.description', 'platform_builtin',
    ARRAY['platform']::text[], ARRAY['service_principal']::text[], true, 'active'
);

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role JOIN system.permissions permission ON permission.permission_key = 'system.runtime_registry.update'
WHERE role.role_key = 'platform.ontology_runtime' AND role.tenant_id IS NULL;

INSERT INTO system.role_assignments (principal_id, role_id, scope_type, status, valid_from, source_type, grant_reason)
SELECT service.id, role.id, 'platform', 'active', transaction_timestamp(), 'bootstrap', 'built-in Ontology registry runtime'
FROM system.service_principals service
JOIN system.roles role ON role.role_key = 'platform.ontology_runtime' AND role.tenant_id IS NULL
WHERE service.name = 'addp-ontology' AND service.owner_scope = 'platform';

-- The existing assignment trigger advances authorization_version atomically.

COMMIT;
