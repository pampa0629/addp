BEGIN;

UPDATE system.permissions
SET allowed_scope_types = ARRAY['tenant', 'department', 'project_group']::text[]
WHERE permission_key = 'catalog.entry.read'
  AND NOT (ARRAY['tenant', 'department', 'project_group']::text[] <@ allowed_scope_types);

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, owner_module, action, risk_level, delegable,
       allowed_scope_types, tenant_customizable, name_i18n_key,
       description_i18n_key, status
FROM (VALUES
    ('catalog.collection.read', 'catalog', 'read', 'low', true,
     ARRAY['tenant', 'project_group']::text[], true,
     'permissions.catalog.collection.read.name', 'permissions.catalog.collection.read.description', 'active'),
    ('catalog.collection.update', 'catalog', 'update', 'medium', false,
     ARRAY['tenant', 'project_group']::text[], true,
     'permissions.catalog.collection.update.name', 'permissions.catalog.collection.update.description', 'active')
) AS seed(permission_key, owner_module, action, risk_level, delegable, allowed_scope_types,
          tenant_customizable, name_i18n_key, description_i18n_key, status)
ORDER BY permission_key;

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key IN ('catalog.collection.read', 'catalog.collection.update')
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.administrator'
  AND role.status = 'active'
ORDER BY permission.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
