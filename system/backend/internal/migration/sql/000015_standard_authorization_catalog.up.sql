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
    ('standard.classification.create', 'standard', 'create', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.classification.create.name', 'permissions.standard.classification.create.description', 'active'),
    ('standard.classification.delete', 'standard', 'delete', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.classification.delete.name', 'permissions.standard.classification.delete.description', 'active'),
    ('standard.classification.read', 'standard', 'read', 'low', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.classification.read.name', 'permissions.standard.classification.read.description', 'active'),
    ('standard.classification.update', 'standard', 'update', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.classification.update.name', 'permissions.standard.classification.update.description', 'active'),
    ('standard.dimension_hierarchy.create', 'standard', 'create', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.dimension_hierarchy.create.name', 'permissions.standard.dimension_hierarchy.create.description', 'active'),
    ('standard.dimension_hierarchy.delete', 'standard', 'delete', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.dimension_hierarchy.delete.name', 'permissions.standard.dimension_hierarchy.delete.description', 'active'),
    ('standard.dimension_hierarchy.read', 'standard', 'read', 'low', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.dimension_hierarchy.read.name', 'permissions.standard.dimension_hierarchy.read.description', 'active'),
    ('standard.dimension_hierarchy.update', 'standard', 'update', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.dimension_hierarchy.update.name', 'permissions.standard.dimension_hierarchy.update.description', 'active'),
    ('standard.element.approve', 'standard', 'approve', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.element.approve.name', 'permissions.standard.element.approve.description', 'active'),
    ('standard.glossary.approve', 'standard', 'approve', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.glossary.approve.name', 'permissions.standard.glossary.approve.description', 'active'),
    ('standard.glossary.create', 'standard', 'create', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.glossary.create.name', 'permissions.standard.glossary.create.description', 'active'),
    ('standard.glossary.delete', 'standard', 'delete', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.glossary.delete.name', 'permissions.standard.glossary.delete.description', 'active'),
    ('standard.glossary.offline', 'standard', 'offline', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.glossary.offline.name', 'permissions.standard.glossary.offline.description', 'active'),
    ('standard.glossary.read', 'standard', 'read', 'low', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.glossary.read.name', 'permissions.standard.glossary.read.description', 'active'),
    ('standard.glossary.update', 'standard', 'update', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.glossary.update.name', 'permissions.standard.glossary.update.description', 'active'),
    ('standard.metric.approve', 'standard', 'approve', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.metric.approve.name', 'permissions.standard.metric.approve.description', 'active'),
    ('standard.metric.offline', 'standard', 'offline', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.metric.offline.name', 'permissions.standard.metric.offline.description', 'active'),
    ('standard.unit.create', 'standard', 'create', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.unit.create.name', 'permissions.standard.unit.create.description', 'active'),
    ('standard.unit.delete', 'standard', 'delete', 'high', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.unit.delete.name', 'permissions.standard.unit.delete.description', 'active'),
    ('standard.unit.read', 'standard', 'read', 'low', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.unit.read.name', 'permissions.standard.unit.read.description', 'active'),
    ('standard.unit.update', 'standard', 'update', 'medium', false, ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.standard.unit.update.name', 'permissions.standard.unit.update.description', 'active');

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
      'standard.classification.create',
      'standard.classification.delete',
      'standard.classification.read',
      'standard.classification.update',
      'standard.dimension_hierarchy.create',
      'standard.dimension_hierarchy.delete',
      'standard.dimension_hierarchy.read',
      'standard.dimension_hierarchy.update',
      'standard.element.approve',
      'standard.glossary.approve',
      'standard.glossary.create',
      'standard.glossary.delete',
      'standard.glossary.offline',
      'standard.glossary.read',
      'standard.glossary.update',
      'standard.metric.approve',
      'standard.metric.offline',
      'standard.unit.create',
      'standard.unit.delete',
      'standard.unit.read',
      'standard.unit.update'
  )
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.governance_manager'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active';

COMMIT;
