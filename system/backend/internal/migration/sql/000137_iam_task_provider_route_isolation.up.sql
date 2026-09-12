BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES
(
    'graph.task_provider.execute', 'graph', 'execute', 'medium', false,
    ARRAY['tenant']::text[], false,
    'permissions.graph.task_provider.execute.name',
    'permissions.graph.task_provider.execute.description', 'active'
),
(
    'graph.task_provider.read', 'graph', 'read', 'low', false,
    ARRAY['tenant']::text[], false,
    'permissions.graph.task_provider.read.name',
    'permissions.graph.task_provider.read.description', 'active'
),
(
    'manager.task_provider.execute', 'manager', 'execute', 'medium', false,
    ARRAY['tenant']::text[], false,
    'permissions.manager.task_provider.execute.name',
    'permissions.manager.task_provider.execute.description', 'active'
),
(
    'manager.task_provider.read', 'manager', 'read', 'low', false,
    ARRAY['tenant']::text[], false,
    'permissions.manager.task_provider.read.name',
    'permissions.manager.task_provider.read.description', 'active'
),
(
    'meta.task_provider.execute', 'meta', 'execute', 'medium', false,
    ARRAY['tenant']::text[], false,
    'permissions.meta.task_provider.execute.name',
    'permissions.meta.task_provider.execute.description', 'active'
),
(
    'meta.task_provider.read', 'meta', 'read', 'low', false,
    ARRAY['tenant']::text[], false,
    'permissions.meta.task_provider.read.name',
    'permissions.meta.task_provider.read.description', 'active'
),
(
    'orchestrator.task_provider.execute', 'orchestrator', 'execute', 'medium', false,
    ARRAY['tenant']::text[], false,
    'permissions.orchestrator.task_provider.execute.name',
    'permissions.orchestrator.task_provider.execute.description', 'active'
),
(
    'orchestrator.task_provider.read', 'orchestrator', 'read', 'low', false,
    ARRAY['tenant']::text[], false,
    'permissions.orchestrator.task_provider.read.name',
    'permissions.orchestrator.task_provider.read.description', 'active'
)
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
    updated_at = now();

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'graph.task_provider.execute',
      'graph.task_provider.read',
      'manager.task_provider.execute',
      'manager.task_provider.read',
      'meta.task_provider.execute',
      'meta.task_provider.read',
      'orchestrator.task_provider.execute',
      'orchestrator.task_provider.read'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.orchestrator_runtime'
  AND role.status = 'active'
ORDER BY permission.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

WITH affected_principals AS (
    SELECT DISTINCT assignment.principal_id
    FROM system.role_assignments AS assignment
    JOIN system.roles AS role ON role.id = assignment.role_id
    WHERE assignment.status = 'active'
      AND role.tenant_id IS NULL
      AND role.role_key = 'tenant.orchestrator_runtime'
    UNION
    SELECT service_principal.id
    FROM system.service_principals AS service_principal
    WHERE service_principal.name = 'addp-orchestrator'
)
UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM affected_principals AS affected
WHERE principal.id = affected.principal_id;

COMMIT;
