-- Migration: 添加 meta_node 唯一性约束
-- Purpose: 防止重复创建相同的节点
-- Date: 2026-01-20

-- 1. 删除重复的节点（保留最新的，即 ID 最大的）
WITH duplicates AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY tenant_id, engine_id, COALESCE(parent_node_id, 0), name, node_type
            ORDER BY id DESC
        ) AS rn
    FROM meta.meta_node
)
DELETE FROM meta.meta_node
WHERE id IN (
    SELECT id FROM duplicates WHERE rn > 1
);

-- 2. 添加唯一性约束
-- 注意：parent_node_id 可能为 NULL，需要使用部分唯一索引
CREATE UNIQUE INDEX IF NOT EXISTS idx_meta_node_unique_with_parent
ON meta.meta_node (tenant_id, engine_id, parent_node_id, name, node_type)
WHERE parent_node_id IS NOT NULL AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_meta_node_unique_without_parent
ON meta.meta_node (tenant_id, engine_id, name, node_type)
WHERE parent_node_id IS NULL AND deleted_at IS NULL;

-- 说明：
-- 1. 使用两个部分唯一索引分别处理有无父节点的情况
-- 2. WHERE deleted_at IS NULL 确保软删除的记录不影响唯一性
-- 3. 这样可以防止数据库层面插入重复节点
