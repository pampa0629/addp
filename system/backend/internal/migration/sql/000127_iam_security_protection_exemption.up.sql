BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES
    (
        'security.protection_exemption.create', 'security', 'create', 'critical', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.protection_exemption.create.name',
        'permissions.security.protection_exemption.create.description', 'active'
    ),
    (
        'security.protection_exemption.delete', 'security', 'delete', 'critical', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.protection_exemption.delete.name',
        'permissions.security.protection_exemption.delete.description', 'active'
    ),
    (
        'security.protection_exemption.read', 'security', 'read', 'high', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.protection_exemption.read.name',
        'permissions.security.protection_exemption.read.description', 'active'
    ),
    (
        'security.protection_exemption.update', 'security', 'update', 'critical', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.protection_exemption.update.name',
        'permissions.security.protection_exemption.update.description', 'active'
    );

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'security.protection_exemption.create',
      'security.protection_exemption.delete',
      'security.protection_exemption.read',
      'security.protection_exemption.update'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.administrator', 'tenant.governance_manager')
ORDER BY role.role_key, permission.permission_key;

COMMIT;
