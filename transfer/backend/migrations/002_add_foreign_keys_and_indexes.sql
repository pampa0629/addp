-- Migration: 002_add_transfer_task_indexes
-- Description: 为当前 transfer.transfer_tasks 基线补充查询索引。
-- Transfer 任务定义的真实来源是 transfer.transfer_tasks；执行记录统一进入 common.task_executions，
-- 通过 module + task_type + source_task_id 软关联，不再维护 Transfer 私有执行表或 source_id/target_id 外键。

BEGIN;

CREATE INDEX IF NOT EXISTS idx_transfer_tasks_tenant_type
    ON transfer.transfer_tasks(tenant_id, task_type);

CREATE INDEX IF NOT EXISTS idx_transfer_tasks_tenant_status
    ON transfer.transfer_tasks(tenant_id, status);

CREATE INDEX IF NOT EXISTS idx_transfer_tasks_tenant_creator_time
    ON transfer.transfer_tasks(tenant_id, created_by, created_at DESC)
    WHERE created_by IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_transfer_tasks_schedule
    ON transfer.transfer_tasks(schedule)
    WHERE schedule IS NOT NULL AND schedule != '';

COMMIT;
