-- Rollback: 003_normalize_transfer_task_runtime
-- Clean break 后不恢复旧 status 枚举、mode、max_parallelism、retry_policy 或私有执行摘要字段。

DROP INDEX IF EXISTS transfer.idx_transfer_tasks_enabled;
DROP INDEX IF EXISTS transfer.idx_transfer_tasks_tenant_status;
DROP INDEX IF EXISTS transfer.idx_transfer_tasks_schedule;

ALTER TABLE transfer.transfer_tasks ALTER COLUMN status SET DEFAULT 'idle';
