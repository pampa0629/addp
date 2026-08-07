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
    'meta.lineage.read',
    'meta',
    'read',
    'low',
    true,
    ARRAY['tenant', 'department', 'project_group']::text[],
    true,
    'permissions.meta.lineage.read.name',
    'permissions.meta.lineage.read.description',
    'active'
)
ON CONFLICT (permission_key) DO NOTHING;

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'meta.lineage.read'
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.data_steward', 'tenant.data_viewer', 'tenant.governance_manager')
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
