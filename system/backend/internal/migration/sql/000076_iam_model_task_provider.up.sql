BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES
(
    'model.task_provider.execute', 'model', 'execute', 'medium', false,
    ARRAY['tenant']::text[], false,
    'permissions.model.task_provider.execute.name',
    'permissions.model.task_provider.execute.description', 'active'
),
(
    'model.task_provider.read', 'model', 'read', 'low', false,
    ARRAY['tenant']::text[], false,
    'permissions.model.task_provider.read.name',
    'permissions.model.task_provider.read.description', 'active'
)
ON CONFLICT (permission_key) DO NOTHING;

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('tenant.orchestrator_runtime', 'model.task_provider.execute'),
    ('tenant.orchestrator_runtime', 'model.task_provider.read')
) AS seed(role_key, permission_key)
JOIN system.roles AS role
  ON role.tenant_id IS NULL
 AND role.role_key = seed.role_key
 AND role.status = 'active'
JOIN system.permissions AS permission
  ON permission.permission_key = seed.permission_key
 AND permission.status = 'active'
ORDER BY seed.role_key, seed.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

COMMIT;
