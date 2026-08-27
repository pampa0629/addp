BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'workbench', action, risk_level, delegable,
       ARRAY['tenant', 'department', 'project_group']::text[], true,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', 'active'
FROM (VALUES
    ('workbench.data_application.create', 'create', 'low', false),
    ('workbench.data_application.delete', 'delete', 'medium', false),
    ('workbench.data_application.execute', 'execute', 'low', true),
    ('workbench.data_application.publish', 'publish', 'medium', false),
    ('workbench.data_application.read', 'read', 'low', true),
    ('workbench.data_application.update', 'update', 'low', false)
) AS seed(permission_key, action, risk_level, delegable)
ORDER BY permission_key
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
  ON permission.owner_module = 'workbench'
 AND permission.permission_key LIKE 'workbench.data_application.%'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.administrator', 'tenant.data_viewer')
  AND role.status = 'active'
ORDER BY role.role_key, permission.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

WITH affected_principals AS (
    SELECT DISTINCT assignment.principal_id
    FROM system.role_assignments AS assignment
    JOIN system.roles AS role ON role.id = assignment.role_id
    WHERE assignment.status = 'active'
      AND role.tenant_id IS NULL
      AND role.role_key IN ('tenant.administrator', 'tenant.data_viewer')
)
UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM affected_principals AS affected
WHERE principal.id = affected.principal_id;

COMMIT;
