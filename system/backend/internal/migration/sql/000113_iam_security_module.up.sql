BEGIN;

DELETE FROM system.role_permissions
WHERE permission_id IN (
    SELECT id FROM system.permissions
    WHERE permission_key LIKE 'standard.classification.%'
);

UPDATE system.permissions
SET status = 'disabled',
    updated_at = transaction_timestamp()
WHERE permission_key LIKE 'standard.classification.%'
  AND status <> 'disabled';

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'security', action, risk_level, false,
       ARRAY['tenant', 'department', 'project_group']::text[], true,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', 'active'
FROM (VALUES
    ('security.classification.create', 'create', 'medium'),
    ('security.classification.delete', 'delete', 'high'),
    ('security.classification.read', 'read', 'low'),
    ('security.classification.update', 'update', 'medium'),
    ('security.grade.create', 'create', 'medium'),
    ('security.grade.delete', 'delete', 'high'),
    ('security.grade.read', 'read', 'low'),
    ('security.grade.update', 'update', 'medium'),
    ('security.protection_baseline.create', 'create', 'medium'),
    ('security.protection_baseline.delete', 'delete', 'high'),
    ('security.protection_baseline.read', 'read', 'low'),
    ('security.protection_baseline.update', 'update', 'medium'),
    ('security.sensitive_data_type.create', 'create', 'medium'),
    ('security.sensitive_data_type.delete', 'delete', 'high'),
    ('security.sensitive_data_type.read', 'read', 'low'),
    ('security.sensitive_data_type.update', 'update', 'medium')
) AS seed(permission_key, action, risk_level)
ORDER BY permission_key;

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
) VALUES (
    'platform.security_runtime', 'roles.platform.security_runtime.name',
    'roles.platform.security_runtime.description', 'platform_builtin',
    ARRAY['platform']::text[], ARRAY['service_principal']::text[], true, 'active'
);

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission ON permission.permission_key = 'system.runtime_registry.update'
WHERE role.tenant_id IS NULL AND role.role_key = 'platform.security_runtime';

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission ON permission.owner_module = 'security' AND permission.status = 'active'
WHERE role.tenant_id IS NULL AND role.role_key IN ('tenant.administrator', 'tenant.governance_manager')
ORDER BY permission.permission_key;

WITH principal AS (
    INSERT INTO system.principals (principal_type, status)
    VALUES ('service_principal', 'active')
    RETURNING id
), service_principal AS (
    INSERT INTO system.service_principals (id, name, description, owner_scope, created_by_principal_id)
    SELECT id, 'addp-security', 'ADDP Security runtime', 'platform', id FROM principal
    RETURNING id
)
INSERT INTO system.oauth_clients (
    client_id, display_name, client_type, client_secret_hash,
    service_principal_id, redirect_uris, grant_types, response_types,
    allowed_scopes, allowed_audiences, token_endpoint_auth_method, status
)
SELECT 'addp-security', 'ADDP Security runtime', 'confidential', NULL,
       id, ARRAY[]::text[], ARRAY['client_credentials']::text[], ARRAY[]::text[],
       ARRAY['addp.api']::text[], ARRAY['addp.api']::text[], 'client_secret_basic', 'disabled'
FROM service_principal;

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, status, valid_from, source_type, reason
)
SELECT service_principal.id, role.id, 'platform', 'active',
       transaction_timestamp(), 'bootstrap', 'built-in Security control plane runtime'
FROM system.service_principals service_principal
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = 'platform.security_runtime'
WHERE service_principal.name = 'addp-security';

COMMIT;
