DROP INDEX IF EXISTS common.idx_task_executions_quality_pending;

CREATE INDEX idx_task_executions_quality_pending
    ON common.task_executions (created_at ASC, id ASC)
    WHERE module = 'quality'
      AND task_type = 'check'
      AND status = 'pending'
      AND execution_authorization_id IS NOT NULL;

CREATE INDEX idx_task_executions_quality_running_lease
    ON common.task_executions (lease_expires_at ASC, id ASC)
    WHERE module = 'quality'
      AND task_type = 'check'
      AND status = 'running'
      AND lease_expires_at IS NOT NULL;
