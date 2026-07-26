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
        'standard.document.create',
        'standard',
        'create',
        'medium',
        false,
        ARRAY['tenant', 'department', 'project_group']::text[],
        true,
        'permissions.standard.document.create.name',
        'permissions.standard.document.create.description',
        'active'
    ),
    (
        'standard.document.delete',
        'standard',
        'delete',
        'high',
        false,
        ARRAY['tenant', 'department', 'project_group']::text[],
        true,
        'permissions.standard.document.delete.name',
        'permissions.standard.document.delete.description',
        'active'
    ),
    (
        'standard.document.read',
        'standard',
        'read',
        'low',
        false,
        ARRAY['tenant', 'department', 'project_group']::text[],
        true,
        'permissions.standard.document.read.name',
        'permissions.standard.document.read.description',
        'active'
    ),
    (
        'standard.document.update',
        'standard',
        'update',
        'medium',
        false,
        ARRAY['tenant', 'department', 'project_group']::text[],
        true,
        'permissions.standard.document.update.name',
        'permissions.standard.document.update.description',
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
CROSS JOIN system.permissions AS permission
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.governance_manager'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
  AND permission.permission_key IN (
      'standard.document.create',
      'standard.document.delete',
      'standard.document.read',
      'standard.document.update'
  );

COMMIT;
