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
)
SELECT seed.role_key,
       'roles.' || seed.role_key || '.name',
       'roles.' || seed.role_key || '.description',
       'platform_builtin',
       ARRAY['platform']::text[],
       ARRAY['service_principal']::text[],
       true,
       'active'
FROM (VALUES
    ('platform.monitor_runtime', 'addp-monitor'),
    ('platform.service_runtime', 'addp-service'),
    ('platform.transfer_runtime', 'addp-transfer')
) AS seed(role_key, service_name)
ORDER BY seed.role_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'system.runtime_registry.update'
WHERE role.role_key IN (
    'platform.monitor_runtime',
    'platform.service_runtime',
    'platform.transfer_runtime'
)
  AND role.tenant_id IS NULL
  AND role.role_type = 'platform_builtin'
  AND role.status = 'active'
  AND permission.status = 'active'
ORDER BY role.role_key;

INSERT INTO system.role_assignments (
    principal_id,
    role_id,
    scope_type,
    status,
    valid_from,
    source_type,
    reason
)
SELECT service_principal.id,
       role.id,
       'platform',
       'active',
       transaction_timestamp(),
       'bootstrap',
       'built-in service control plane runtime'
FROM (VALUES
    ('platform.monitor_runtime', 'addp-monitor'),
    ('platform.service_runtime', 'addp-service'),
    ('platform.transfer_runtime', 'addp-transfer')
) AS seed(role_key, service_name)
JOIN system.service_principals AS service_principal
  ON service_principal.name = seed.service_name
JOIN system.roles AS role
  ON role.tenant_id IS NULL
 AND role.role_key = seed.role_key
 AND role.role_type = 'platform_builtin'
 AND role.status = 'active'
ORDER BY seed.role_key;

COMMIT;
