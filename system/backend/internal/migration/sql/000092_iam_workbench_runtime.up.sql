BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'workbench', action, risk_level, delegable,
       ARRAY['tenant', 'department', 'project_group']::text[], true,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', 'active'
FROM (VALUES
    ('workbench.view.create', 'create', 'low', false),
    ('workbench.view.delete', 'delete', 'medium', false),
    ('workbench.view.read', 'read', 'low', true),
    ('workbench.view.update', 'update', 'low', false)
) AS seed(permission_key, action, risk_level, delegable)
ORDER BY permission_key;

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
)
VALUES (
    'platform.workbench_runtime',
    'roles.platform.workbench_runtime.name',
    'roles.platform.workbench_runtime.description',
    'platform_builtin', ARRAY['platform']::text[], ARRAY['service_principal']::text[], true, 'active'
);

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'system.runtime_registry.update'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'platform.workbench_runtime'
  AND role.status = 'active';

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles role
JOIN system.permissions permission
  ON permission.owner_module = 'workbench' AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.administrator', 'tenant.data_viewer')
  AND role.status = 'active'
ORDER BY role.role_key, permission.permission_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'service.data_read.execute'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.data_viewer'
  AND role.status = 'active';

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
        service_id, 'addp-workbench', 'ADDP Workbench runtime', 'platform', service_id
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
WHERE service_principal.name = 'addp-workbench';

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, status, valid_from, source_type, reason
)
SELECT service_principal.id, role.id, 'platform', 'active', transaction_timestamp(),
       'bootstrap', 'built-in Workbench control plane runtime'
FROM system.service_principals service_principal
JOIN system.roles role
  ON role.tenant_id IS NULL AND role.role_key = 'platform.workbench_runtime'
WHERE service_principal.name = 'addp-workbench';

COMMIT;
