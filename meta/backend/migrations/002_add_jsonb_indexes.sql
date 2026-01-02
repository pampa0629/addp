-- Migration: 002_add_jsonb_indexes
-- Description: 为 JSONB 字段添加 GIN 索引，优化查询性能
-- Date: 2026-01-02
-- Author: Claude Code
-- Expected Benefit: JSONB 查询性能提升 10-100 倍

-- ============================================================================
-- 创建 GIN 索引（CONCURRENTLY，不能在事务中）
-- ============================================================================

-- 1. meta_node.attributes 通用 GIN 索引
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_meta_node_attributes_gin
ON metadata.meta_node USING GIN (attributes);

-- 2. meta_item.attributes 通用 GIN 索引
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_meta_item_attributes_gin
ON metadata.meta_item USING GIN (attributes);

-- 3. 空间表元数据索引（高频查询）
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_meta_item_spatial
ON metadata.meta_item ((attributes->'spatial_metadata'))
WHERE attributes ? 'spatial_metadata';

-- 4. 文件扩展名索引
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_meta_item_extension
ON metadata.meta_item ((attributes->>'extension'))
WHERE attributes ? 'extension';

-- 5. 文件 MIME 类型索引
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_meta_item_mime
ON metadata.meta_item ((attributes->>'mime_type'))
WHERE attributes ? 'mime_type';

-- 6. scan_task_runs.parameters 索引
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_scan_run_parameters_gin
ON metadata.scan_task_runs USING GIN (parameters);

-- 7. scan_task_runs.result_summary 索引
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_scan_run_result_gin
ON metadata.scan_task_runs USING GIN (result_summary);

-- ============================================================================
-- 验证索引创建
-- ============================================================================
\echo '========================================='
\echo '迁移完成，正在验证...'
\echo '========================================='

SELECT
  schemaname,
  tablename,
  indexname,
  indexdef
FROM pg_indexes
WHERE schemaname = 'metadata'
  AND (indexname LIKE '%_gin' OR indexname LIKE '%_spatial%' OR indexname LIKE '%_extension%' OR indexname LIKE '%_mime%')
ORDER BY tablename, indexname;

\echo '========================================='
\echo '✅ 迁移 002 完成'
\echo '========================================='
