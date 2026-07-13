ALTER TABLE transfer.transfer_tasks
    ADD COLUMN IF NOT EXISTS apply_identity UUID;

UPDATE transfer.transfer_tasks
SET apply_identity = gen_random_uuid()
WHERE apply_identity IS NULL;

ALTER TABLE transfer.transfer_tasks
    ALTER COLUMN apply_identity SET DEFAULT gen_random_uuid(),
    ALTER COLUMN apply_identity SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_tasks_apply_identity
    ON transfer.transfer_tasks (apply_identity);
