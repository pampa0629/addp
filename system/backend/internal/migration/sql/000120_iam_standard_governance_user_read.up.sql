BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES
    ('standard.collection.create', 'standard', 'create', 'medium', false, ARRAY['tenant','department','project_group']::text[], true, 'permissions.standard.collection.create.name', 'permissions.standard.collection.create.description', 'active'),
    ('standard.collection.delete', 'standard', 'delete', 'high', false, ARRAY['tenant','department','project_group']::text[], true, 'permissions.standard.collection.delete.name', 'permissions.standard.collection.delete.description', 'active'),
    ('standard.collection.publish', 'standard', 'publish', 'high', false, ARRAY['tenant','department','project_group']::text[], true, 'permissions.standard.collection.publish.name', 'permissions.standard.collection.publish.description', 'active'),
    ('standard.collection.read', 'standard', 'read', 'low', false, ARRAY['tenant','department','project_group']::text[], true, 'permissions.standard.collection.read.name', 'permissions.standard.collection.read.description', 'active'),
    ('standard.collection.update', 'standard', 'update', 'medium', false, ARRAY['tenant','department','project_group']::text[], true, 'permissions.standard.collection.update.name', 'permissions.standard.collection.update.description', 'active'),
    ('standard.collection_assignment.update', 'standard', 'update', 'high', false, ARRAY['tenant','department','project_group']::text[], true, 'permissions.standard.collection_assignment.update.name', 'permissions.standard.collection_assignment.update.description', 'active')
ON CONFLICT (permission_key) DO NOTHING;

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'standard.collection.create', 'standard.collection.delete', 'standard.collection.publish',
      'standard.collection.read', 'standard.collection.update', 'standard.collection_assignment.update'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.governance_manager'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'iam.tenant_membership.read'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.standard_runtime'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
