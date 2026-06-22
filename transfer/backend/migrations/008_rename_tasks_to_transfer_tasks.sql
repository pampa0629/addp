-- Phase 1: ensure transfer.transfer_tasks baseline
-- Transfer 当前只保留 transfer.transfer_tasks 任务定义表；
-- 执行记录统一进入 common.task_executions，不保留 Transfer 私有执行表。

ALTER TABLE transfer.transfer_tasks
    ADD COLUMN IF NOT EXISTS task_type VARCHAR(20) NOT NULL DEFAULT 'sync',
    ADD COLUMN IF NOT EXISTS last_execution_id VARCHAR(36),
    ADD COLUMN IF NOT EXISTS last_execution_status VARCHAR(20),
    ADD COLUMN IF NOT EXISTS last_run_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_transfer_tasks_task_type ON transfer.transfer_tasks(task_type);
CREATE INDEX IF NOT EXISTS idx_transfer_tasks_deleted_at ON transfer.transfer_tasks(deleted_at)
    WHERE deleted_at IS NOT NULL;

DROP TABLE IF EXISTS transfer.task_executions;
