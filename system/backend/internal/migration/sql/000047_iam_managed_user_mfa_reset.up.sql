BEGIN;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key,
    description_i18n_key, status
) VALUES (
    'iam.mfa_credential.reset', 'system', 'reset', 'high', false,
    ARRAY['platform']::text[], false,
    'permissions.iam.mfa_credential.reset.name',
    'permissions.iam.mfa_credential.reset.description', 'active'
);

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles role
JOIN system.permissions permission
  ON permission.permission_key = 'iam.mfa_credential.reset'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'platform.security_administrator'
  AND role.role_type = 'platform_builtin'
  AND role.status = 'active';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM system.role_permissions role_permission
        JOIN system.roles role ON role.id = role_permission.role_id
        JOIN system.permissions permission ON permission.id = role_permission.permission_id
        WHERE role.tenant_id IS NULL
          AND role.role_key = 'platform.security_administrator'
          AND permission.permission_key = 'iam.mfa_credential.reset'
          AND permission.status = 'active'
    ) THEN
        RAISE EXCEPTION 'security administrator MFA credential reset permission is required';
    END IF;
END;
$$;

COMMIT;
