BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES
    (
        'security.policy.create', 'security', 'create', 'high', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.policy.create.name',
        'permissions.security.policy.create.description', 'active'
    ),
    (
        'security.policy.delete', 'security', 'delete', 'high', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.policy.delete.name',
        'permissions.security.policy.delete.description', 'active'
    ),
    (
        'security.policy.read', 'security', 'read', 'medium', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.policy.read.name',
        'permissions.security.policy.read.description', 'active'
    ),
    (
        'security.policy.update', 'security', 'update', 'high', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.policy.update.name',
        'permissions.security.policy.update.description', 'active'
    );

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'security.policy.create',
      'security.policy.delete',
      'security.policy.read',
      'security.policy.update'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.administrator', 'tenant.governance_manager')
ORDER BY role.role_key, permission.permission_key;

COMMIT;
