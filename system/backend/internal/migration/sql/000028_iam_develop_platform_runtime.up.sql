BEGIN;

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
) VALUES (
    'platform.develop_runtime', 'roles.platform.develop_runtime.name',
    'roles.platform.develop_runtime.description', 'platform_builtin',
    ARRAY['platform']::text[], ARRAY['service_principal']::text[], true, 'active'
);

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'system.runtime_registry.update'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'platform.develop_runtime';

INSERT INTO system.role_assignments (
    principal_id, role_id, scope_type, status, valid_from,
    source_type, reason
)
SELECT service_principal.id, role.id, 'platform', 'active', transaction_timestamp(),
       'bootstrap', 'built-in service control plane runtime'
FROM system.service_principals service_principal
JOIN system.roles role
  ON role.tenant_id IS NULL
 AND role.role_key = 'platform.develop_runtime'
WHERE service_principal.name = 'addp-develop';

COMMIT;
