BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'inference.model_label.read', 'inference', 'read', 'low', false,
    ARRAY['tenant', 'department', 'project_group']::text[], true,
    'permissions.inference.model_label.read.name',
    'permissions.inference.model_label.read.description', 'active'
);

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'inference.model_label.read'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
  AND role.role_key IN ('tenant.administrator', 'tenant.data_steward')
ON CONFLICT (role_id, permission_id) DO NOTHING;

CREATE TEMP TABLE inference_model_label_affected_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id
FROM system.role_assignments AS assignment
JOIN system.roles AS role ON role.id = assignment.role_id
WHERE assignment.status = 'active'
  AND role.tenant_id IS NULL
  AND role.role_key IN ('tenant.administrator', 'tenant.data_steward');

UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = now()
FROM inference_model_label_affected_principals AS affected
WHERE principal.id = affected.principal_id;

UPDATE system.refresh_token_families AS family
SET revoked_at = now(),
    revoked_reason = 'authorization_catalog_changed',
    updated_at = now()
FROM inference_model_label_affected_principals AS affected
WHERE family.principal_id = affected.principal_id
  AND family.revoked_at IS NULL;

COMMIT;
