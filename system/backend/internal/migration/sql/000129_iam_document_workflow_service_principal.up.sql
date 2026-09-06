BEGIN;

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
)
VALUES
    ('platform.document_runtime', 'roles.platform.document_runtime.name', 'roles.platform.document_runtime.description',
     'platform_builtin', ARRAY['platform']::text[], ARRAY['service_principal']::text[], true, 'active'),
    ('tenant.document_runtime', 'roles.tenant.document_runtime.name', 'roles.tenant.document_runtime.description',
     'tenant_builtin', ARRAY['tenant']::text[], ARRAY['service_principal']::text[], true, 'active');

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM (VALUES
    ('platform.document_runtime', 'system.runtime_registry.update'),
    ('tenant.document_runtime', 'manager.derived_artifact.create')
) AS seed(role_key, permission_key)
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = seed.role_key AND role.status = 'active'
JOIN system.permissions permission
  ON permission.permission_key = seed.permission_key AND permission.status = 'active';

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
        service_id, 'addp-document', 'ADDP Document Workflow runtime', 'platform', service_id
    );

    INSERT INTO system.oauth_clients (
        client_id, display_name, client_type, client_secret_hash,
        service_principal_id, redirect_uris, grant_types, response_types,
        allowed_scopes, allowed_audiences, token_endpoint_auth_method, status
    ) VALUES (
        'addp-document', 'ADDP Document Workflow runtime', 'confidential', NULL,
        service_id, ARRAY[]::text[], ARRAY['client_credentials']::text[],
        ARRAY[]::text[], ARRAY['addp.api']::text[], ARRAY['addp.api']::text[],
        'client_secret_basic', 'disabled'
    );

    INSERT INTO system.role_assignments (
        principal_id, role_id, scope_type, status, valid_from, source_type, reason
    )
    SELECT service_id, role.id, 'platform', 'active', transaction_timestamp(),
           'bootstrap', 'built-in document workflow runtime registration'
    FROM system.roles role
    WHERE role.tenant_id IS NULL AND role.role_key = 'platform.document_runtime';

    INSERT INTO system.tenant_memberships (
        tenant_id, principal_id, status, source_type, joined_at
    )
    SELECT tenant.id, service_id, 'active', 'bootstrap', transaction_timestamp()
    FROM system.tenants tenant
    WHERE tenant.status = 'active';

    INSERT INTO system.role_assignments (
        principal_id, role_id, scope_type, tenant_id, status, valid_from, source_type, reason
    )
    SELECT service_id, role.id, 'tenant', tenant.id, 'active', transaction_timestamp(),
           'bootstrap', 'built-in document workflow runtime'
    FROM system.tenants tenant
    JOIN system.roles role
      ON role.tenant_id IS NULL AND role.role_key = 'tenant.document_runtime'
    WHERE tenant.status = 'active';
END;
$$;

COMMIT;
