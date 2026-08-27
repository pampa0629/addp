BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT seed.permission_key, 'quality', seed.action, seed.risk_level, false,
       seed.scope_types, seed.tenant_customizable, seed.name_key, seed.description_key, 'active'
FROM (VALUES
    ('quality.materialization_gate.create', 'create', 'medium', ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.quality.materialization_gate.create.name', 'permissions.quality.materialization_gate.create.description'),
    ('quality.materialization_gate.delete', 'delete', 'high', ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.quality.materialization_gate.delete.name', 'permissions.quality.materialization_gate.delete.description'),
    ('quality.materialization_gate.read', 'read', 'low', ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.quality.materialization_gate.read.name', 'permissions.quality.materialization_gate.read.description'),
    ('quality.materialization_gate.update', 'update', 'medium', ARRAY['tenant', 'department', 'project_group']::text[], true, 'permissions.quality.materialization_gate.update.name', 'permissions.quality.materialization_gate.update.description'),
    ('quality.task_provider.execute', 'execute', 'medium', ARRAY['tenant']::text[], false, 'permissions.quality.task_provider.execute.name', 'permissions.quality.task_provider.execute.description'),
    ('quality.task_provider.read', 'read', 'low', ARRAY['tenant']::text[], false, 'permissions.quality.task_provider.read.name', 'permissions.quality.task_provider.read.description')
) AS seed(permission_key, action, risk_level, scope_types, tenant_customizable, name_key, description_key)
ON CONFLICT (permission_key) DO UPDATE
SET owner_module = EXCLUDED.owner_module,
    action = EXCLUDED.action,
    risk_level = EXCLUDED.risk_level,
    delegable = EXCLUDED.delegable,
    allowed_scope_types = EXCLUDED.allowed_scope_types,
    tenant_customizable = EXCLUDED.tenant_customizable,
    name_i18n_key = EXCLUDED.name_i18n_key,
    description_i18n_key = EXCLUDED.description_i18n_key,
    status = 'active',
    updated_at = NOW();

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('tenant.governance_manager', 'quality.materialization_gate.create'),
    ('tenant.governance_manager', 'quality.materialization_gate.delete'),
    ('tenant.governance_manager', 'quality.materialization_gate.read'),
    ('tenant.governance_manager', 'quality.materialization_gate.update'),
    ('tenant.orchestrator_runtime', 'quality.task_provider.execute'),
    ('tenant.orchestrator_runtime', 'quality.task_provider.read'),
    ('tenant.quality_runtime', 'model.materialization_group.read')
) AS seed(role_key, permission_key)
JOIN system.roles AS role
  ON role.tenant_id IS NULL AND role.role_key = seed.role_key AND role.status = 'active'
JOIN system.permissions AS permission
  ON permission.permission_key = seed.permission_key AND permission.status = 'active'
ORDER BY seed.role_key, seed.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
