-- ================================================
-- Develop 模块表结构优化（激进清理）
-- 创建时间: 2026-02-11
-- 说明: 彻底清理废弃字段，不保留向后兼容
--   1. 迁移 query_type 到 content.query_type 并删除顶层字段
--   2. 迁移 engine_id 到 execution_config.engine_id 并删除顶层字段
--   3. 删除冗余字段 is_scheduled
-- ================================================

BEGIN;

-- 步骤 1：迁移 query_type 到 content
UPDATE develop.dev_items
SET content = jsonb_set(
  COALESCE(content, '{}'::jsonb),
  '{query_type}',
  to_jsonb(query_type)
)
WHERE dev_type = 'query'
  AND query_type IS NOT NULL
  AND query_type != '';

-- 步骤 2：迁移 engine_id 到 execution_config
UPDATE develop.dev_items
SET execution_config = COALESCE(execution_config, '{}'::jsonb) || jsonb_build_object('engine_id', engine_id)
WHERE engine_id IS NOT NULL;

-- 步骤 3：删除废弃字段
ALTER TABLE develop.dev_items DROP COLUMN IF EXISTS is_scheduled;
ALTER TABLE develop.dev_items DROP COLUMN IF EXISTS query_type;
ALTER TABLE develop.dev_items DROP COLUMN IF EXISTS engine_id;

-- 步骤 4：添加 JSONB 索引
CREATE INDEX IF NOT EXISTS idx_dev_items_content_query_type
ON develop.dev_items USING gin ((content -> 'query_type'));

CREATE INDEX IF NOT EXISTS idx_dev_items_execution_config_engine_id
ON develop.dev_items USING gin ((execution_config -> 'engine_id'));

-- 验证迁移结果
DO $$
DECLARE
  total_items INTEGER;
  query_items INTEGER;
  items_with_content_qt INTEGER;
  items_with_config_engine INTEGER;
BEGIN
  SELECT COUNT(*) INTO total_items FROM develop.dev_items;

  SELECT COUNT(*) INTO query_items
  FROM develop.dev_items
  WHERE dev_type = 'query';

  SELECT COUNT(*) INTO items_with_content_qt
  FROM develop.dev_items
  WHERE dev_type = 'query' AND content ? 'query_type';

  SELECT COUNT(*) INTO items_with_config_engine
  FROM develop.dev_items
  WHERE execution_config ? 'engine_id';

  RAISE NOTICE '========================================';
  RAISE NOTICE 'Develop 模块表结构优化迁移完成（激进清理）';
  RAISE NOTICE '========================================';
  RAISE NOTICE '总记录数: %', total_items;
  RAISE NOTICE 'Query 类型记录数: %', query_items;
  RAISE NOTICE 'content.query_type: %', items_with_content_qt;
  RAISE NOTICE 'execution_config.engine_id: %', items_with_config_engine;
  RAISE NOTICE '========================================';
  RAISE NOTICE '已删除字段: is_scheduled, query_type, engine_id';
  RAISE NOTICE '========================================';
END $$;

COMMIT;
