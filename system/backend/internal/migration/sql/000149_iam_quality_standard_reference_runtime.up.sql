BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'quality.standard_reference.update', 'quality', 'update', 'high', false,
    ARRAY['tenant']::text[], false,
    'permissions.quality.standard_reference.update.name',
    'permissions.quality.standard_reference.update.description', 'active'
)
ON CONFLICT (permission_key) DO NOTHING;

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'quality.standard_reference.update'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.standard_runtime'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
ON CONFLICT (role_id, permission_id) DO NOTHING;

CREATE TEMP TABLE quality_standard_reference_affected_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id
FROM system.role_assignments AS assignment
JOIN system.roles AS role ON role.id = assignment.role_id
JOIN system.role_permissions AS role_permission ON role_permission.role_id = role.id
JOIN system.permissions AS permission ON permission.id = role_permission.permission_id
WHERE assignment.status = 'active'
  AND role.role_key = 'tenant.standard_runtime'
  AND permission.permission_key = 'quality.standard_reference.update';

UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM quality_standard_reference_affected_principals AS affected
WHERE principal.id = affected.principal_id;

UPDATE system.refresh_token_families AS family
SET revoked_at = now(),
    revoked_reason = 'authorization_catalog_changed',
    updated_at = now()
FROM quality_standard_reference_affected_principals AS affected
WHERE family.principal_id = affected.principal_id
  AND family.revoked_at IS NULL;

COMMIT;
