BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'security.assessment.create', 'security', 'create', 'high', false,
    ARRAY['tenant', 'department', 'project_group']::text[], true,
    'permissions.security.assessment.create.name',
    'permissions.security.assessment.create.description', 'active'
);

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'security.assessment.create'
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.administrator', 'tenant.governance_manager')
ORDER BY role.role_key;

COMMIT;
