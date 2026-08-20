ALTER TABLE common.task_executions
    ADD COLUMN IF NOT EXISTS execution_boundary VARCHAR(20),
    ADD COLUMN IF NOT EXISTS retry_of_execution_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS lease_token UUID;

UPDATE common.task_executions
SET execution_boundary = CASE
    WHEN module = 'transfer'
     AND execution_config -> 'runtime' ->> 'boundary' = 'continuous'
    THEN 'continuous'
    ELSE 'bounded'
END
WHERE execution_boundary IS NULL;

ALTER TABLE common.task_executions
    ALTER COLUMN execution_boundary SET DEFAULT 'bounded',
    ALTER COLUMN execution_boundary SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_task_executions_bounded_pending
    ON common.task_executions (module, task_type, created_at ASC, id ASC)
    WHERE execution_boundary = 'bounded' AND status = 'pending';

CREATE INDEX IF NOT EXISTS idx_task_executions_bounded_running_lease
    ON common.task_executions (module, task_type, lease_expires_at ASC, id ASC)
    WHERE execution_boundary = 'bounded'
      AND status = 'running'
      AND lease_expires_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_task_executions_retry_of
    ON common.task_executions (retry_of_execution_id)
    WHERE retry_of_execution_id IS NOT NULL;
