-- ========================================
-- Develop 模块: 回滚 Resource → Engine 重命名
-- 作者: Claude Code
-- 日期: 2025-12-29
-- ========================================

-- develop.scripts 表
ALTER TABLE develop.scripts RENAME COLUMN engine_id TO resource_id;
DROP INDEX IF EXISTS idx_scripts_engine_id;
CREATE INDEX idx_scripts_resource_id ON develop.scripts(resource_id);

-- develop.dev_executions 表
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'develop'
               AND table_name = 'dev_executions'
               AND column_name = 'engine_id') THEN
        ALTER TABLE develop.dev_executions RENAME COLUMN engine_id TO resource_id;
    END IF;
END $$;

-- develop.dev_items 表索引
DROP INDEX IF EXISTS develop.idx_dev_items_engine;
CREATE INDEX idx_dev_items_resource ON develop.dev_items(engine_id);

-- 回滚完成
