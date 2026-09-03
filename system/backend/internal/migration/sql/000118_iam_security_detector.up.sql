BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES
    (
        'security.detector.create', 'security', 'create', 'medium', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.detector.create.name',
        'permissions.security.detector.create.description', 'active'
    ),
    (
        'security.detector.delete', 'security', 'delete', 'high', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.detector.delete.name',
        'permissions.security.detector.delete.description', 'active'
    ),
    (
        'security.detector.read', 'security', 'read', 'low', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.detector.read.name',
        'permissions.security.detector.read.description', 'active'
    ),
    (
        'security.detector.update', 'security', 'update', 'medium', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.detector.update.name',
        'permissions.security.detector.update.description', 'active'
    );

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'security.detector.create',
      'security.detector.delete',
      'security.detector.read',
      'security.detector.update'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.administrator', 'tenant.governance_manager')
ORDER BY role.role_key, permission.permission_key;

COMMIT;
