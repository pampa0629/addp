BEGIN;

UPDATE system.permissions
SET allowed_scope_types = ARRAY['platform', 'tenant']::text[],
    updated_at = transaction_timestamp()
WHERE permission_key = 'system.api_key.read';

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
)
SELECT seed.role_key, 'roles.' || seed.role_key || '.name',
       'roles.' || seed.role_key || '.description', 'platform_builtin',
       ARRAY['platform']::text[], ARRAY['service_principal']::text[], true, 'active'
FROM (VALUES
    ('platform.gateway_runtime'),
    ('platform.model_runtime'),
    ('platform.quality_runtime'),
    ('platform.standard_runtime')
) AS seed(role_key)
ORDER BY seed.role_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('platform.gateway_runtime', 'system.api_key.read'),
    ('platform.gateway_runtime', 'system.runtime_registry.read'),
    ('platform.model_runtime', 'system.runtime_registry.update'),
    ('platform.monitor_runtime', 'system.runtime_registry.read'),
    ('platform.quality_runtime', 'system.runtime_registry.update'),
    ('platform.standard_runtime', 'system.runtime_registry.update'),
    ('tenant.graph_runtime', 'model.entity.read'),
    ('tenant.graph_runtime', 'model.entity_relation.read'),
    ('tenant.graph_runtime', 'system.engine.read'),
    ('tenant.manager_runtime', 'system.engine.read'),
    ('tenant.monitor_runtime', 'audit.tenant_event.create'),
    ('tenant.quality_runtime', 'standard.element.read'),
    ('tenant.quality_runtime', 'system.engine.read'),
    ('tenant.service_runtime', 'system.engine.read'),
    ('tenant.transfer_runtime', 'system.engine.read')
) AS seed(role_key, permission_key)
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = seed.role_key AND role.status = 'active'
JOIN system.permissions permission
  ON permission.permission_key = seed.permission_key AND permission.status = 'active'
ORDER BY seed.role_key, seed.permission_key;

DO $$
DECLARE
    service_name text;
    service_description text;
    service_id bigint;
BEGIN
    FOR service_name, service_description IN
        SELECT * FROM (VALUES
            ('addp-gateway', 'ADDP Gateway runtime'),
            ('addp-model', 'ADDP Model runtime'),
            ('addp-standard', 'ADDP Standard runtime')
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

        INSERT INTO system.oauth_clients (
            client_id, display_name, client_type, client_secret_hash,
            service_principal_id, redirect_uris, grant_types, response_types,
            allowed_scopes, allowed_audiences, token_endpoint_auth_method, status
        ) VALUES (
            service_name, service_description, 'confidential', NULL,
            service_id, ARRAY[]::text[], ARRAY['client_credentials']::text[],
            ARRAY[]::text[], ARRAY['addp.api']::text[], ARRAY['addp.api']::text[],
            'client_secret_basic', 'disabled'
        );
    END LOOP;
END;
$$;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, status, valid_from, source_type, reason
)
SELECT service_principal.id, role.id, 'platform', 'active',
       transaction_timestamp(), 'bootstrap',
       'built-in service control plane runtime'
FROM (VALUES
    ('addp-gateway', 'platform.gateway_runtime'),
    ('addp-model', 'platform.model_runtime'),
    ('addp-quality', 'platform.quality_runtime'),
    ('addp-standard', 'platform.standard_runtime')
) AS seed(service_name, role_key)
JOIN system.service_principals service_principal
  ON service_principal.name = seed.service_name
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = seed.role_key AND role.status = 'active'
ORDER BY seed.service_name;

COMMIT;
