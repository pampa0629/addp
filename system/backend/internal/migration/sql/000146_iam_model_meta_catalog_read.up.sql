-- Model reads physical source facts through Meta; no new permission is created.
BEGIN;

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'meta.catalog.read'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key IN ('tenant.model_runtime')
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

UPDATE system.principals
SET authorization_version = authorization_version + 1,
    updated_at = now()
WHERE id IN (
    SELECT assignment.principal_id
    FROM system.role_assignments AS assignment
    JOIN system.roles AS role ON role.id = assignment.role_id
    WHERE role.role_key IN ('tenant.model_runtime')
      AND assignment.status = 'active'
    UNION
    SELECT id
    FROM system.service_principals
    WHERE name IN ('addp-model')
);

COMMIT;
