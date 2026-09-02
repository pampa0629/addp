BEGIN;

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
    ('security.enrollment.create', 'create', 'high'),
    ('security.enrollment.read', 'read', 'low'),
    ('security.enrollment.update', 'update', 'high')
) AS seed(permission_key, action, risk_level)
ORDER BY permission_key;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'security', action, risk_level, false,
       ARRAY['tenant']::text[], false,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', 'active'
FROM (VALUES
    ('security.protection_projection.read', 'read', 'medium'),
    ('security.protection_projection.update', 'update', 'medium')
) AS seed(permission_key, action, risk_level)
ORDER BY permission_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key IN (
      'security.enrollment.create',
      'security.enrollment.read',
      'security.enrollment.update'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.administrator', 'tenant.governance_manager')
ORDER BY role.role_key, permission.permission_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key IN (
      'security.protection_projection.read',
      'security.protection_projection.update'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key IN (
      'tenant.develop_runtime',
      'tenant.manager_runtime',
      'tenant.service_runtime',
      'tenant.transfer_runtime'
  )
ORDER BY role.role_key, permission.permission_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'platform.tenant.read'
WHERE role.tenant_id IS NULL
  AND role.role_key IN (
      'platform.develop_runtime',
      'platform.manager_runtime',
      'platform.service_runtime',
      'platform.transfer_runtime'
  )
ORDER BY role.role_key;

COMMIT;
