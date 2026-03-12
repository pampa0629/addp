-- Phase 1: transfer.tasks → transfer.transfer_tasks
-- 重命名表，补充 BaseTask 基类字段，删除废弃的 task_executions 表

-- 1. 重命名表
ALTER TABLE transfer.tasks RENAME TO transfer_tasks;

-- 2. 补充 BaseTask 基类字段
ALTER TABLE transfer.transfer_tasks
    ADD COLUMN IF NOT EXISTS task_type VARCHAR(20) NOT NULL DEFAULT 'import',
    ADD COLUMN IF NOT EXISTS last_execution_id VARCHAR(36),
    ADD COLUMN IF NOT EXISTS last_execution_status VARCHAR(20),
    ADD COLUMN IF NOT EXISTS last_run_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- 3. 新增索引
CREATE INDEX IF NOT EXISTS idx_transfer_tasks_task_type ON transfer.transfer_tasks(task_type);
CREATE INDEX IF NOT EXISTS idx_transfer_tasks_deleted_at ON transfer.transfer_tasks(deleted_at)
    WHERE deleted_at IS NOT NULL;

-- 4. 删除废弃的 transfer.task_executions 表（执行记录已迁移至 common.task_executions）
DROP TABLE IF EXISTS transfer.task_executions;
