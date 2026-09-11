BEGIN;

-- DuckDB consumes one execution authorization and receives the exact Engine
-- access from System. It must not retain the historical Meta catalog read.
CREATE TEMP TABLE affected_duckdb_principals ON COMMIT DROP AS
SELECT DISTINCT assignment.principal_id, principal.authorization_version
FROM system.role_assignments AS assignment
JOIN system.roles AS role ON role.id = assignment.role_id
JOIN system.principals AS principal ON principal.id = assignment.principal_id
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.duckdb_runtime'
  AND assignment.status = 'active';

DO $$
DECLARE
    updated_roles integer;
BEGIN
    UPDATE system.roles
    SET name_i18n_key = 'roles.tenant.duckdb_runtime.name',
        description_i18n_key = 'roles.tenant.duckdb_runtime.description',
        role_type = 'tenant_builtin',
        allowed_scope_types = ARRAY['tenant']::text[],
        allowed_principal_types = ARRAY['service_principal']::text[],
        immutable = true,
        status = 'active',
        updated_at = transaction_timestamp()
    WHERE tenant_id IS NULL
      AND role_key = 'tenant.duckdb_runtime';

    GET DIAGNOSTICS updated_roles = ROW_COUNT;
    IF updated_roles <> 1 THEN
        RAISE EXCEPTION 'DuckDB tenant runtime role count = %, want 1', updated_roles;
    END IF;
END;
$$;

DELETE FROM system.role_permissions AS role_permission
USING system.roles AS role, system.permissions AS permission
WHERE role_permission.role_id = role.id
  AND role_permission.permission_id = permission.id
  AND role.tenant_id IS NULL
  AND role.role_key = 'tenant.duckdb_runtime'
  AND permission.permission_key <> 'system.execution_authorization.execute';

INSERT INTO system.role_permissions (
    role_id, permission_id, source_type, created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'system.execution_authorization.execute'
 AND permission.status = 'active'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.duckdb_runtime'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Authorization-version triggers normally advance active role holders when the
-- role definition or permission set changes. Keep the migration self-contained:
-- if a pre-existing installation did not advance a holder, advance it exactly
-- once from the version captured before this migration.
UPDATE system.principals AS principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = transaction_timestamp()
FROM affected_duckdb_principals AS affected
WHERE principal.id = affected.principal_id
  AND principal.authorization_version = affected.authorization_version;

UPDATE system.refresh_token_families AS family
SET revoked_at = transaction_timestamp(),
    revoked_reason = 'duckdb_runtime_role_permissions_changed',
    updated_at = transaction_timestamp()
FROM affected_duckdb_principals AS affected
WHERE family.principal_id = affected.principal_id
  AND family.revoked_at IS NULL;

DO $$
BEGIN
    IF (
        SELECT count(*)
        FROM system.role_permissions AS role_permission
        JOIN system.roles AS role ON role.id = role_permission.role_id
        JOIN system.permissions AS permission ON permission.id = role_permission.permission_id
        WHERE role.tenant_id IS NULL
          AND role.role_key = 'tenant.duckdb_runtime'
          AND permission.permission_key = 'system.execution_authorization.execute'
          AND permission.status = 'active'
    ) <> 1 THEN
        RAISE EXCEPTION 'DuckDB tenant runtime role must contain execution authorization consume permission';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM system.role_permissions AS role_permission
        JOIN system.roles AS role ON role.id = role_permission.role_id
        JOIN system.permissions AS permission ON permission.id = role_permission.permission_id
        WHERE role.tenant_id IS NULL
          AND role.role_key = 'tenant.duckdb_runtime'
          AND permission.permission_key <> 'system.execution_authorization.execute'
    ) THEN
        RAISE EXCEPTION 'DuckDB tenant runtime role contains permissions outside its execution boundary';
    END IF;
END;
$$;

COMMIT;
