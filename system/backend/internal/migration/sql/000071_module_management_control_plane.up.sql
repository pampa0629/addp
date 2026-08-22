BEGIN;

ALTER TABLE system.module_definitions
    ADD COLUMN version bigint NOT NULL DEFAULT 1;

ALTER TABLE system.module_definitions
    ADD CONSTRAINT module_definitions_version_positive CHECK (version > 0);

UPDATE system.permissions
SET status = 'active',
    updated_at = now()
WHERE permission_key IN ('platform.module.read', 'platform.module.update');

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key IN ('platform.module.read', 'platform.module.update')
WHERE role.tenant_id IS NULL
  AND role.role_key = 'platform.system_administrator'
  AND role.role_type = 'platform_builtin'
  AND role.status = 'active'
  AND permission.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

WITH affected_principals AS (
    SELECT DISTINCT assignment.principal_id
    FROM system.role_assignments AS assignment
    JOIN system.roles AS role ON role.id = assignment.role_id
    WHERE assignment.status = 'active'
      AND role.tenant_id IS NULL
      AND role.role_key = 'platform.system_administrator'
)
UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM affected_principals AS affected
WHERE principal.id = affected.principal_id;

DO $$
BEGIN
    IF (
        SELECT count(*)
        FROM system.role_permissions AS role_permission
        JOIN system.roles AS role ON role.id = role_permission.role_id
        JOIN system.permissions AS permission ON permission.id = role_permission.permission_id
        WHERE role.tenant_id IS NULL
          AND role.role_key = 'platform.system_administrator'
          AND permission.permission_key IN ('platform.module.read', 'platform.module.update')
          AND permission.status = 'active'
    ) <> 2 THEN
        RAISE EXCEPTION 'platform system administrator module management permissions are required';
    END IF;
END;
$$;

COMMIT;
