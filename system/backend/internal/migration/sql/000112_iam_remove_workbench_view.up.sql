BEGIN;

DELETE FROM system.role_permissions AS role_permission
USING system.permissions AS permission
WHERE role_permission.permission_id = permission.id
  AND permission.permission_key IN (
      'workbench.view.create',
      'workbench.view.delete',
      'workbench.view.read',
      'workbench.view.update'
  );

UPDATE system.permissions
SET status = 'disabled',
    updated_at = transaction_timestamp()
WHERE permission_key IN (
    'workbench.view.create',
    'workbench.view.delete',
    'workbench.view.read',
    'workbench.view.update'
)
  AND status <> 'disabled';

COMMIT;
