BEGIN;

DELETE FROM system.role_permissions AS role_permission
USING system.permissions AS permission
WHERE role_permission.permission_id = permission.id
  AND permission.permission_key IN (
      'standard.dimension_hierarchy.create',
      'standard.dimension_hierarchy.delete',
      'standard.dimension_hierarchy.read',
      'standard.dimension_hierarchy.update'
  );

UPDATE system.permissions
SET status = 'disabled',
    updated_at = transaction_timestamp()
WHERE permission_key IN (
    'standard.dimension_hierarchy.create',
    'standard.dimension_hierarchy.delete',
    'standard.dimension_hierarchy.read',
    'standard.dimension_hierarchy.update'
)
  AND status <> 'disabled';

COMMIT;
