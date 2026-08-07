BEGIN;

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
)
SELECT seed.role_key, 'roles.' || seed.role_key || '.name',
       'roles.' || seed.role_key || '.description', seed.role_type,
       seed.allowed_scope_types, ARRAY['service_principal']::text[], true, 'active'
FROM (VALUES
    ('platform.geopython_runtime', 'platform_builtin', ARRAY['platform']::text[]),
    ('platform.model3d_runtime', 'platform_builtin', ARRAY['platform']::text[]),
    ('platform.pointcloud_runtime', 'platform_builtin', ARRAY['platform']::text[]),
    ('platform.spark_runtime', 'platform_builtin', ARRAY['platform']::text[]),
    ('tenant.geopython_runtime', 'tenant_builtin', ARRAY['tenant']::text[]),
    ('tenant.model3d_runtime', 'tenant_builtin', ARRAY['tenant']::text[]),
    ('tenant.pointcloud_runtime', 'tenant_builtin', ARRAY['tenant']::text[]),
    ('tenant.spark_runtime', 'tenant_builtin', ARRAY['tenant']::text[])
) AS seed(role_key, role_type, allowed_scope_types)
ORDER BY seed.role_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('platform.geopython_runtime', 'system.runtime_registry.update'),
    ('platform.model3d_runtime', 'system.runtime_registry.update'),
    ('platform.pointcloud_runtime', 'system.runtime_registry.update'),
    ('platform.spark_runtime', 'system.runtime_registry.update'),
    ('tenant.geopython_runtime', 'manager.derived_artifact.create'),
    ('tenant.model3d_runtime', 'manager.derived_artifact.create'),
    ('tenant.pointcloud_runtime', 'manager.derived_artifact.create'),
    ('tenant.spark_runtime', 'system.engine.read')
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
            ('addp-geopython', 'ADDP GeoPython Workflow runtime'),
            ('addp-model3d', 'ADDP Model3D Workflow runtime'),
            ('addp-pointcloud', 'ADDP PointCloud Workflow runtime'),
            ('addp-spark', 'ADDP Spark Workflow runtime')
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
       transaction_timestamp(), 'bootstrap', 'built-in workflow runtime registration'
FROM (VALUES
    ('addp-geopython', 'platform.geopython_runtime'),
    ('addp-model3d', 'platform.model3d_runtime'),
    ('addp-pointcloud', 'platform.pointcloud_runtime'),
    ('addp-spark', 'platform.spark_runtime')
) AS seed(service_name, role_key)
JOIN system.service_principals service_principal ON service_principal.name = seed.service_name
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = seed.role_key
ORDER BY seed.service_name;

INSERT INTO system.tenant_memberships (
    tenant_id, principal_id, status, source_type, joined_at
)
SELECT tenant.id, service_principal.id, 'active', 'bootstrap', transaction_timestamp()
FROM system.tenants tenant
CROSS JOIN system.service_principals service_principal
WHERE tenant.status = 'active'
  AND service_principal.name IN ('addp-geopython', 'addp-model3d', 'addp-pointcloud', 'addp-spark')
ORDER BY tenant.id, service_principal.name;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, tenant_id, status, valid_from, source_type, reason
)
SELECT service_principal.id, role.id, 'tenant', tenant.id, 'active',
       transaction_timestamp(), 'bootstrap', 'built-in workflow runtime'
FROM system.tenants tenant
CROSS JOIN (VALUES
    ('addp-geopython', 'tenant.geopython_runtime'),
    ('addp-model3d', 'tenant.model3d_runtime'),
    ('addp-pointcloud', 'tenant.pointcloud_runtime'),
    ('addp-spark', 'tenant.spark_runtime')
) AS seed(service_name, role_key)
JOIN system.service_principals service_principal ON service_principal.name = seed.service_name
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = seed.role_key
WHERE tenant.status = 'active'
ORDER BY tenant.id, seed.service_name;

COMMIT;
