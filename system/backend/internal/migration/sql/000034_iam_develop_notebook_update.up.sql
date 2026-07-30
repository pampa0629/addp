BEGIN;

UPDATE system.permissions
SET status = 'active',
    updated_at = transaction_timestamp()
WHERE permission_key = 'develop.notebook.update'
  AND status = 'disabled';

INSERT INTO system.role_permissions (
    role_id,
    permission_id,
    source_type,
    created_by_principal_id
)
SELECT role.id, permission.id, 'product', NULL
FROM system.roles AS role
JOIN system.permissions AS permission
  ON permission.permission_key = 'develop.notebook.update'
WHERE role.tenant_id IS NULL
  AND role.role_key = 'tenant.data_engineer'
  AND role.role_type = 'tenant_builtin'
  AND role.status = 'active'
  AND permission.status = 'active';

COMMIT;
