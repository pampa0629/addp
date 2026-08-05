BEGIN;

INSERT INTO system.roles (
    role_key,
    name_i18n_key,
    description_i18n_key,
    role_type,
    allowed_scope_types,
    allowed_principal_types,
    immutable,
    status
) VALUES (
    'platform.manager_runtime',
    'roles.platform.manager_runtime.name',
    'roles.platform.manager_runtime.description',
    'platform_builtin',
    ARRAY['platform']::text[],
    ARRAY['service_principal']::text[],
    true,
    'active'
);

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'system.runtime_registry.update'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'platform.manager_runtime'
  AND role.role_type = 'platform_builtin'
  AND role.status = 'active'
  AND permission.status = 'active';

INSERT INTO system.role_assignments (
    principal_id,
    role_id,
    scope_type,
    status,
    valid_from,
    source_type,
    reason
)
SELECT service_principal.id, role.id, 'platform', 'active', transaction_timestamp(),
       'bootstrap', 'built-in service control plane runtime'
FROM system.service_principals AS service_principal
JOIN system.roles AS role
  ON role.tenant_id IS NULL
 AND role.role_key = 'platform.manager_runtime'
WHERE service_principal.name = 'addp-manager';

COMMIT;
