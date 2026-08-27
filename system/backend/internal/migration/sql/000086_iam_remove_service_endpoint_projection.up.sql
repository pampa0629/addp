BEGIN;

DELETE FROM system.role_permissions role_permission
USING system.permissions permission
WHERE role_permission.permission_id = permission.id
  AND permission.permission_key = 'service.endpoint.read';

UPDATE system.permissions
SET status = 'disabled', updated_at = transaction_timestamp()
WHERE permission_key = 'service.endpoint.read'
  AND status <> 'disabled';

COMMIT;
