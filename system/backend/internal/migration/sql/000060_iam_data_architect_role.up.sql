BEGIN;

INSERT INTO system.roles (
    tenant_id,
    role_key,
    name,
    description,
    name_i18n_key,
    description_i18n_key,
    role_type,
    allowed_scope_types,
    allowed_principal_types,
    immutable,
    status,
    created_by_principal_id
) VALUES (
    NULL,
    'tenant.data_architect',
    NULL,
    NULL,
    'roles.tenant.data_architect.name',
    'roles.tenant.data_architect.description',
    'tenant_builtin',
    ARRAY['tenant']::text[],
    ARRAY['user']::text[],
    true,
    'active',
    NULL
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
  ON permission.permission_key IN (
      'model.dw_layer.create',
      'model.dw_layer.delete',
      'model.dw_layer.read',
      'model.dw_layer.update'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.data_architect'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ORDER BY permission.permission_key;

COMMIT;
