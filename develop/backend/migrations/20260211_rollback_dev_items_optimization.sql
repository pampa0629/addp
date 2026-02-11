-- ================================================
-- Develop 模块表结构优化回滚脚本（激进清理回滚）
-- 创建时间: 2026-02-11
-- 说明: 回滚所有字段删除
--   1. 恢复 is_scheduled、query_type、engine_id 字段
--   2. 从 JSONB 中提取数据回填
--   3. 删除新增的索引
-- ================================================

BEGIN;

-- 步骤 1：恢复已删除的字段
ALTER TABLE develop.dev_items ADD COLUMN IF NOT EXISTS is_scheduled BOOLEAN DEFAULT false;
ALTER TABLE develop.dev_items ADD COLUMN IF NOT EXISTS query_type VARCHAR(50);
ALTER TABLE develop.dev_items ADD COLUMN IF NOT EXISTS engine_id INTEGER;

-- 步骤 2：从 content 中回填 query_type
UPDATE develop.dev_items
SET query_type = (content->>'query_type')::VARCHAR
WHERE dev_type = 'query' AND content ? 'query_type';

-- 步骤 3：从 execution_config 中回填 engine_id
UPDATE develop.dev_items
SET engine_id = (execution_config->>'engine_id')::INTEGER
WHERE execution_config ? 'engine_id';

-- 步骤 4：从 schedule 推导 is_scheduled
UPDATE develop.dev_items
SET is_scheduled = (schedule IS NOT NULL AND schedule != '' AND schedule != '0');

-- 步骤 5：删除新增的索引
DROP INDEX IF EXISTS develop.idx_dev_items_content_query_type;
DROP INDEX IF EXISTS develop.idx_dev_items_execution_config_engine_id;

-- 步骤 6：恢复旧索引
CREATE INDEX IF NOT EXISTS idx_dev_items_query_type ON develop.dev_items(query_type);
CREATE INDEX IF NOT EXISTS idx_dev_items_resource ON develop.dev_items(engine_id);

-- 验证回滚结果
DO $$
DECLARE
  total_items INTEGER;
  restored_query_type INTEGER;
  restored_engine_id INTEGER;
  scheduled_items INTEGER;
BEGIN
  SELECT COUNT(*) INTO total_items FROM develop.dev_items;

  SELECT COUNT(*) INTO restored_query_type
  FROM develop.dev_items
  WHERE query_type IS NOT NULL;

  SELECT COUNT(*) INTO restored_engine_id
  FROM develop.dev_items
  WHERE engine_id IS NOT NULL;

  SELECT COUNT(*) INTO scheduled_items
  FROM develop.dev_items
  WHERE is_scheduled = true;

  RAISE NOTICE '========================================';
  RAISE NOTICE 'Develop 模块表结构优化回滚完成';
  RAISE NOTICE '========================================';
  RAISE NOTICE '总记录数: %', total_items;
  RAISE NOTICE '已恢复 query_type: %', restored_query_type;
  RAISE NOTICE '已恢复 engine_id: %', restored_engine_id;
  RAISE NOTICE '已启用调度的记录数: %', scheduled_items;
  RAISE NOTICE '========================================';
END $$;

COMMIT;
