ALTER TABLE common.task_executions
    ADD COLUMN IF NOT EXISTS lease_owner VARCHAR(100),
    ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS attempt INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 3;

CREATE INDEX IF NOT EXISTS idx_task_executions_quality_pending
    ON common.task_executions (created_at, id)
    WHERE module = 'quality' AND status = 'pending';

CREATE INDEX IF NOT EXISTS idx_task_executions_running_lease
    ON common.task_executions (lease_expires_at, id)
    WHERE status = 'running' AND lease_expires_at IS NOT NULL;
