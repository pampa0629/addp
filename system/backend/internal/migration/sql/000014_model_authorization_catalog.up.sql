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
    ('model.dw_layer.create', 'model', 'create', 'medium', false, ARRAY['tenant']::text[], true, 'permissions.model.dw_layer.create.name', 'permissions.model.dw_layer.create.description', 'active'),
    ('model.dw_layer.delete', 'model', 'delete', 'high', false, ARRAY['tenant']::text[], true, 'permissions.model.dw_layer.delete.name', 'permissions.model.dw_layer.delete.description', 'active'),
    ('model.dw_layer.read', 'model', 'read', 'low', false, ARRAY['tenant']::text[], true, 'permissions.model.dw_layer.read.name', 'permissions.model.dw_layer.read.description', 'active'),
    ('model.dw_layer.update', 'model', 'update', 'medium', false, ARRAY['tenant']::text[], true, 'permissions.model.dw_layer.update.name', 'permissions.model.dw_layer.update.description', 'active'),
    ('model.entity.approve', 'model', 'approve', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.model.entity.approve.name', 'permissions.model.entity.approve.description', 'active'),
    ('model.entity.create', 'model', 'create', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.model.entity.create.name', 'permissions.model.entity.create.description', 'active'),
    ('model.entity.delete', 'model', 'delete', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.model.entity.delete.name', 'permissions.model.entity.delete.description', 'active'),
    ('model.entity.read', 'model', 'read', 'low', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.model.entity.read.name', 'permissions.model.entity.read.description', 'active'),
    ('model.entity.update', 'model', 'update', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.model.entity.update.name', 'permissions.model.entity.update.description', 'active'),
    ('model.entity_relation.create', 'model', 'create', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.model.entity_relation.create.name', 'permissions.model.entity_relation.create.description', 'active'),
    ('model.entity_relation.delete', 'model', 'delete', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.model.entity_relation.delete.name', 'permissions.model.entity_relation.delete.description', 'active'),
    ('model.entity_relation.read', 'model', 'read', 'low', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.model.entity_relation.read.name', 'permissions.model.entity_relation.read.description', 'active'),
    ('model.entity_relation.update', 'model', 'update', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.model.entity_relation.update.name', 'permissions.model.entity_relation.update.description', 'active');

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
      'model.entity.approve',
      'model.entity.create',
      'model.entity.delete',
      'model.entity.read',
      'model.entity.update',
      'model.entity_relation.create',
      'model.entity_relation.delete',
      'model.entity_relation.read',
      'model.entity_relation.update'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.governance_manager'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active';

COMMIT;
