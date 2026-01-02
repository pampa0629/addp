-- Migration: 004_drop_unused_tables
-- Description: 删除未使用的 4 个空表
-- Date: 2026-01-02
-- Author: Claude Code

BEGIN;

-- 删除顺序：先删子表，后删父表（处理外键约束）
DROP TABLE IF EXISTS metadata.meta_node_child_rule CASCADE;
DROP TABLE IF EXISTS metadata.meta_node_type_dict CASCADE;
DROP TABLE IF EXISTS metadata.meta_change_log CASCADE;
DROP TABLE IF EXISTS metadata.meta_json_schema CASCADE;

COMMIT;

-- 验证删除结果
SELECT
    CASE
        WHEN COUNT(*) = 0 THEN '✅ 所有表已成功删除'
        ELSE '❌ 仍有表未删除: ' || string_agg(table_name, ', ')
    END AS result
FROM information_schema.tables
WHERE table_schema = 'metadata'
  AND table_name IN ('meta_change_log', 'meta_node_type_dict', 'meta_json_schema', 'meta_node_child_rule');
