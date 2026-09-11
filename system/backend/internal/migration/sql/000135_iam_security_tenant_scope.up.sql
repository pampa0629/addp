BEGIN;

-- Security 当前只拥有 Tenant 级治理事实，也没有 Department / Project Group
-- 到专业资源的权威归属或策略校验。先冻结受影响主体，再收敛目录与存量角色。
CREATE TEMP TABLE affected_security_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id
FROM system.role_assignments AS assignment
JOIN system.role_permissions AS role_permission
  ON role_permission.role_id = assignment.role_id
JOIN system.permissions AS permission
  ON permission.id = role_permission.permission_id
WHERE assignment.status = 'active'
  AND permission.owner_module = 'security'
  AND permission.status = 'active'
  AND permission.allowed_scope_types IS DISTINCT FROM ARRAY['tenant']::text[];

DO $$
BEGIN
    IF (
        SELECT count(*)
        FROM system.permissions
        WHERE owner_module = 'security'
          AND status = 'active'
    ) <> 39 THEN
        RAISE EXCEPTION 'Security tenant-scope migration expected 39 active permissions';
    END IF;

END;
$$;

-- 保留原角色及其其他权限，只移除无法继续满足角色 Scope 的 Security 绑定。
DELETE FROM system.role_permissions AS role_permission
USING system.roles AS role, system.permissions AS permission
WHERE role_permission.role_id = role.id
  AND role_permission.permission_id = permission.id
  AND permission.owner_module = 'security'
  AND permission.status = 'active'
  AND NOT (role.allowed_scope_types <@ ARRAY['tenant']::text[]);

UPDATE system.permissions
SET allowed_scope_types = ARRAY['tenant']::text[],
    updated_at = transaction_timestamp()
WHERE owner_module = 'security'
  AND status = 'active'
  AND allowed_scope_types IS DISTINCT FROM ARRAY['tenant']::text[];

INSERT INTO system.roles (
    role_key, name_i18n_key, description_i18n_key, role_type,
    allowed_scope_types, allowed_principal_types, immutable, status
) VALUES
    (
        'tenant.protected_data_requester',
        'roles.tenant.protected_data_requester.name',
        'roles.tenant.protected_data_requester.description',
        'tenant_builtin', ARRAY['tenant']::text[], ARRAY['user']::text[], true, 'active'
    ),
    (
        'tenant.security_manager',
        'roles.tenant.security_manager.name',
        'roles.tenant.security_manager.description',
        'tenant_builtin', ARRAY['tenant']::text[], ARRAY['user']::text[], true, 'active'
    );

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.owner_module = 'security'
 AND permission.status = 'active'
 AND permission.tenant_customizable
WHERE role.tenant_id IS NULL
  AND (
      role.role_key = 'tenant.security_manager'
      OR (
          role.role_key = 'tenant.protected_data_requester'
          AND permission.permission_key IN (
              'security.protection_access_request.create',
              'security.protection_access_request.read'
          )
      )
  )
ORDER BY role.role_key, permission.permission_key;

UPDATE system.refresh_token_families AS family
SET revoked_at = transaction_timestamp(),
    revoked_reason = 'security_permission_scope_changed',
    updated_at = transaction_timestamp()
FROM affected_security_principals AS affected
WHERE family.principal_id = affected.principal_id
  AND family.revoked_at IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM system.permissions
        WHERE owner_module = 'security'
          AND status = 'active'
          AND allowed_scope_types IS DISTINCT FROM ARRAY['tenant']::text[]
    ) THEN
        RAISE EXCEPTION 'active Security permissions must be tenant-scoped';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM system.role_permissions AS role_permission
        JOIN system.roles AS role ON role.id = role_permission.role_id
        JOIN system.permissions AS permission ON permission.id = role_permission.permission_id
        WHERE permission.owner_module = 'security'
          AND permission.status = 'active'
          AND NOT (role.allowed_scope_types <@ permission.allowed_scope_types)
    ) THEN
        RAISE EXCEPTION 'role scope exceeds the migrated Security permission scope';
    END IF;

    IF (
        SELECT count(*)
        FROM system.role_permissions AS role_permission
        JOIN system.roles AS role ON role.id = role_permission.role_id
        JOIN system.permissions AS permission ON permission.id = role_permission.permission_id
        WHERE role.tenant_id IS NULL
          AND role.role_key = 'tenant.security_manager'
          AND permission.owner_module = 'security'
          AND permission.status = 'active'
    ) <> 37 THEN
        RAISE EXCEPTION 'tenant Security Manager must contain 37 management permissions';
    END IF;

    IF (
        SELECT count(*)
        FROM system.role_permissions AS role_permission
        JOIN system.roles AS role ON role.id = role_permission.role_id
        JOIN system.permissions AS permission ON permission.id = role_permission.permission_id
        WHERE role.tenant_id IS NULL
          AND role.role_key = 'tenant.protected_data_requester'
          AND permission.permission_key IN (
              'security.protection_access_request.create',
              'security.protection_access_request.read'
          )
    ) <> 2 THEN
        RAISE EXCEPTION 'tenant Protected Data Requester must contain two request permissions';
    END IF;
END;
$$;

COMMIT;
