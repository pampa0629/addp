BEGIN;

-- Infra inspection is private source reading, owned by the Transfer runtime.
CREATE TEMP TABLE affected_meta_inspect_principals ON COMMIT DROP AS
SELECT DISTINCT principal.id, principal.authorization_version
FROM system.principals principal
JOIN system.role_assignments assignment ON assignment.principal_id = principal.id
JOIN system.roles role ON role.id = assignment.role_id
JOIN system.role_permissions binding ON binding.role_id = role.id
JOIN system.permissions permission ON permission.id = binding.permission_id
WHERE assignment.status = 'active'
  AND permission.permission_key = 'meta.inspect.execute'
  AND NOT (role.tenant_id IS NULL AND role.role_key = 'tenant.transfer_runtime');

DELETE FROM system.role_permissions binding
USING system.roles role, system.permissions permission
WHERE binding.role_id = role.id
  AND binding.permission_id = permission.id
  AND permission.permission_key = 'meta.inspect.execute'
  AND NOT (role.tenant_id IS NULL AND role.role_key = 'tenant.transfer_runtime');

UPDATE system.permissions
SET allowed_scope_types = ARRAY['tenant']::text[],
    tenant_customizable = false,
    delegable = false,
    updated_at = transaction_timestamp()
WHERE permission_key = 'meta.inspect.execute';

-- Binding triggers normally advance holders. Preserve that behavior and ensure
-- every affected subject advances even on an installation without those hooks.
UPDATE system.principals principal
SET authorization_version = principal.authorization_version + 1,
    updated_at = transaction_timestamp()
FROM affected_meta_inspect_principals affected
WHERE principal.id = affected.id
  AND principal.authorization_version = affected.authorization_version;

DO $$
BEGIN
    IF (
        SELECT count(*) FROM system.permissions
        WHERE permission_key = 'meta.inspect.execute' AND status = 'active'
          AND allowed_scope_types = ARRAY['tenant']::text[]
          AND NOT tenant_customizable AND NOT delegable
    ) <> 1 THEN
        RAISE EXCEPTION 'Infra inspection must be a private Tenant runtime permission';
    END IF;
    IF (
        SELECT count(*) FROM system.role_permissions binding
        JOIN system.permissions permission ON permission.id = binding.permission_id
        WHERE permission.permission_key = 'meta.inspect.execute'
    ) <> 1 OR (
        SELECT count(*) FROM system.role_permissions binding
        JOIN system.roles role ON role.id = binding.role_id
        JOIN system.permissions permission ON permission.id = binding.permission_id
        WHERE permission.permission_key = 'meta.inspect.execute'
          AND role.tenant_id IS NULL AND role.role_key = 'tenant.transfer_runtime'
    ) <> 1 THEN
        RAISE EXCEPTION 'Infra inspection must only be granted to Transfer runtime';
    END IF;
END;
$$;

COMMIT;
