-- ========================================
-- Develop 模块: Resource → Engine 重命名
-- 作者: Claude Code
-- 日期: 2025-12-29
-- ========================================

-- develop.scripts 表
ALTER TABLE develop.scripts RENAME COLUMN resource_id TO engine_id;
DROP INDEX IF EXISTS idx_scripts_resource_id;
CREATE INDEX idx_scripts_engine_id ON develop.scripts(engine_id);

-- develop.dev_executions 表
-- 注意：确认表是否存在 resource_id 列
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'develop'
               AND table_name = 'dev_executions'
               AND column_name = 'resource_id') THEN
        ALTER TABLE develop.dev_executions RENAME COLUMN resource_id TO engine_id;
    END IF;
END $$;

-- develop.dev_items 表索引
DROP INDEX IF EXISTS develop.idx_dev_items_resource;
DROP INDEX IF EXISTS develop.idx_dev_items_engine;
CREATE INDEX IF NOT EXISTS idx_dev_items_engine ON develop.dev_items(engine_id);

-- 迁移完成
