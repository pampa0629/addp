BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
)
SELECT permission_key, 'workbench', action, 'high', false,
       ARRAY['tenant']::text[], false,
       'permissions.' || permission_key || '.name',
       'permissions.' || permission_key || '.description', 'active'
FROM (VALUES
    ('workbench.resource_grant.create', 'create'),
    ('workbench.resource_grant.revoke', 'revoke')
) AS seed(permission_key, action)
ORDER BY permission_key;

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN (
      'workbench.resource_grant.create',
      'workbench.resource_grant.revoke'
  )
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.asset_runtime'
  AND role.status = 'active'
ORDER BY permission.permission_key
ON CONFLICT (role_id, permission_id) DO NOTHING;

WITH affected_principals AS (
    SELECT DISTINCT assignment.principal_id
    FROM system.role_assignments AS assignment
    JOIN system.roles AS role ON role.id = assignment.role_id
    WHERE assignment.status = 'active'
      AND role.tenant_id IS NULL
      AND role.role_key = 'tenant.asset_runtime'
)
UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM affected_principals AS affected
WHERE principal.id = affected.principal_id;

COMMIT;
