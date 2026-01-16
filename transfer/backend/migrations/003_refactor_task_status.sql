-- 003_refactor_task_status.sql
-- 重构任务状态字段：拆分启用状态和执行状态，删除冗余字段

-- 1. 添加新字段 enabled
ALTER TABLE transfer.tasks ADD COLUMN IF NOT EXISTS enabled BOOLEAN DEFAULT false;
CREATE INDEX IF NOT EXISTS idx_tasks_enabled ON transfer.tasks(enabled);

-- 2. 数据迁移：设置 enabled 字段
-- 将有 schedule 且状态为 'scheduled' 或 'running' 的任务设置为启用
UPDATE transfer.tasks
SET enabled = true
WHERE schedule IS NOT NULL
  AND schedule != ''
  AND status IN ('scheduled', 'running');

-- 3. 简化 status 字段值
-- 将所有非 'running' 状态统一为 'idle'
UPDATE transfer.tasks
SET status = CASE
    WHEN status = 'running' THEN 'running'
    ELSE 'idle'
END;

-- 4. 删除未使用的字段
ALTER TABLE transfer.tasks DROP COLUMN IF EXISTS mode;
ALTER TABLE transfer.tasks DROP COLUMN IF EXISTS max_parallelism;
ALTER TABLE transfer.tasks DROP COLUMN IF EXISTS retry_policy;

-- 5. 删除冗余字段（与 task_executions 表重复）
ALTER TABLE transfer.tasks DROP COLUMN IF EXISTS last_execution_id;
ALTER TABLE transfer.tasks DROP COLUMN IF EXISTS last_execution_status;
ALTER TABLE transfer.tasks DROP COLUMN IF EXISTS last_execution_started_at;
ALTER TABLE transfer.tasks DROP COLUMN IF EXISTS last_execution_finished_at;

-- 6. 添加性能优化索引
CREATE INDEX IF NOT EXISTS idx_tasks_tenant_status ON transfer.tasks(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant_type ON transfer.tasks(tenant_id, type);

-- 创建部分索引：只索引有 schedule 的任务
CREATE INDEX IF NOT EXISTS idx_tasks_schedule
ON transfer.tasks(schedule)
WHERE schedule IS NOT NULL AND schedule != '';

-- 7. 更新 status 字段的默认值（对于新创建的记录）
ALTER TABLE transfer.tasks ALTER COLUMN status SET DEFAULT 'idle';
