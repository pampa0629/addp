BEGIN;

-- The Transfer create wizard reads tenant engine and resource options from Meta.
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
  AND role.role_key = 'tenant.data_engineer'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

CREATE TEMP TABLE data_engineer_catalog_affected_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id
FROM system.role_assignments AS assignment
JOIN system.roles AS role ON role.id = assignment.role_id
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.data_engineer'
  AND role.role_type = 'tenant_builtin'
  AND assignment.status = 'active';

UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM data_engineer_catalog_affected_principals AS affected
WHERE principal.id = affected.principal_id;

UPDATE system.refresh_token_families AS family
SET revoked_at = now(),
    revoked_reason = 'authorization_catalog_changed',
    updated_at = now()
FROM data_engineer_catalog_affected_principals AS affected
WHERE family.principal_id = affected.principal_id
  AND family.revoked_at IS NULL;

COMMIT;
