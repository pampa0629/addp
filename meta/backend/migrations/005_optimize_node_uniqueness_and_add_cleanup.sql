-- Migration: 005_optimize_node_uniqueness_and_add_cleanup
-- Description: 优化 meta_node 唯一约束（去掉 node_type）并支持软删除定期清理
-- Date: 2026-01-02
-- Author: Claude Code

-- ============================================================================
-- 第一部分: 重建唯一索引（去掉 node_type）
-- ============================================================================

-- 删除旧的唯一索引
DROP INDEX IF EXISTS metadata.idx_meta_node_unique_root;
DROP INDEX IF EXISTS metadata.idx_meta_node_unique_child;

-- 创建新的唯一索引（不包含 node_type）
-- 根节点唯一索引（parent_node_id IS NULL）
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_meta_node_unique_root_v2
ON metadata.meta_node (tenant_id, engine_id, name)
WHERE parent_node_id IS NULL AND deleted_at IS NULL;

-- 子节点唯一索引（parent_node_id IS NOT NULL）
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_meta_node_unique_child_v2
ON metadata.meta_node (tenant_id, engine_id, parent_node_id, name)
WHERE parent_node_id IS NOT NULL AND deleted_at IS NULL;

-- ============================================================================
-- 第二部分: 添加清理相关索引
-- ============================================================================

-- 优化软删除记录查询（按删除时间排序）
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_meta_node_deleted_at_sorted
ON metadata.meta_node (deleted_at)
WHERE deleted_at IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_meta_item_deleted_at_sorted
ON metadata.meta_item (deleted_at)
WHERE deleted_at IS NOT NULL;

-- ============================================================================
-- 第三部分: 验证
-- ============================================================================
\echo '========================================='
\echo '迁移完成，正在验证...'
\echo '========================================='

-- 检查新索引
SELECT
  indexname,
  indexdef
FROM pg_indexes
WHERE schemaname = 'metadata'
  AND tablename = 'meta_node'
  AND indexname IN ('idx_meta_node_unique_root_v2', 'idx_meta_node_unique_child_v2', 'idx_meta_node_deleted_at_sorted');

-- 检查 meta_item 清理索引
SELECT
  indexname,
  indexdef
FROM pg_indexes
WHERE schemaname = 'metadata'
  AND tablename = 'meta_item'
  AND indexname = 'idx_meta_item_deleted_at_sorted';

-- 统计当前数据状态
SELECT
  'meta_node' AS table_name,
  COUNT(*) FILTER (WHERE deleted_at IS NULL) AS active_count,
  COUNT(*) FILTER (WHERE deleted_at IS NOT NULL) AS soft_deleted_count
FROM metadata.meta_node
UNION ALL
SELECT
  'meta_item' AS table_name,
  COUNT(*) FILTER (WHERE deleted_at IS NULL) AS active_count,
  COUNT(*) FILTER (WHERE deleted_at IS NOT NULL) AS soft_deleted_count
FROM metadata.meta_item;

\echo '========================================='
\echo '✅ 迁移 005 完成'
\echo '========================================='
