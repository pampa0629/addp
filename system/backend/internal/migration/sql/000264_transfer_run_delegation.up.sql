BEGIN;

-- Publish only the declared Transfer Tool delegation boundary. No Role,
-- membership, source Grant or execution authorization is granted here.
-- The exact start route additionally requires one-shot Transfer owner approval;
-- delegated credentials can neither decide approvals nor call other routes.
UPDATE system.permissions
SET delegable = true, updated_at = now()
WHERE permission_key IN ('transfer.task.execute', 'transfer.task.read')
  AND owner_module = 'transfer' AND status = 'active';

COMMIT;
