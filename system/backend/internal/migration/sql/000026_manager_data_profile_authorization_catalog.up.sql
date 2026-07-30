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
) VALUES (
    'manager.data_profile.execute',
    'manager',
    'execute',
    'medium',
    false,
    ARRAY['tenant', 'department', 'project_group']::text[],
    true,
    'permissions.manager.data_profile.execute.name',
    'permissions.manager.data_profile.execute.description',
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
  ON permission.permission_key = 'manager.data_profile.execute'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.data_engineer'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active';

COMMIT;
