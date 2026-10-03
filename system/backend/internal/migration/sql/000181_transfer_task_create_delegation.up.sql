BEGIN;

-- Enable only the metadata-create Tool for principals already holding this
-- permission. No Role, membership, scope or task execution grant is added.
UPDATE system.permissions
SET delegable = true, updated_at = now()
WHERE permission_key = 'transfer.task.create'
  AND owner_module = 'transfer' AND status = 'active';

COMMIT;
