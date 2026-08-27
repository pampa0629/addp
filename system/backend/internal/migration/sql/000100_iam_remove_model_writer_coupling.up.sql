BEGIN;

DELETE FROM system.role_permissions AS role_permission
USING system.roles AS role, system.permissions AS permission
WHERE role_permission.role_id = role.id
  AND role_permission.permission_id = permission.id
  AND role.tenant_id IS NULL
  AND role.role_key IN ('tenant.develop_runtime', 'tenant.transfer_runtime')
  AND permission.permission_key IN (
      'model.materialization_read.execute',
      'model.materialization_write.execute'
  );

UPDATE system.permissions
SET status = 'disabled',
    updated_at = transaction_timestamp()
WHERE permission_key = 'model.materialization_write.execute'
  AND status <> 'disabled';

COMMIT;
