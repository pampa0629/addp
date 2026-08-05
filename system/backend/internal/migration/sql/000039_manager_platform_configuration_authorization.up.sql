BEGIN;

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
) VALUES
    (
        'manager.configuration.read', 'manager', 'read', 'low', false,
        ARRAY['platform']::text[], false,
        'permissions.manager.configuration.read.name',
        'permissions.manager.configuration.read.description',
        'active'
    ),
    (
        'manager.configuration.update', 'manager', 'update', 'high', false,
        ARRAY['platform']::text[], false,
        'permissions.manager.configuration.update.name',
        'permissions.manager.configuration.update.description',
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
  ON permission.permission_key IN (
      'manager.configuration.read',
      'manager.configuration.update'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key = 'platform.system_administrator'
  AND role.role_type = 'platform_builtin'
  AND role.status = 'active'
  AND permission.status = 'active';

COMMIT;
