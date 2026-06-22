-- 003_normalize_transfer_task_runtime.sql
-- 当前 Transfer task definition 只保存定义态和轻量运行摘要；
-- 单次执行状态进入 common.task_executions。任务定义 status 只保留 idle / running。

ALTER TABLE transfer.transfer_tasks ADD COLUMN IF NOT EXISTS enabled BOOLEAN DEFAULT false;
CREATE INDEX IF NOT EXISTS idx_transfer_tasks_enabled ON transfer.transfer_tasks(enabled);

UPDATE transfer.transfer_tasks
SET enabled = true
WHERE schedule IS NOT NULL
  AND schedule != ''
  AND status = 'running';

UPDATE transfer.transfer_tasks
SET status = CASE
    WHEN status = 'running' THEN 'running'
    ELSE 'idle'
END;

ALTER TABLE transfer.transfer_tasks DROP COLUMN IF EXISTS mode;
ALTER TABLE transfer.transfer_tasks DROP COLUMN IF EXISTS max_parallelism;
ALTER TABLE transfer.transfer_tasks DROP COLUMN IF EXISTS retry_policy;
ALTER TABLE transfer.transfer_tasks DROP COLUMN IF EXISTS last_execution_started_at;
ALTER TABLE transfer.transfer_tasks DROP COLUMN IF EXISTS last_execution_finished_at;

CREATE INDEX IF NOT EXISTS idx_transfer_tasks_tenant_status
    ON transfer.transfer_tasks(tenant_id, status);

CREATE INDEX IF NOT EXISTS idx_transfer_tasks_schedule
    ON transfer.transfer_tasks(schedule)
    WHERE schedule IS NOT NULL AND schedule != '';

ALTER TABLE transfer.transfer_tasks ALTER COLUMN status SET DEFAULT 'idle';
