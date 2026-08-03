BEGIN;

ALTER TABLE system.execution_authorizations
    DROP CONSTRAINT execution_authorizations_audience_check,
    ADD CONSTRAINT execution_authorizations_audience_check
        CHECK (audience IN ('develop', 'duckdb', 'service'));

INSERT INTO system.permissions (
    permission_key,
    owner_module,
    action,
    risk_level,
    delegable,
    allowed_scope_types,
    tenant_customizable,
    name_i18n_key,
    description_i18n_key,
    status
) VALUES (
    'service.data_read.execute',
    'service',
    'execute',
    'low',
    false,
    ARRAY['tenant', 'department', 'project_group']::text[],
    true,
    'permissions.service.data_read.execute.name',
    'permissions.service.data_read.execute.description',
    'active'
), (
    'system.execution_authorization.create',
    'system',
    'create',
    'low',
    false,
    ARRAY['tenant', 'department', 'project_group']::text[],
    false,
    'permissions.system.execution_authorization.create.name',
    'permissions.system.execution_authorization.create.description',
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
  ON permission.permission_key = 'service.data_read.execute'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.service_publisher'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active';

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('tenant.data_engineer'),
    ('tenant.service_publisher')
) AS seed(role_key)
JOIN system.roles AS role
  ON role.tenant_id IS NULL
 AND role.role_key = seed.role_key
 AND role.role_type = 'tenant_builtin'
 AND role.status = 'active'
JOIN system.permissions AS permission
  ON permission.permission_key = 'system.execution_authorization.create'
ORDER BY seed.role_key;

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'system.engine_descriptor.read',
      'system.execution_authorization.execute'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.service_runtime'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ORDER BY permission.permission_key;

COMMIT;
