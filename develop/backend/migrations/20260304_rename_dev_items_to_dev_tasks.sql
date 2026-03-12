-- Step 4: develop.dev_items → develop.dev_tasks
-- 重命名表，重命名字段，补充 BaseTask 基类字段

-- 1. 重命名表
ALTER TABLE develop.dev_items RENAME TO dev_tasks;

-- 2. 重命名字段 last_executed_at → last_run_at
ALTER TABLE develop.dev_tasks RENAME COLUMN last_executed_at TO last_run_at;

-- 3. 补充 BaseTask 字段
ALTER TABLE develop.dev_tasks
    ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ;

-- 4. 更新索引名称（PostgreSQL 索引不随表名自动更新）
ALTER INDEX IF EXISTS idx_dev_items_tenant_type RENAME TO idx_dev_tasks_tenant_type;
ALTER INDEX IF EXISTS idx_dev_items_status RENAME TO idx_dev_tasks_status;
