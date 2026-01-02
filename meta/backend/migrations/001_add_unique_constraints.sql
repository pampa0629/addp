-- Migration: 001_add_unique_constraints
-- Description: 添加唯一约束和外键约束，防止重复数据
-- Date: 2026-01-02
-- Author: Claude Code

-- ============================================================================
-- 第一部分: 清理重复数据和添加外键（在事务中）
-- ============================================================================

BEGIN;

-- 1. 清理 meta_node 表中的重复数据（保留最新记录）
WITH duplicates AS (
  SELECT
    id,
    ROW_NUMBER() OVER (
      PARTITION BY engine_id, tenant_id, node_type, name, COALESCE(parent_node_id, 0)
      ORDER BY created_at DESC, id DESC
    ) AS rn
  FROM metadata.meta_node
  WHERE deleted_at IS NULL
)
UPDATE metadata.meta_node
SET deleted_at = NOW()
WHERE id IN (
  SELECT id FROM duplicates WHERE rn > 1
);

-- 2. 清理 meta_item 表中的重复数据（按 fingerprint 去重）
WITH duplicates AS (
  SELECT
    id,
    ROW_NUMBER() OVER (
      PARTITION BY fingerprint
      ORDER BY created_at DESC, id DESC
    ) AS rn
  FROM metadata.meta_item
  WHERE deleted_at IS NULL AND fingerprint IS NOT NULL
)
UPDATE metadata.meta_item
SET deleted_at = NOW()
WHERE id IN (
  SELECT id FROM duplicates WHERE rn > 1
);

-- 3. 添加外键约束
ALTER TABLE metadata.meta_item
DROP CONSTRAINT IF EXISTS fk_meta_item_node;

ALTER TABLE metadata.meta_item
ADD CONSTRAINT fk_meta_item_node
FOREIGN KEY (node_id)
REFERENCES metadata.meta_node(id)
ON DELETE CASCADE;

ALTER TABLE metadata.scan_task_runs
DROP CONSTRAINT IF EXISTS fk_scan_run_task;

ALTER TABLE metadata.scan_task_runs
ADD CONSTRAINT fk_scan_run_task
FOREIGN KEY (task_id)
REFERENCES metadata.scan_tasks(id)
ON DELETE CASCADE;

COMMIT;

-- ============================================================================
-- 第二部分: 创建唯一索引（CONCURRENTLY，不能在事务中）
-- ============================================================================

-- 根节点唯一索引（parent_node_id IS NULL）
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_meta_node_unique_root
ON metadata.meta_node (engine_id, tenant_id, node_type, name)
WHERE parent_node_id IS NULL AND deleted_at IS NULL;

-- 子节点唯一索引（parent_node_id IS NOT NULL）
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_meta_node_unique_child
ON metadata.meta_node (engine_id, tenant_id, node_type, name, parent_node_id)
WHERE parent_node_id IS NOT NULL AND deleted_at IS NULL;

-- ============================================================================
-- 第三部分: 验证（只读查询）
-- ============================================================================
\echo '========================================='
\echo '迁移完成，正在验证...'
\echo '========================================='

-- 检查唯一索引
SELECT
  tablename,
  indexname,
  indexdef
FROM pg_indexes
WHERE schemaname = 'metadata'
  AND tablename = 'meta_node'
  AND indexname IN ('idx_meta_node_unique_root', 'idx_meta_node_unique_child');

-- 检查外键约束
SELECT
  table_name,
  constraint_name,
  constraint_type
FROM information_schema.table_constraints
WHERE table_schema = 'metadata'
  AND constraint_type = 'FOREIGN KEY'
  AND constraint_name IN ('fk_meta_item_node', 'fk_scan_run_task');

\echo '========================================='
\echo '✅ 迁移 001 完成'
\echo '========================================='
