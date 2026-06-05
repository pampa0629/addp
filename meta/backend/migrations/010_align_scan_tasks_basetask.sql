-- Step 3: meta.scan_tasks 字段清理
-- 删除 schedule_type，补充 last_execution_id / last_execution_status

-- 1. 删除 schedule_type（统一用 schedule Cron 字段）
ALTER TABLE meta.scan_tasks DROP COLUMN IF EXISTS schedule_type;

-- 2. 补充 BaseTask 字段
ALTER TABLE meta.scan_tasks
    ADD COLUMN IF NOT EXISTS last_execution_id VARCHAR(36),
    ADD COLUMN IF NOT EXISTS last_execution_status VARCHAR(20);
