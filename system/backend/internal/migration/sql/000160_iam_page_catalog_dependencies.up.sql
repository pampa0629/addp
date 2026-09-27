BEGIN;

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('tenant.administrator', 'meta.catalog.read'),
    ('tenant.security_manager', 'meta.catalog.read'),
    ('tenant.data_architect', 'standard.unit.read')
) AS required(role_key, permission_key)
JOIN system.roles AS role
  ON role.role_key = required.role_key
 AND role.tenant_id IS NULL
 AND role.role_type = 'tenant_builtin'
 AND role.status = 'active'
JOIN system.permissions AS permission
  ON permission.permission_key = required.permission_key
 AND permission.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

CREATE TEMP TABLE page_catalog_affected_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id
FROM system.role_assignments AS assignment
JOIN system.roles AS role ON role.id = assignment.role_id
WHERE assignment.status = 'active'
  AND role.tenant_id IS NULL
  AND role.role_key IN ('tenant.administrator', 'tenant.security_manager', 'tenant.data_architect');

UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM page_catalog_affected_principals AS affected
WHERE principal.id = affected.principal_id;

UPDATE system.refresh_token_families AS family
SET revoked_at = now(),
    revoked_reason = 'authorization_catalog_changed',
    updated_at = now()
FROM page_catalog_affected_principals AS affected
WHERE family.principal_id = affected.principal_id
  AND family.revoked_at IS NULL;

COMMIT;
