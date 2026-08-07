BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'meta.lineage.create', 'meta', 'create', 'medium', false,
    ARRAY['tenant']::text[], false,
    'permissions.meta.lineage.create.name',
    'permissions.meta.lineage.create.description', 'active'
)
ON CONFLICT (permission_key) DO NOTHING;

INSERT INTO system.role_permissions (role_id, permission_id, source_type)
SELECT role.id, permission.id, 'product'
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'meta.lineage.create'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.service_runtime'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
