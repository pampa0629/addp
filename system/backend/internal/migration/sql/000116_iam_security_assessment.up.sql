BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES
    (
        'security.assessment.update', 'security', 'update', 'high', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.assessment.update.name',
        'permissions.security.assessment.update.description', 'active'
    ),
    (
        'security.assessment.read', 'security', 'read', 'medium', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.assessment.read.name',
        'permissions.security.assessment.read.description', 'active'
    ),
    (
        'security.finding.update', 'security', 'update', 'high', false,
        ARRAY['tenant', 'department', 'project_group']::text[], true,
        'permissions.security.finding.update.name',
        'permissions.security.finding.update.description', 'active'
    );

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'security.assessment.read',
      'security.assessment.update',
      'security.finding.update'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.administrator', 'tenant.governance_manager')
ORDER BY role.role_key, permission.permission_key;

COMMIT;
