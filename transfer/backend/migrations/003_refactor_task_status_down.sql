-- 003_refactor_task_status_down.sql
-- 回滚迁移：恢复原有字段结构

-- 注意：此回滚脚本会尽力恢复原有结构，但无法完全恢复已删除字段的数据

-- 1. 恢复 status 字段的默认值
ALTER TABLE transfer.tasks ALTER COLUMN status SET DEFAULT 'pending';

-- 2. 删除新增的索引
DROP INDEX IF EXISTS idx_tasks_schedule;
DROP INDEX IF EXISTS idx_tasks_tenant_type;
DROP INDEX IF EXISTS idx_tasks_tenant_status;

-- 3. 恢复冗余字段（但数据已丢失，只能添加空字段）
ALTER TABLE transfer.tasks ADD COLUMN IF NOT EXISTS last_execution_id INTEGER;
ALTER TABLE transfer.tasks ADD COLUMN IF NOT EXISTS last_execution_status VARCHAR(20);
ALTER TABLE transfer.tasks ADD COLUMN IF NOT EXISTS last_execution_started_at TIMESTAMP;
ALTER TABLE transfer.tasks ADD COLUMN IF NOT EXISTS last_execution_finished_at TIMESTAMP;

-- 恢复索引
CREATE INDEX IF NOT EXISTS idx_tasks_last_execution ON transfer.tasks(last_execution_id);

-- 4. 恢复未使用的字段（但数据已丢失）
ALTER TABLE transfer.tasks ADD COLUMN IF NOT EXISTS mode VARCHAR(20) DEFAULT 'batch';
ALTER TABLE transfer.tasks ADD COLUMN IF NOT EXISTS max_parallelism INTEGER DEFAULT 1;
ALTER TABLE transfer.tasks ADD COLUMN IF NOT EXISTS retry_policy JSONB;

-- 5. 删除 enabled 字段
DROP INDEX IF EXISTS idx_tasks_enabled;
ALTER TABLE transfer.tasks DROP COLUMN IF EXISTS enabled;

-- 警告：status 字段值的转换无法完全回滚，建议从备份恢复数据
-- 可选：尝试恢复 status 字段（仅适用于定时任务）
-- UPDATE transfer.tasks
-- SET status = 'scheduled'
-- WHERE schedule IS NOT NULL AND schedule != '' AND status = 'idle';
